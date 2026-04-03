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

// SandboxBrowserClient manages a browser session connected to a sandbox Chrome
// instance via Chrome DevTools Protocol (CDP). It reuses the same labeling,
// element parsing, and screenshot patterns as BrowserClient but connects to
// sandbox Chrome via a CDP port instead of Browserless WebSocket.
type SandboxBrowserClient struct {
	cdpPort int
	wsURL   string // derived: ws://localhost:<cdpPort>
	logger  *zap.Logger
	session *Session
	mu      sync.RWMutex
}

// NewSandboxBrowserClient creates a new browser client for a sandbox Chrome instance
func NewSandboxBrowserClient(cdpPort int, logger *zap.Logger) (*SandboxBrowserClient, error) {
	if cdpPort <= 0 {
		return nil, fmt.Errorf("cdp port must be positive, got %d", cdpPort)
	}

	wsURL := fmt.Sprintf("ws://localhost:%d", cdpPort)

	return &SandboxBrowserClient{
		cdpPort: cdpPort,
		wsURL:   wsURL,
		logger:  logger,
	}, nil
}

// Connect creates the ChromeDP remote allocator and tab context.
func (sbc *SandboxBrowserClient) Connect(ctx context.Context) error {
	sbc.mu.Lock()
	defer sbc.mu.Unlock()

	if sbc.session != nil {
		return nil // already connected
	}

	// Create allocator pointing to sandbox Chrome via CDP
	allocCtx, allocCancel := chromedp.NewRemoteAllocator(context.Background(), sbc.wsURL)

	// Create a new tab context
	tabCtx, tabCancel := chromedp.NewContext(allocCtx)

	combinedCancel := func() {
		tabCancel()
		allocCancel()
	}

	sbc.session = &Session{
		ID:     "sandbox",
		Ctx:    tabCtx,
		Cancel: combinedCancel,
	}

	sbc.logger.Info("sandbox browser client connected", zap.Int("cdp_port", sbc.cdpPort))
	return nil
}

// actionContext derives a timeout context from the session's persistent tab context.
func (sbc *SandboxBrowserClient) actionContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(sbc.session.Ctx, 30*time.Second)
}

// Screenshot takes a screenshot via CDP and returns raw PNG bytes.
func (sbc *SandboxBrowserClient) Screenshot(ctx context.Context) ([]byte, error) {
	if err := sbc.ensureConnected(); err != nil {
		return nil, err
	}

	sbc.mu.RLock()
	sess := sbc.session
	sbc.mu.RUnlock()

	actCtx, actCancel := sbc.actionContext()
	defer actCancel()

	var screenshotBuf []byte
	if err := chromedp.Run(actCtx, chromedp.ActionFunc(func(ctx context.Context) error {
		var err error
		screenshotBuf, err = page.CaptureScreenshot().Do(ctx)
		return err
	})); err != nil {
		return nil, fmt.Errorf("screenshot failed: %w", err)
	}

	sbc.mu.Lock()
	sess.Screenshot = screenshotBuf
	sbc.mu.Unlock()

	return screenshotBuf, nil
}

// Navigate navigates to a URL, labels elements, and takes a screenshot.
func (sbc *SandboxBrowserClient) Navigate(ctx context.Context, url string) (*PageInfo, error) {
	if err := sbc.ensureConnected(); err != nil {
		return nil, err
	}

	sbc.mu.RLock()
	sess := sbc.session
	sbc.mu.RUnlock()

	actCtx, actCancel := sbc.actionContext()
	defer actCancel()

	var title, currentURL string
	var screenshotBuf []byte
	var elementsJSON string

	actions := []chromedp.Action{
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
	}

	if err := chromedp.Run(actCtx, actions...); err != nil {
		return nil, fmt.Errorf("navigate failed: %w", err)
	}

	elements := parseElements(elementsJSON)

	sbc.mu.Lock()
	sess.URL = currentURL
	sess.Title = title
	sess.Elements = elements
	sess.Screenshot = screenshotBuf
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
	if err := sbc.ensureConnected(); err != nil {
		return nil, err
	}

	sbc.mu.RLock()
	sess := sbc.session
	sbc.mu.RUnlock()

	selector := sbc.selectorForElement(sess, elementID)
	if selector == "" {
		return nil, fmt.Errorf("element %d not found", elementID)
	}

	actCtx, actCancel := sbc.actionContext()
	defer actCancel()

	var title, currentURL string
	var screenshotBuf []byte
	var elementsJSON string

	actions := []chromedp.Action{
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
	}

	if err := chromedp.Run(actCtx, actions...); err != nil {
		return nil, fmt.Errorf("click failed: %w", err)
	}

	elements := parseElements(elementsJSON)

	sbc.mu.Lock()
	sess.URL = currentURL
	sess.Title = title
	sess.Elements = elements
	sess.Screenshot = screenshotBuf
	sbc.mu.Unlock()

	return &PageInfo{
		URL:        currentURL,
		Title:      title,
		Elements:   elements,
		Screenshot: base64.StdEncoding.EncodeToString(screenshotBuf),
	}, nil
}

