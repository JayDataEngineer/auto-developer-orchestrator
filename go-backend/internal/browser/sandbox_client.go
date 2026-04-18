package browser

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
	"go.uber.org/zap"
)

const (
	defaultTimeout     = 30 * time.Second
	navigationTimeout  = 60 * time.Second
	settleDelay        = 1 * time.Second // pause after navigation for dynamic content
)

// SandboxBrowserClient manages a browser connection to a sandbox Chrome instance
// via Chrome DevTools Protocol (CDP). A persistent tab is created on Connect()
// and reused for all actions so the user can see agent activity in the VNC viewer.
type SandboxBrowserClient struct {
	cdpPort     int
	wsURL       string
	logger      *zap.Logger
	allocator   context.Context
	allocCancel context.CancelFunc
	tabCtx      context.Context  // persistent tab — reused across actions
	tabCancel   context.CancelFunc

	// Cached state from last action
	lastURL        string
	lastTitle      string
	lastElements   []LabeledElement
	lastScreenshot []byte

	mu sync.RWMutex
}

// NewSandboxBrowserClient creates a new browser client for a sandbox Chrome instance.
// hostname should be the Docker container name so the Go backend can reach Chrome
// via the shared Docker network.
func NewSandboxBrowserClient(cdpPort int, hostname string, logger *zap.Logger) (*SandboxBrowserClient, error) {
	if cdpPort <= 0 {
		return nil, fmt.Errorf("cdp port must be positive, got %d", cdpPort)
	}
	if hostname == "" {
		hostname = "localhost"
	}

	wsURL := fmt.Sprintf("ws://%s:%d", hostname, cdpPort)

	return &SandboxBrowserClient{
		cdpPort: cdpPort,
		wsURL:   wsURL,
		logger:  logger,
	}, nil
}

// Connect verifies Chrome CDP is reachable, stores the allocator, and creates
// a persistent tab that will be reused for all subsequent actions.
func (sbc *SandboxBrowserClient) Connect(ctx context.Context) error {
	sbc.mu.Lock()
	defer sbc.mu.Unlock()

	if sbc.tabCtx != nil {
		return nil
	}

	allocCtx, allocCancel := chromedp.NewRemoteAllocator(context.Background(), sbc.wsURL)

	// Create the persistent tab — this is the tab the user sees in VNC.
	tabCtx, tabCancel := chromedp.NewContext(allocCtx)
	if err := chromedp.Run(tabCtx, chromedp.Navigate("about:blank")); err != nil {
		tabCancel()
		allocCancel()
		return fmt.Errorf("CDP connection test failed: %w", err)
	}

	sbc.allocator = allocCtx
	sbc.allocCancel = allocCancel
	sbc.tabCtx = tabCtx
	sbc.tabCancel = tabCancel

	sbc.logger.Info("sandbox browser client connected", zap.Int("cdp_port", sbc.cdpPort))
	return nil
}

// runInTab runs fn in the persistent tab with a timeout. The same tab is reused
// across all actions so the user sees agent activity in the VNC viewer.
func (sbc *SandboxBrowserClient) runInTab(timeout time.Duration, fn func(ctx context.Context) error) error {
	sbc.mu.RLock()
	tabCtx := sbc.tabCtx
	sbc.mu.RUnlock()

	if tabCtx == nil {
		return fmt.Errorf("not connected — call Connect() first")
	}

	timeoutCtx, timeoutCancel := context.WithTimeout(tabCtx, timeout)
	defer timeoutCancel()

	return fn(timeoutCtx)
}

// Screenshot takes a screenshot via CDP and returns raw PNG bytes.
func (sbc *SandboxBrowserClient) Screenshot(ctx context.Context) ([]byte, error) {
	var screenshotBuf []byte
	err := sbc.runInTab(defaultTimeout, func(actCtx context.Context) error {
		return chromedp.Run(actCtx, chromedp.ActionFunc(func(ctx context.Context) error {
			var err error
			screenshotBuf, err = page.CaptureScreenshot().Do(ctx)
			return err
		}))
	})
	if err != nil {
		return nil, fmt.Errorf("screenshot failed: %w", err)
	}

	sbc.mu.Lock()
	sbc.lastScreenshot = screenshotBuf
	sbc.mu.Unlock()

	return screenshotBuf, nil
}

