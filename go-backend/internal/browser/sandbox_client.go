package browser

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/auto-developer-orchestrator/backend/internal/framestream"
	"github.com/auto-developer-orchestrator/backend/internal/retry"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/cdproto/target"
	"github.com/chromedp/chromedp"
	"go.uber.org/zap"
)

const (
	defaultTimeout     = 30 * time.Second
	navigationTimeout  = 60 * time.Second
	settleDelay        = 1 * time.Second // pause after navigation for dynamic content
)

// imageExtractorJS extracts image URLs from the current page.
// Returns JSON array of src strings (up to 50, skipping data: URIs and SVGs).
const imageExtractorJS = `
(function(){
	var imgs = document.querySelectorAll('img[src]');
	var urls = [];
	for (var i = 0; i < imgs.length; i++) {
		var src = imgs[i].src || imgs[i].getAttribute('src') || '';
		if (src && !src.startsWith('data:') && !src.startsWith('blob:') && src.length < 2000) {
			urls.push(src);
			if (urls.length >= 50) break;
		}
	}
	return JSON.stringify(urls);
})()
`

// SandboxBrowserClient manages a browser connection to a sandbox Chrome instance
// via Chrome DevTools Protocol (CDP).
//
// STRATEGY: We keep ONE long-lived Chrome tab (the one the user sees in VNC)
// but create a FRESH chromedp session for each action. This is necessary because
// chromedp's internal state gets corrupted when a page navigates — the CDP session
// attached to the target becomes stale and all subsequent calls hang.
//
// Each action:
//  1. Creates a fresh chromedp context via NewContext(allocator)
//  2. That automatically creates a new Chrome tab — NOT what we want
//  3. So instead: we navigate the EXISTING tab via raw CDP HTTP (fetch /json/list,
//     find our tab, send Page.navigate via WebSocket), then attach a fresh chromedp
//     session to that tab for post-navigation queries.
//
// For non-navigation actions (click, type, scroll), we use the simpler approach of
// a fresh tab each time since they don't corrupt the session.
type SandboxBrowserClient struct {
	cdpPort     int
	wsURL       string // ws://hostname:port for Chrome CDP
	logger      *zap.Logger
	allocator   context.Context
	allocCancel context.CancelFunc

	// The target ID of the Chrome tab we're currently using.
	// Updated on each Navigate to point to the new tab.
	persistentTargetID string

	// Keep-alive references for the current navigated tab.
	// Cancelling keepAliveCancel will close the Chrome tab.
	keepAliveCtx    context.Context
	keepAliveCancel context.CancelFunc

	// Cached state from last action
	lastURL          string
	lastTitle        string
	lastElements     []LabeledElement
	lastScreenshot   []byte
	lastA11yElements []AccessibleElement

	// Frame streamer for continuous visual monitoring
	streamer *framestream.Streamer

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

// Connect verifies Chrome CDP is reachable, creates the allocator, and
// finds or creates a persistent tab. The tab is the one the user sees in VNC.
func (sbc *SandboxBrowserClient) Connect(ctx context.Context) error {
	sbc.mu.Lock()
	defer sbc.mu.Unlock()

	if sbc.allocator != nil {
		return nil
	}

	allocCtx, allocCancel := chromedp.NewRemoteAllocator(context.Background(), sbc.wsURL)
	sbc.allocator = allocCtx
	sbc.allocCancel = allocCancel

	// Find an existing page target or create one
	targetID, err := sbc.findOrCreateTarget(ctx)
	if err != nil {
		allocCancel()
		sbc.allocator = nil
		return fmt.Errorf("CDP connection test failed: %w", err)
	}

	sbc.persistentTargetID = targetID
	sbc.logger.Info("sandbox browser client connected",
		zap.Int("cdp_port", sbc.cdpPort),
		zap.String("targetID", targetID))
	return nil
}

// cdpHTTPPut makes a PUT request to the Chrome CDP HTTP API.
// Used for /json/new endpoint which requires PUT method.
func (sbc *SandboxBrowserClient) cdpHTTPPut(url string) (map[string]interface{}, error) {
	req, err := http.NewRequest(http.MethodPut, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("CDP HTTP PUT: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("decode response: %w (body: %s)", err, string(body[:min(len(body), 200)]))
	}
	return result, nil
}

// findOrCreateTarget finds an existing page target in Chrome or creates a new one.
// Returns the target ID string.
func (sbc *SandboxBrowserClient) findOrCreateTarget(ctx context.Context) (string, error) {
	// Create a temporary context to list targets
	tmpCtx, tmpCancel := chromedp.NewContext(sbc.allocator)
	defer tmpCancel()

	targets, err := chromedp.Targets(tmpCtx)
	if err != nil {
		return "", fmt.Errorf("list targets: %w", err)
	}

	// Find first page-type target that's NOT about:blank or the landing page
	for _, t := range targets {
		if t.Type == "page" && t.URL != "" && t.URL != "about:blank" && t.URL != "about:blank#" {
			return string(t.TargetID), nil
		}
	}

	// No existing target — create one by navigating
	if err := chromedp.Run(tmpCtx, chromedp.Navigate("about:blank")); err != nil {
		return "", fmt.Errorf("create target: %w", err)
	}

	// Get the target ID of the tab we just created
	bt := chromedp.FromContext(tmpCtx)
	if bt == nil || bt.Target == nil {
		return "", fmt.Errorf("no target in context")
	}
	return string(bt.Target.TargetID), nil
}

// runOnActiveTab runs fn on the currently active Chrome tab.
// It reuses the keepAlive context (which keeps the tab alive) with a timeout.
func (sbc *SandboxBrowserClient) runOnActiveTab(timeout time.Duration, fn func(ctx context.Context) error) error {
	sbc.mu.RLock()
	ctx := sbc.keepAliveCtx
	sbc.mu.RUnlock()

	if ctx == nil {
		return fmt.Errorf("no active tab — navigate first")
	}

	timeoutCtx, timeoutCancel := context.WithTimeout(ctx, timeout)
	defer timeoutCancel()

	return fn(timeoutCtx)
}

// Screenshot takes a screenshot via CDP and returns raw PNG bytes.
func (sbc *SandboxBrowserClient) Screenshot(ctx context.Context) ([]byte, error) {
	var screenshotBuf []byte
	err := sbc.runOnActiveTab(defaultTimeout, func(actCtx context.Context) error {
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

// ScreenshotRaw captures raw PNG bytes via CDP without updating cached state.
// Used by the frame streamer for lightweight background captures.
func (sbc *SandboxBrowserClient) ScreenshotRaw(ctx context.Context) ([]byte, error) {
	var buf []byte
	err := sbc.runOnActiveTab(10*time.Second, func(actCtx context.Context) error {
		return chromedp.Run(actCtx, chromedp.ActionFunc(func(ctx context.Context) error {
			var err error
			buf, err = page.CaptureScreenshot().Do(ctx)
			return err
		}))
	})
	return buf, err
}

// StartStream begins continuous frame capture at the configured FPS.
func (sbc *SandboxBrowserClient) StartStream(ctx context.Context, cfg framestream.Config) {
	sbc.mu.Lock()
	defer sbc.mu.Unlock()

	if sbc.streamer != nil && sbc.streamer.IsRunning() {
		return
	}

	capture := func(capCtx context.Context) ([]byte, error) {
		return sbc.ScreenshotRaw(capCtx)
	}

	sbc.streamer = framestream.NewStreamer(cfg, capture)
	if err := sbc.streamer.Start(ctx); err != nil {
		sbc.logger.Warn("frame stream failed to start", zap.Error(err))
		sbc.streamer = nil
		return
	}
	sbc.logger.Info("frame stream started", zap.Float64("fps", cfg.FPS))
}

// StopStream stops the frame streamer if running.
func (sbc *SandboxBrowserClient) StopStream() {
	sbc.mu.Lock()
	s := sbc.streamer
	sbc.streamer = nil
	sbc.mu.Unlock()

	if s != nil {
		s.Stop()
		sbc.logger.Info("frame stream stopped")
	}
}

// GetStreamer returns the active frame streamer, or nil.
func (sbc *SandboxBrowserClient) GetStreamer() *framestream.Streamer {
	sbc.mu.RLock()
	defer sbc.mu.RUnlock()
	return sbc.streamer
}

// Navigate navigates to a URL, labels elements, and takes a screenshot.
// Creates a new Chrome tab for the navigation and keeps it alive for subsequent actions.
// Also enriches results with accessibility tree elements when available.
func (sbc *SandboxBrowserClient) Navigate(ctx context.Context, url string) (*PageInfo, error) {
	info, err := retry.DoWithResult(ctx, retry.Short, func() (*PageInfo, error) {
		info, err := sbc.navigateInner(ctx, url)
		if err != nil {
			sbc.logger.Warn("navigate failed, reconnecting for retry", zap.Error(err))
			if recErr := sbc.reconnect(ctx); recErr != nil {
				return nil, fmt.Errorf("navigate failed and recovery also failed: %w (recovery: %v)", err, recErr)
			}
			return sbc.navigateInner(ctx, url)
		}
		return info, nil
	})
	if info != nil {
		sbc.enrichA11yBackground(ctx, info)
	}
	return info, err
}

// navigateInner creates a new Chrome tab via HTTP CDP API, navigates it,
// then attaches a fresh chromedp context to the navigated tab for data collection.
func (sbc *SandboxBrowserClient) navigateInner(ctx context.Context, url string) (*PageInfo, error) {
	var title, currentURL string
	var screenshotBuf []byte
	var elementsJSON string
	var imageURLsJSON string

	sbc.mu.RLock()
	allocCtx := sbc.allocator
	oldTargetID := sbc.persistentTargetID
	sbc.mu.RUnlock()

	if allocCtx == nil {
		return nil, fmt.Errorf("not connected — call Connect() first")
	}

	// Step 1: Create a blank tab via CDP HTTP API, then navigate with chromedp.
	// Using /json/new (without URL) avoids async navigation issues.
	// chromedp.Navigate properly waits for the page load event.
	httpURL := fmt.Sprintf("http://%s/json/new", sbc.wsURL[len("ws://"):])
	sbc.logger.Info("navigateInner: creating tab via HTTP API", zap.String("httpURL", httpURL))

	createResp, err := sbc.cdpHTTPPut(httpURL)
	if err != nil {
		return nil, fmt.Errorf("create tab: %w", err)
	}
	newTargetID, _ := createResp["id"].(string)
	if newTargetID == "" {
		return nil, fmt.Errorf("create tab: no target ID in response")
	}
	sbc.logger.Info("navigateInner: tab created", zap.String("targetID", newTargetID))

	// Step 2: Attach a fresh chromedp context to the new tab
	tabCtx, tabCancel := chromedp.NewContext(allocCtx,
		chromedp.WithTargetID(target.ID(newTargetID)),
	)

	// Step 3: Navigate using chromedp (waits for page load event) then collect data.
	err = chromedp.Run(tabCtx,
		network.Enable(),                      // required for GetCookies, SetCookie, etc.
		chromedp.Navigate(url),
		chromedp.Sleep(500*time.Millisecond), // brief settle for dynamic content
		chromedp.Title(&title),
		chromedp.Location(&currentURL),
		chromedp.Evaluate(labelerJS, &elementsJSON),
		chromedp.Evaluate(imageExtractorJS, &imageURLsJSON),
		chromedp.ActionFunc(func(ctx context.Context) error {
			var err error
			screenshotBuf, err = page.CaptureScreenshot().Do(ctx)
			return err
		}),
	)

	if err != nil {
		tabCancel()
		// Clean up the new tab on error
		go func() {
			closeCtx, closeCancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer closeCancel()
			target.CloseTarget(target.ID(newTargetID)).Do(closeCtx)
		}()
		return nil, fmt.Errorf("navigate data collection failed: %w", err)
	}

	// Store the tab context to keep it alive — cancelling it would close the tab
	sbc.mu.Lock()
	if sbc.keepAliveCancel != nil {
		sbc.keepAliveCancel()
	}
	sbc.keepAliveCtx = tabCtx
	sbc.keepAliveCancel = tabCancel
	sbc.persistentTargetID = newTargetID
	sbc.mu.Unlock()

	// Close old tab
	if oldTargetID != "" && oldTargetID != newTargetID {
		go func() {
			closeCtx, closeCancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer closeCancel()
			if err := target.CloseTarget(target.ID(oldTargetID)).Do(closeCtx); err != nil {
				sbc.logger.Debug("failed to close old tab (non-fatal)", zap.String("targetID", oldTargetID), zap.Error(err))
			}
		}()
	}

	elements := parseElements(elementsJSON)
	imageURLs := parseImageURLs(imageURLsJSON)
	sbc.mu.Lock()
	sbc.lastURL = currentURL
	sbc.lastTitle = title
	sbc.lastElements = elements
	sbc.lastScreenshot = screenshotBuf
	sbc.mu.Unlock()

	sbc.logger.Info("navigated successfully",
		zap.String("url", currentURL),
		zap.String("targetID", newTargetID),
		zap.Int("elements", len(elements)),
		zap.Int("images", len(imageURLs)))

	return &PageInfo{
		URL:        currentURL,
		Title:      title,
		Elements:   elements,
		ImageURLs:  imageURLs,
		Screenshot: base64.StdEncoding.EncodeToString(screenshotBuf),
	}, nil
}

// Click clicks an element by its label ID.
func (sbc *SandboxBrowserClient) Click(ctx context.Context, elementID int) (*PageInfo, error) {
	return retry.DoWithResult(ctx, retry.Short, func() (*PageInfo, error) {
		info, err := sbc.clickInner(ctx, elementID)
		if err != nil {
			sbc.logger.Warn("click failed, reconnecting for retry", zap.Error(err))
			if recErr := sbc.reconnect(ctx); recErr != nil {
				return nil, fmt.Errorf("click failed and recovery also failed: %w (recovery: %v)", err, recErr)
			}
			return sbc.clickInner(ctx, elementID)
		}
		return info, nil
	})
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
	var imageURLsJSON string

	clickJS := fmt.Sprintf(`document.querySelector('%s') && document.querySelector('%s').click()`, selector, selector)

	err := sbc.runOnActiveTab(navigationTimeout, func(actCtx context.Context) error {
		return chromedp.Run(actCtx,
			chromedp.Evaluate(clickJS, nil),
			chromedp.Sleep(settleDelay),
			chromedp.Title(&title),
			chromedp.Location(&currentURL),
			chromedp.Evaluate(labelerJS, &elementsJSON),
			chromedp.Evaluate(imageExtractorJS, &imageURLsJSON),
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
	imageURLs := parseImageURLs(imageURLsJSON)
	sbc.updateState(currentURL, title, elements, screenshotBuf)

	return &PageInfo{
		URL:        currentURL,
		Title:      title,
		Elements:   elements,
		ImageURLs:  imageURLs,
		Screenshot: base64.StdEncoding.EncodeToString(screenshotBuf),
	}, nil
}

// Type types text into an element by its label ID.
func (sbc *SandboxBrowserClient) Type(ctx context.Context, elementID int, text string, submit bool) (*PageInfo, error) {
	return retry.DoWithResult(ctx, retry.Short, func() (*PageInfo, error) {
		info, err := sbc.typeInner(ctx, elementID, text, submit)
		if err != nil {
			sbc.logger.Warn("type failed, reconnecting for retry", zap.Error(err))
			if recErr := sbc.reconnect(ctx); recErr != nil {
				return nil, fmt.Errorf("type failed and recovery also failed: %w (recovery: %v)", err, recErr)
			}
			return sbc.typeInner(ctx, elementID, text, submit)
		}
		return info, nil
	})
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
	var imageURLsJSON string

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
	err := sbc.runOnActiveTab(timeout, func(actCtx context.Context) error {
		return chromedp.Run(actCtx,
			chromedp.Evaluate(typeJS, nil),
			chromedp.Sleep(settleDelay),
			chromedp.Title(&title),
			chromedp.Location(&currentURL),
			chromedp.Evaluate(labelerJS, &elementsJSON),
			chromedp.Evaluate(imageExtractorJS, &imageURLsJSON),
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
	imageURLs := parseImageURLs(imageURLsJSON)
	sbc.updateState(currentURL, title, elements, screenshotBuf)

	return &PageInfo{
		URL:        currentURL,
		Title:      title,
		Elements:   elements,
		ImageURLs:  imageURLs,
		Screenshot: base64.StdEncoding.EncodeToString(screenshotBuf),
	}, nil
}

// Scroll scrolls the page in a direction.
func (sbc *SandboxBrowserClient) Scroll(ctx context.Context, direction string, amount int) (*PageInfo, error) {
	return retry.DoWithResult(ctx, retry.Short, func() (*PageInfo, error) {
		info, err := sbc.scrollInner(ctx, direction, amount)
		if err != nil {
			sbc.logger.Warn("scroll failed, reconnecting for retry", zap.Error(err))
			if recErr := sbc.reconnect(ctx); recErr != nil {
				return nil, fmt.Errorf("scroll failed and recovery also failed: %w (recovery: %v)", err, recErr)
			}
			return sbc.scrollInner(ctx, direction, amount)
		}
		return info, nil
	})
}

func (sbc *SandboxBrowserClient) scrollInner(ctx context.Context, direction string, amount int) (*PageInfo, error) {
	scrollBy := amount
	if direction == "up" {
		scrollBy = -amount
	}

	var title, currentURL string
	var screenshotBuf []byte
	var elementsJSON string
	var imageURLsJSON string

	err := sbc.runOnActiveTab(defaultTimeout, func(actCtx context.Context) error {
		return chromedp.Run(actCtx,
			chromedp.Evaluate(fmt.Sprintf("window.scrollBy(0, %d)", scrollBy), nil),
			chromedp.Title(&title),
			chromedp.Location(&currentURL),
			chromedp.Evaluate(labelerJS, &elementsJSON),
			chromedp.Evaluate(imageExtractorJS, &imageURLsJSON),
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
	imageURLs := parseImageURLs(imageURLsJSON)
	sbc.updateState(currentURL, title, elements, screenshotBuf)

	return &PageInfo{
		URL:        currentURL,
		Title:      title,
		Elements:   elements,
		ImageURLs:  imageURLs,
		Screenshot: base64.StdEncoding.EncodeToString(screenshotBuf),
	}, nil
}

// ExtractPageContent evaluates JavaScript on the active tab to return page content.
// The browser is a real Chrome — it renders JS, handles cookies, bypasses most anti-bot.
// Returns raw HTML for processing by MCP process_html, or innerText as fallback.
func (sbc *SandboxBrowserClient) ExtractPageContent(ctx context.Context, rawHTML bool) (string, error) {
	var result string
	jsExpr := `document.body ? document.body.innerText : document.documentElement.outerHTML`
	if rawHTML {
		jsExpr = `document.documentElement.outerHTML`
	}
	err := sbc.runOnActiveTab(defaultTimeout, func(actCtx context.Context) error {
		return chromedp.Run(actCtx,
			chromedp.Evaluate(jsExpr, &result),
		)
	})
	if err != nil {
		return "", fmt.Errorf("extract page content: %w", err)
	}
	return result, nil
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

// Close releases the allocator connection and closes the active tab.
func (sbc *SandboxBrowserClient) Close() {
	// Stop frame streamer first (outside lock since Stop blocks)
	sbc.StopStream()

	sbc.mu.Lock()
	defer sbc.mu.Unlock()

	if sbc.keepAliveCancel != nil {
		sbc.keepAliveCancel()
		sbc.keepAliveCtx = nil
		sbc.keepAliveCancel = nil
	}
	if sbc.allocCancel != nil {
		sbc.allocCancel()
		sbc.allocator = nil
		sbc.allocCancel = nil
	}
	sbc.persistentTargetID = ""
	sbc.logger.Info("sandbox browser client closed", zap.Int("cdp_port", sbc.cdpPort))
}

// reconnect recreates the allocator and finds a fresh target.
// Closes stale Chrome tabs to prevent accumulation.
func (sbc *SandboxBrowserClient) reconnect(ctx context.Context) error {
	sbc.mu.Lock()

	// Close old tab
	if sbc.keepAliveCancel != nil {
		sbc.keepAliveCancel()
		sbc.keepAliveCtx = nil
		sbc.keepAliveCancel = nil
	}
	// Close old allocator
	if sbc.allocCancel != nil {
		sbc.allocCancel()
		sbc.allocator = nil
		sbc.allocCancel = nil
	}

	// Create fresh allocator
	allocCtx, allocCancel := chromedp.NewRemoteAllocator(context.Background(), sbc.wsURL)
	sbc.allocator = allocCtx
	sbc.allocCancel = allocCancel
	sbc.mu.Unlock()

	// Close stale tabs
	cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cleanupCancel()
	tmpCtx, tmpCancel := chromedp.NewContext(allocCtx)
	defer tmpCancel()
	if err := sbc.closeStaleTabs(cleanupCtx, tmpCtx); err != nil {
		sbc.logger.Warn("failed to close stale tabs (non-fatal)", zap.Error(err))
	}

	// Find or create a target
	targetID, err := sbc.findOrCreateTarget(ctx)
	if err != nil {
		return fmt.Errorf("reconnect failed: %w", err)
	}

	sbc.mu.Lock()
	sbc.persistentTargetID = targetID
	sbc.mu.Unlock()

	sbc.logger.Info("reconnected successfully", zap.Int("cdp_port", sbc.cdpPort), zap.String("targetID", targetID))
	return nil
}

// closeStaleTabs closes all Chrome page tabs except the current one.
func (sbc *SandboxBrowserClient) closeStaleTabs(ctx context.Context, currentTabCtx context.Context) error {
	targets, err := chromedp.Targets(currentTabCtx)
	if err != nil {
		return fmt.Errorf("list targets: %w", err)
	}

	var currentTargetID target.ID
	if bt := chromedp.FromContext(currentTabCtx); bt != nil && bt.Target != nil {
		currentTargetID = bt.Target.TargetID
	}

	// Also protect our persistent target
	sbc.mu.RLock()
	persistentID := target.ID(sbc.persistentTargetID)
	sbc.mu.RUnlock()

	closed := 0
	for _, t := range targets {
		if t.TargetID == currentTargetID || t.TargetID == persistentID || t.Type != "page" {
			continue
		}
		if err := target.CloseTarget(t.TargetID).Do(ctx); err != nil {
			sbc.logger.Debug("failed to close stale tab", zap.String("targetID", string(t.TargetID)), zap.Error(err))
		} else {
			closed++
		}
	}
	if closed > 0 {
		sbc.logger.Info("closed stale Chrome tabs", zap.Int("count", closed))
	}
	return nil
}

// IsConnected returns whether the client has an active allocator and target.
func (sbc *SandboxBrowserClient) IsConnected() bool {
	sbc.mu.RLock()
	defer sbc.mu.RUnlock()
	return sbc.allocator != nil && sbc.persistentTargetID != ""
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

// enrichA11yBackground attempts to enrich page info with accessibility tree data.
// Runs in a background goroutine — best-effort, does not block the caller.
func (sbc *SandboxBrowserClient) enrichA11yBackground(parentCtx context.Context, info *PageInfo) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = sbc.EnrichWithA11y(ctx)
		sbc.mu.RLock()
		elems := sbc.lastA11yElements
		sbc.mu.RUnlock()
		if len(elems) > 0 {
			info.A11yElements = elems
		}
	}()
}

// GetA11ySnapshot returns the accessibility tree elements from cache or fetches them.
func (sbc *SandboxBrowserClient) GetA11ySnapshot(ctx context.Context) (*A11ySnapshot, error) {
	sbc.mu.RLock()
	cached := sbc.lastA11yElements
	url := sbc.lastURL
	title := sbc.lastTitle
	sbc.mu.RUnlock()

	if len(cached) > 0 {
		return &A11ySnapshot{URL: url, Title: title, Elements: cached}, nil
	}

	return sbc.GetAccessibilityTree(ctx)
}

// GetPageInfo returns a full PageInfo including both SoM and accessibility elements.
func (sbc *SandboxBrowserClient) GetPageInfo() *PageInfo {
	sbc.mu.RLock()
	defer sbc.mu.RUnlock()
	return &PageInfo{
		URL:         sbc.lastURL,
		Title:       sbc.lastTitle,
		Elements:    sbc.lastElements,
		A11yElements: sbc.lastA11yElements,
	}
}
