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

// LabeledElement represents an interactive element on the page with its position.
type LabeledElement struct {
	ID       int    `json:"id"`
	Tag      string `json:"tag"`
	Text     string `json:"text"`
	Role     string `json:"role,omitempty"`
	Selector string `json:"selector"`
	X        int    `json:"x,omitempty"` // bounding box left
	Y        int    `json:"y,omitempty"` // bounding box top
	W        int    `json:"w,omitempty"` // bounding box width
	H        int    `json:"h,omitempty"` // bounding box height
	Parent   string `json:"parent,omitempty"` // nearest container tag (form, nav, main, etc.)
}

// PageInfo contains the result of a browser action
type PageInfo struct {
	URL        string           `json:"url"`
	Title      string           `json:"title"`
	Elements   []LabeledElement `json:"elements"`
	Screenshot string           `json:"screenshot,omitempty"` // base64 PNG
}

// Session holds state for one browser tab
type Session struct {
	ID         string
	Ctx        context.Context // persistent chromedp tab context
	Cancel     context.CancelFunc
	URL        string
	Title      string
	Elements   []LabeledElement
	Screenshot []byte // raw PNG
}

// BrowserClient manages browser sessions via chromedp
type BrowserClient struct {
	wsURL    string
	logger   *zap.Logger
	sessions map[string]*Session
	mu       sync.RWMutex
}

// NewBrowserClient creates a new browser client connected to a remote Browserless instance
func NewBrowserClient(wsURL string, logger *zap.Logger) (*BrowserClient, error) {
	if wsURL == "" {
		return nil, fmt.Errorf("BROWSERLESS_URL is required")
	}

	return &BrowserClient{
		wsURL:    wsURL,
		logger:   logger,
		sessions: make(map[string]*Session),
	}, nil
}

// CreateSession opens a new browser tab that persists across actions
func (bc *BrowserClient) CreateSession(ctx context.Context, sessionID string) error {
	bc.mu.Lock()
	defer bc.mu.Unlock()

	if _, exists := bc.sessions[sessionID]; exists {
		return nil // idempotent
	}

	// Create allocator pointing to remote Browserless
	allocCtx, allocCancel := chromedp.NewRemoteAllocator(context.Background(), bc.wsURL)

	// Create a new tab context — this persists for the session lifetime
	tabCtx, tabCancel := chromedp.NewContext(allocCtx)

	combinedCancel := func() {
		tabCancel()
		allocCancel()
	}

	bc.sessions[sessionID] = &Session{
		ID:     sessionID,
		Ctx:    tabCtx,
		Cancel: combinedCancel,
	}

	bc.logger.Info("browser session created", zap.String("session_id", sessionID))
	return nil
}

// actionContext derives a timeout context from the session's persistent tab context.
// This allows each action to have its own deadline while keeping the tab alive.
func (bc *BrowserClient) actionContext(sess *Session) (context.Context, context.CancelFunc) {
	return context.WithTimeout(sess.Ctx, 30*time.Second)
}

// Navigate navigates to a URL, labels elements, and takes a screenshot
func (bc *BrowserClient) Navigate(ctx context.Context, sessionID, url string) (*PageInfo, error) {
	sess, err := bc.getSession(sessionID)
	if err != nil {
		return nil, err
	}

	actCtx, actCancel := bc.actionContext(sess)
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

	bc.mu.Lock()
	sess.URL = currentURL
	sess.Title = title
	sess.Elements = elements
	sess.Screenshot = screenshotBuf
	bc.mu.Unlock()

	return &PageInfo{
		URL:        currentURL,
		Title:      title,
		Elements:   elements,
		Screenshot: base64.StdEncoding.EncodeToString(screenshotBuf),
	}, nil
}

// Click clicks an element by its label ID
func (bc *BrowserClient) Click(ctx context.Context, sessionID string, elementID int) (*PageInfo, error) {
	sess, err := bc.getSession(sessionID)
	if err != nil {
		return nil, err
	}

	selector := bc.selectorForElement(sess, elementID)
	if selector == "" {
		return nil, fmt.Errorf("element %d not found", elementID)
	}

	actCtx, actCancel := bc.actionContext(sess)
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

	bc.mu.Lock()
	sess.URL = currentURL
	sess.Title = title
	sess.Elements = elements
	sess.Screenshot = screenshotBuf
	bc.mu.Unlock()

	return &PageInfo{
		URL:        currentURL,
		Title:      title,
		Elements:   elements,
		Screenshot: base64.StdEncoding.EncodeToString(screenshotBuf),
	}, nil
}