// Navigate navigates to a URL, labels elements, and takes a screenshot.
func (sbc *SandboxBrowserClient) Navigate(ctx context.Context, url string) (*PageInfo, error) {
	info, err := sbc.navigateInner(ctx, url)
	if err != nil {
		// Tab may be corrupted after a failed navigate — recreate it and retry once
		sbc.logger.Warn("navigate failed, recreating tab and retrying", zap.Error(err))
		if recErr := sbc.reconnectTab(ctx); recErr != nil {
			return nil, fmt.Errorf("navigate failed and tab recovery also failed: %w (recovery: %v)", err, recErr)
		}
		info, err = sbc.navigateInner(ctx, url)
	}
	return info, err
}

// navigateInner performs the actual navigation without retry logic.
func (sbc *SandboxBrowserClient) navigateInner(ctx context.Context, url string) (*PageInfo, error) {
	var title, currentURL string
	var screenshotBuf []byte
	var elementsJSON string

	err := sbc.runInTab(navigationTimeout, func(actCtx context.Context) error {
		// Use raw CDP Page.navigate to avoid chromedp's built-in wait-for-load
		// which hangs on heavy JS pages like google.com.
		// We just send the navigation command and wait for the page to settle.
		return chromedp.Run(actCtx,
			chromedp.ActionFunc(func(ctx context.Context) error {
				_, _, _, _, err := page.Navigate(url).Do(ctx)
				return err
			}),
			chromedp.Sleep(3*time.Second), // Wait for page to partially render
			chromedp.Title(&title),
			chromedp.Location(&currentURL),
			chromedp.Evaluate(labelerJS, &elementsJSON),
			chromedp.ActionFunc(func(ctx context.Context) error {
				var err error
				screenshotBuf, err = page.CaptureScreenshot().Do(ctx)
				return err
			}),
		)
	})
	if err != nil {
		return nil, fmt.Errorf("navigate failed: %w", err)
	}

	elements := parseElements(elementsJSON)

	sbc.mu.Lock()
	sbc.lastURL = currentURL
	sbc.lastTitle = title
	sbc.lastElements = elements
	sbc.lastScreenshot = screenshotBuf
	sbc.mu.Unlock()

	return &PageInfo{
		URL:        currentURL,
		Title:      title,
		Elements:   elements,
		Screenshot: base64.StdEncoding.EncodeToString(screenshotBuf),
	}, nil
}

// Click clicks an element by its label ID.
func (sbc *SandboxBrowserClient) Click(ctx context.Context, elementID int) (*PageInfo, error) {
	info, err := sbc.clickInner(ctx, elementID)
	if err != nil {
		sbc.logger.Warn("click failed, recreating tab and retrying", zap.Error(err))
		if recErr := sbc.reconnectTab(ctx); recErr != nil {
			return nil, fmt.Errorf("click failed and tab recovery also failed: %w (recovery: %v)", err, recErr)
		}
		info, err = sbc.clickInner(ctx, elementID)
	}
	return info, err
}

func (sbc *SandboxBrowserClient) clickInner(ctx context.Context, elementID int) (*PageInfo, error) {
	sbc.mu.RLock()
	selector := sbc.selectorForElement(elementID)
	sbc.mu.RUnlock()

	if selector == "" {
		return nil, fmt.Errorf("element %d not found", elementID)
	}

	var title, currentURL string
	var screenshotBuf []byte
	var elementsJSON string

	// Use JavaScript click instead of chromedp.Click — avoids NodeVisible
	// hang on pages loaded via raw CDP that haven't completed full load cycle.
	clickJS := fmt.Sprintf(`document.querySelector('%s') && document.querySelector('%s').click()`, selector, selector)

	err := sbc.runInTab(navigationTimeout, func(actCtx context.Context) error {
		return chromedp.Run(actCtx,
			chromedp.Evaluate(clickJS, nil),
			chromedp.Sleep(settleDelay),
			chromedp.Title(&title),
			chromedp.Location(&currentURL),
			chromedp.Evaluate(labelerJS, &elementsJSON),
			chromedp.ActionFunc(func(ctx context.Context) error {
				var err error
				screenshotBuf, err = page.CaptureScreenshot().Do(ctx)
				return err
			}),
		)
	})
	if err != nil {
		return nil, fmt.Errorf("click failed: %w", err)
	}

	elements := parseElements(elementsJSON)
	sbc.updateState(currentURL, title, elements, screenshotBuf)

	return &PageInfo{
		URL:        currentURL,
		Title:      title,
		Elements:   elements,
		Screenshot: base64.StdEncoding.EncodeToString(screenshotBuf),
	}, nil
}

