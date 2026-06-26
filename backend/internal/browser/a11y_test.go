package browser

import (
	"context"
	"testing"
	"time"

	"github.com/chromedp/cdproto/accessibility"
	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/dom"
	"github.com/chromedp/cdproto/target"
	"github.com/chromedp/chromedp"
)

func TestRealA11yExtraction(t *testing.T) {
	// Connect to the sandbox Chrome at localhost:19222
	wsURL := "ws://localhost:19222"
	allocCtx, allocCancel := chromedp.NewRemoteAllocator(context.Background(), wsURL)
	defer allocCancel()

	// Create a temp context to list targets
	tmpCtx, tmpCancel := chromedp.NewContext(allocCtx)
	defer tmpCancel()

	targets, err := chromedp.Targets(tmpCtx)
	if err != nil {
		t.Skipf("can't connect to Chrome CDP: %v", err)
	}

	var pageTargetID string
	for _, tgt := range targets {
		if tgt.Type == "page" && tgt.URL != "" && tgt.URL != "about:blank" {
			pageTargetID = string(tgt.TargetID)
			break
		}
	}
	if pageTargetID == "" {
		t.Fatal("no page target found")
	}

	ctx, cancel := chromedp.NewContext(allocCtx, chromedp.WithTargetID(target.ID(pageTargetID)))
	defer cancel()
	ctx, timeoutCancel := context.WithTimeout(ctx, 15*time.Second)
	defer timeoutCancel()

	// Navigate to a test page
	var title, currentURL string
	err = chromedp.Run(ctx,
		chromedp.Navigate("https://www.google.com"),
		chromedp.Sleep(2*time.Second),
		chromedp.Title(&title),
		chromedp.Location(&currentURL),
	)
	if err != nil {
		t.Fatalf("navigate failed: %v", err)
	}
	t.Logf("Page: %s (%s)", title, currentURL)

	// Step 1: Get raw accessibility tree
	var nodes []*accessibility.Node
	err = chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		var err error
		nodes, err = accessibility.GetFullAXTree().Do(ctx)
		return err
	}))
	if err != nil {
		t.Fatalf("GetFullAXTree failed: %v", err)
	}
	t.Logf("Raw AX nodes: %d", len(nodes))

	// Count by role
	roleCount := map[string]int{}
	for _, n := range nodes {
		if n.Ignored {
			continue
		}
		r := stringFromValue(n.Role)
		roleCount[r]++
	}
	for r, c := range roleCount {
		t.Logf("  role=%s count=%d", r, c)
	}

	// Step 2: Resolve selectors for interactive nodes
	interactiveRoles := map[string]bool{
		"button": true, "link": true, "textbox": true, "searchbox": true,
		"combobox": true, "listbox": true, "menuitem": true,
		"radio": true, "checkbox": true, "tab": true,
	}

	resolved := 0
	failed := 0
	// dom.DescribeNode().Do(ctx) needs an active chromedp executor in ctx.
	// After chromedp.Run returns, the executor wrapper may have been torn down
	// (chromedp closes the per-run listener), so bare .Do(ctx) calls fail with
	// "invalid context". Wrap the whole resolution loop in a single Run/ActionFunc
	// so every .Do() gets a live executor.
	err = chromedp.Run(ctx, chromedp.ActionFunc(func(runCtx context.Context) error {
		for _, n := range nodes {
			if n.Ignored || n.BackendDOMNodeID == 0 {
				continue
			}
			role := stringFromValue(n.Role)
			if !interactiveRoles[role] {
				continue
			}

			name := stringFromValue(n.Name)
			domNode, dErr := dom.DescribeNode().
				WithBackendNodeID(cdp.BackendNodeID(n.BackendDOMNodeID)).
				WithDepth(0).
				Do(runCtx)
			if dErr != nil {
				t.Logf("  FAILED dom.DescribeNode for %s backendID=%d: %v", role, n.BackendDOMNodeID, dErr)
				failed++
				continue
			}

			sel := selectorFromDOMNode(domNode)
			nameTrunc := name
			if len(nameTrunc) > 30 {
				nameTrunc = nameTrunc[:30]
			}
			t.Logf("  OK %s name=%q tag=%s selector=%s", role, nameTrunc, domNode.LocalName, sel)
			resolved++
		}
		return nil
	}))
	if err != nil {
		t.Fatalf("resolution run failed: %v", err)
	}
	t.Logf("Resolved: %d, Failed: %d", resolved, failed)

	if resolved == 0 {
		t.Error("no interactive elements resolved — selector resolution is broken")
	}
}