// Type types text into an element by its label ID.
func (sbc *SandboxBrowserClient) Type(ctx context.Context, elementID int, text string, submit bool) (*PageInfo, error) {
	if err := sbc.ensureConnected(); err != nil {
		return nil, err
	}

	sbc.mu.RLock()
	sess := sbc.session
	sbc.mu.RUnlock()

	selector := sbc.selectorForElement(sess, elementID)
	if selector == "" {
		return nil, fmt.Errorf("element %d not found", elementID)
	}

	actCtx, actCancel := sbc.actionContext()
	defer actCancel()

	actions := []chromedp.Action{
		chromedp.Clear(selector, chromedp.NodeVisible),
		chromedp.SendKeys(selector, text, chromedp.NodeVisible),
	}

	if submit {
		actions = append(actions, chromedp.Submit(selector))
	}

	var title, currentURL string
	var screenshotBuf []byte
	var elementsJSON string

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

	if err := chromedp.Run(actCtx, actions...); err != nil {
		return nil, fmt.Errorf("type failed: %w", err)
	}

	elements := parseElements(elementsJSON)

	sbc.mu.Lock()
	sess.URL = currentURL
	sess.Title = title
	sess.Elements = elements
	sess.Screenshot = screenshotBuf
	sbc.mu.Unlock()

	return &PageInfo{
		URL:        currentURL,
		Title:      title,
		Elements:   elements,
		Screenshot: base64.StdEncoding.EncodeToString(screenshotBuf),
	}, nil
}

// Scroll scrolls the page in a direction.
func (sbc *SandboxBrowserClient) Scroll(ctx context.Context, direction string, amount int) (*PageInfo, error) {
	if err := sbc.ensureConnected(); err != nil {
		return nil, err
	}

	sbc.mu.RLock()
	sess := sbc.session
	sbc.mu.RUnlock()

	actCtx, actCancel := sbc.actionContext()
	defer actCancel()

	scrollJS := fmt.Sprintf("window.scrollBy(0, %d)", amount)
	if direction == "up" {
		scrollJS = fmt.Sprintf("window.scrollBy(0, %d)", -amount)
	}

	var title, currentURL string
	var screenshotBuf []byte
	var elementsJSON string

	actions := []chromedp.Action{
		chromedp.Evaluate(scrollJS, nil),
		chromedp.Title(&title),
		chromedp.Location(&currentURL),
		chromedp.Evaluate(labelerJS, &elementsJSON),
		chromedp.ActionFunc(func(ctx context.Context) error {
			var err error
			screenshotBuf, err = page.CaptureScreenshot().Do(ctx)
			return err
		}),
	}

	if err := chromedp.Run(actCtx, actions...); err != nil {
		return nil, fmt.Errorf("scroll failed: %w", err)
	}

	elements := parseElements(elementsJSON)

	sbc.mu.Lock()
	sess.URL = currentURL
	sess.Title = title
	sess.Elements = elements
	sess.Screenshot = screenshotBuf
	sbc.mu.Unlock()

	return &PageInfo{
		URL:        currentURL,
		Title:      title,
		Elements:   elements,
		Screenshot: base64.StdEncoding.EncodeToString(screenshotBuf),
	}, nil
}

// GetSnapshot returns current URL, title, and elements without a new screenshot.
func (sbc *SandboxBrowserClient) GetSnapshot() (*PageInfo, error) {
	sbc.mu.RLock()
	defer sbc.mu.RUnlock()

	if sbc.session == nil {
		return nil, fmt.Errorf("not connected")
	}

	return &PageInfo{
		URL:      sbc.session.URL,
		Title:    sbc.session.Title,
		Elements: sbc.session.Elements,
	}, nil
}

// GetCachedScreenshot returns the cached screenshot as raw PNG.
func (sbc *SandboxBrowserClient) GetCachedScreenshot() ([]byte, error) {
	sbc.mu.RLock()
	defer sbc.mu.RUnlock()

	if sbc.session == nil {
		return nil, fmt.Errorf("not connected")
	}

	if len(sbc.session.Screenshot) == 0 {
		return nil, fmt.Errorf("no screenshot available")
	}

	return sbc.session.Screenshot, nil
}

// Close closes the browser session.
func (sbc *SandboxBrowserClient) Close() {
	sbc.mu.Lock()
	defer sbc.mu.Unlock()

	if sbc.session != nil {
		sbc.session.Cancel()
		sbc.session = nil
		sbc.logger.Info("sandbox browser client closed", zap.Int("cdp_port", sbc.cdpPort))
	}
}

// IsConnected returns whether the client has an active session.
func (sbc *SandboxBrowserClient) IsConnected() bool {
	sbc.mu.RLock()
	defer sbc.mu.RUnlock()
	return sbc.session != nil
}

// ensureConnected checks the session is ready.
func (sbc *SandboxBrowserClient) ensureConnected() error {
	sbc.mu.RLock()
	defer sbc.mu.RUnlock()
	if sbc.session == nil {
		return fmt.Errorf("sandbox browser not connected — call Connect() first")
	}
	return nil
}

// selectorForElement finds the CSS selector for a labeled element.
func (sbc *SandboxBrowserClient) selectorForElement(sess *Session, elementID int) string {
	for _, el := range sess.Elements {
		if el.ID == elementID {
			return el.Selector
		}
	}
	return ""
}