// Type types text into an element by its label ID.
func (sbc *SandboxBrowserClient) Type(ctx context.Context, elementID int, text string, submit bool) (*PageInfo, error) {
	info, err := sbc.typeInner(ctx, elementID, text, submit)
	if err != nil {
		sbc.logger.Warn("type failed, recreating tab and retrying", zap.Error(err))
		if recErr := sbc.reconnectTab(ctx); recErr != nil {
			return nil, fmt.Errorf("type failed and tab recovery also failed: %w (recovery: %v)", err, recErr)
		}
		info, err = sbc.typeInner(ctx, elementID, text, submit)
	}
	return info, err
}

func (sbc *SandboxBrowserClient) typeInner(ctx context.Context, elementID int, text string, submit bool) (*PageInfo, error) {
	sbc.mu.RLock()
	selector := sbc.selectorForElement(elementID)
	sbc.mu.RUnlock()

	if selector == "" {
		return nil, fmt.Errorf("element %d not found", elementID)
	}

	var title, currentURL string
	var screenshotBuf []byte
	var elementsJSON string

	// Use JavaScript to set value and optionally submit — avoids chromedp.SendKeys
	// NodeVisible hang on pages loaded via raw CDP.
	escaped := strings.ReplaceAll(text, `\`, `\\`)
	escaped = strings.ReplaceAll(escaped, `'`, `\'`)
	escaped = strings.ReplaceAll(escaped, "\n", `\n`)
	typeJS := fmt.Sprintf(`(function(){ var el = document.querySelector('%s'); if(el){el.value='%s'; el.dispatchEvent(new Event('input',{bubbles:true})); el.dispatchEvent(new Event('change',{bubbles:true})); %s} })()`,
		selector, escaped,
		func() string {
			if submit {
				return `if(el.form){el.form.submit();}else{el.dispatchEvent(new KeyboardEvent('keydown',{key:'Enter'}));}`
			}
			return ""
		}(),
	)

	timeout := defaultTimeout
	if submit {
		timeout = navigationTimeout
	}
	err := sbc.runInTab(timeout, func(actCtx context.Context) error {
		return chromedp.Run(actCtx,
			chromedp.Evaluate(typeJS, nil),
			chromedp.Sleep(settleDelay),
			chromedp.Title(&title),
			chromedp.Location(&currentURL),
			chromedp.Evaluate(labelerJS, &elementsJSON),
			chromedp.ActionFunc(func(ctx context.Context) error {
				var err error
				screenshotBuf, err = page.CaptureScreenshot().Do(ctx)
				return err
			}),
		)
	})
	if err != nil {
		return nil, fmt.Errorf("type failed: %w", err)
	}

	elements := parseElements(elementsJSON)
	sbc.updateState(currentURL, title, elements, screenshotBuf)

	return &PageInfo{
		URL:        currentURL,
		Title:      title,
		Elements:   elements,
		Screenshot: base64.StdEncoding.EncodeToString(screenshotBuf),
	}, nil
}

// Scroll scrolls the page in a direction.
func (sbc *SandboxBrowserClient) Scroll(ctx context.Context, direction string, amount int) (*PageInfo, error) {
	info, err := sbc.scrollInner(ctx, direction, amount)
	if err != nil {
		sbc.logger.Warn("scroll failed, recreating tab and retrying", zap.Error(err))
		if recErr := sbc.reconnectTab(ctx); recErr != nil {
			return nil, fmt.Errorf("scroll failed and tab recovery also failed: %w (recovery: %v)", err, recErr)
		}
		info, err = sbc.scrollInner(ctx, direction, amount)
	}
	return info, err
}

func (sbc *SandboxBrowserClient) scrollInner(ctx context.Context, direction string, amount int) (*PageInfo, error) {
	scrollBy := amount
	if direction == "up" {
		scrollBy = -amount
	}

	var title, currentURL string
	var screenshotBuf []byte
	var elementsJSON string

	err := sbc.runInTab(defaultTimeout, func(actCtx context.Context) error {
		return chromedp.Run(actCtx,
			chromedp.Evaluate(fmt.Sprintf("window.scrollBy(0, %d)", scrollBy), nil),
			chromedp.Title(&title),
			chromedp.Location(&currentURL),
			chromedp.Evaluate(labelerJS, &elementsJSON),
			chromedp.ActionFunc(func(ctx context.Context) error {
				var err error
				screenshotBuf, err = page.CaptureScreenshot().Do(ctx)
				return err
			}),
		)
	})
	if err != nil {
		return nil, fmt.Errorf("scroll failed: %w", err)
	}

	elements := parseElements(elementsJSON)
	sbc.updateState(currentURL, title, elements, screenshotBuf)

	return &PageInfo{
		URL:        currentURL,
		Title:      title,
		Elements:   elements,
		Screenshot: base64.StdEncoding.EncodeToString(screenshotBuf),
	}, nil
}

// GetSnapshot returns cached URL, title, and elements.
func (sbc *SandboxBrowserClient) GetSnapshot() (*PageInfo, error) {
	sbc.mu.RLock()
	defer sbc.mu.RUnlock()

	return &PageInfo{
		URL:      sbc.lastURL,
		Title:    sbc.lastTitle,
		Elements: sbc.lastElements,
	}, nil
}

// Close closes the persistent tab and the allocator.
func (sbc *SandboxBrowserClient) Close() {
	sbc.mu.Lock()
	defer sbc.mu.Unlock()

	if sbc.tabCancel != nil {
		sbc.tabCancel()
		sbc.tabCtx = nil
		sbc.tabCancel = nil
	}
	if sbc.allocCancel != nil {
		sbc.allocCancel()
		sbc.allocator = nil
		sbc.allocCancel = nil
	}
	sbc.logger.Info("sandbox browser client closed", zap.Int("cdp_port", sbc.cdpPort))
}

// reconnectTab closes the current tab and creates a fresh one.
// Used to recover from corrupted tab state after a failed navigation.
func (sbc *SandboxBrowserClient) reconnectTab(ctx context.Context) error {
	sbc.mu.Lock()
	defer sbc.mu.Unlock()

	// Close old tab
	if sbc.tabCancel != nil {
		sbc.tabCancel()
		sbc.tabCtx = nil
		sbc.tabCancel = nil
	}
	// Close old allocator
	if sbc.allocCancel != nil {
		sbc.allocCancel()
		sbc.allocator = nil
		sbc.allocCancel = nil
	}

	// Create fresh allocator and tab
	allocCtx, allocCancel := chromedp.NewRemoteAllocator(context.Background(), sbc.wsURL)
	tabCtx, tabCancel := chromedp.NewContext(allocCtx)
	if err := chromedp.Run(tabCtx, chromedp.Navigate("about:blank")); err != nil {
		tabCancel()
		allocCancel()
		return fmt.Errorf("CDP reconnection test failed: %w", err)
	}

	sbc.allocator = allocCtx
	sbc.allocCancel = allocCancel
	sbc.tabCtx = tabCtx
	sbc.tabCancel = tabCancel

	sbc.logger.Info("tab recreated successfully", zap.Int("cdp_port", sbc.cdpPort))
	return nil
}

// IsConnected returns whether the client has an active tab.
func (sbc *SandboxBrowserClient) IsConnected() bool {
	sbc.mu.RLock()
	defer sbc.mu.RUnlock()
	if sbc.tabCtx == nil {
		return false
	}
	// Check if the tab context has been cancelled (e.g. container died)
	return sbc.tabCtx.Err() == nil
}

// updateState caches the current page state.
func (sbc *SandboxBrowserClient) updateState(url, title string, elements []LabeledElement, screenshot []byte) {
	sbc.mu.Lock()
	sbc.lastURL = url
	sbc.lastTitle = title
	sbc.lastElements = elements
	sbc.lastScreenshot = screenshot
	sbc.mu.Unlock()
}

// selectorForElement finds the CSS selector for a labeled element.
func (sbc *SandboxBrowserClient) selectorForElement(elementID int) string {
	for _, el := range sbc.lastElements {
		if el.ID == elementID {
			return el.Selector
		}
	}
	return ""
}
