package browser

import (
	"context"
	"encoding/base64"
	"fmt"
	"sync"
	"time"

	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
	"go.uber.org/zap"
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
	err := sbc.runInTab(30*time.Second, func(actCtx context.Context) error {
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
	var title, currentURL string
	var screenshotBuf []byte
	var elementsJSON string

	err := sbc.runInTab(30*time.Second, func(actCtx context.Context) error {
		return chromedp.Run(actCtx,
			chromedp.Navigate(url),
			chromedp.WaitReady("body"),
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
	sbc.mu.RLock()
	selector := sbc.selectorForElement(elementID)
	sbc.mu.RUnlock()

	if selector == "" {
		return nil, fmt.Errorf("element %d not found", elementID)
	}

	var title, currentURL string
	var screenshotBuf []byte
	var elementsJSON string

	err := sbc.runInTab(30*time.Second, func(actCtx context.Context) error {
		return chromedp.Run(actCtx,
			chromedp.Click(selector, chromedp.NodeVisible),
			chromedp.WaitReady("body"),
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
	sbc.mu.RLock()
	selector := sbc.selectorForElement(elementID)
	sbc.mu.RUnlock()

	if selector == "" {
		return nil, fmt.Errorf("element %d not found", elementID)
	}

	var title, currentURL string
	var screenshotBuf []byte
	var elementsJSON string

	err := sbc.runInTab(30*time.Second, func(actCtx context.Context) error {
		actions := []chromedp.Action{
			chromedp.Clear(selector, chromedp.NodeVisible),
			chromedp.SendKeys(selector, text, chromedp.NodeVisible),
		}
		if submit {
			actions = append(actions, chromedp.Submit(selector))
		}
		actions = append(actions,
			chromedp.WaitReady("body"),
			chromedp.Title(&title),
			chromedp.Location(&currentURL),
			chromedp.Evaluate(labelerJS, &elementsJSON),
			chromedp.ActionFunc(func(ctx context.Context) error {
				var err error
				screenshotBuf, err = page.CaptureScreenshot().Do(ctx)
				return err
			}),
		)
		return chromedp.Run(actCtx, actions...)
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
	scrollBy := amount
	if direction == "up" {
		scrollBy = -amount
	}

	var title, currentURL string
	var screenshotBuf []byte
	var elementsJSON string

	err := sbc.runInTab(30*time.Second, func(actCtx context.Context) error {
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

// IsConnected returns whether the client has an active tab.
func (sbc *SandboxBrowserClient) IsConnected() bool {
	sbc.mu.RLock()
	defer sbc.mu.RUnlock()
	return sbc.tabCtx != nil
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