// Type types text into an element by its label ID
func (bc *BrowserClient) Type(ctx context.Context, sessionID string, elementID int, text string, submit bool) (*PageInfo, error) {
	sess, err := bc.getSession(sessionID)
	if err != nil {
		return nil, err
	}

	selector := bc.selectorForElement(sess, elementID)
	if selector == "" {
		return nil, fmt.Errorf("element %d not found", elementID)
	}

	actCtx, actCancel := bc.actionContext(sess)
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

	bc.mu.Lock()
	sess.URL = currentURL
	sess.Title = title
	sess.Elements = elements
	sess.Screenshot = screenshotBuf
	bc.mu.Unlock()

	return &PageInfo{
		URL:        currentURL,
		Title:      title,
		Elements:   elements,
		Screenshot: base64.StdEncoding.EncodeToString(screenshotBuf),
	}, nil
}

// Scroll scrolls the page in a direction
func (bc *BrowserClient) Scroll(ctx context.Context, sessionID, direction string, amount int) (*PageInfo, error) {
	sess, err := bc.getSession(sessionID)
	if err != nil {
		return nil, err
	}

	actCtx, actCancel := bc.actionContext(sess)
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

	bc.mu.Lock()
	sess.URL = currentURL
	sess.Title = title
	sess.Elements = elements
	sess.Screenshot = screenshotBuf
	bc.mu.Unlock()

	return &PageInfo{
		URL:        currentURL,
		Title:      title,
		Elements:   elements,
		Screenshot: base64.StdEncoding.EncodeToString(screenshotBuf),
	}, nil
}

// GetScreenshot returns the cached screenshot for a session (raw PNG)
func (bc *BrowserClient) GetScreenshot(sessionID string) ([]byte, error) {
	bc.mu.RLock()
	defer bc.mu.RUnlock()

	sess, exists := bc.sessions[sessionID]
	if !exists {
		return nil, fmt.Errorf("session %s not found", sessionID)
	}

	if len(sess.Screenshot) == 0 {
		return nil, fmt.Errorf("no screenshot available for session %s", sessionID)
	}

	return sess.Screenshot, nil
}

// GetState returns current URL, title, and elements without a new screenshot
func (bc *BrowserClient) GetState(sessionID string) (*PageInfo, error) {
	bc.mu.RLock()
	defer bc.mu.RUnlock()

	sess, exists := bc.sessions[sessionID]
	if !exists {
		return nil, fmt.Errorf("session %s not found", sessionID)
	}

	return &PageInfo{
		URL:      sess.URL,
		Title:    sess.Title,
		Elements: sess.Elements,
	}, nil
}

// CloseSession closes a browser tab
func (bc *BrowserClient) CloseSession(sessionID string) error {
	bc.mu.Lock()
	defer bc.mu.Unlock()

	sess, exists := bc.sessions[sessionID]
	if !exists {
		return nil
	}

	sess.Cancel()
	delete(bc.sessions, sessionID)

	bc.logger.Info("browser session closed", zap.String("session_id", sessionID))
	return nil
}

// Shutdown closes all sessions
func (bc *BrowserClient) Shutdown() {
	bc.mu.Lock()
	defer bc.mu.Unlock()

	for id, sess := range bc.sessions {
		sess.Cancel()
		delete(bc.sessions, id)
	}

	bc.logger.Info("browser client shutdown, all sessions closed")
}

// getSession returns a session by ID
func (bc *BrowserClient) getSession(sessionID string) (*Session, error) {
	bc.mu.RLock()
	defer bc.mu.RUnlock()

	sess, exists := bc.sessions[sessionID]
	if !exists {
		return nil, fmt.Errorf("session %s not found", sessionID)
	}
	return sess, nil
}

// selectorForElement finds the CSS selector for a labeled element
func (bc *BrowserClient) selectorForElement(sess *Session, elementID int) string {
	for _, el := range sess.Elements {
		if el.ID == elementID {
			return el.Selector
		}
	}
	return ""
}
