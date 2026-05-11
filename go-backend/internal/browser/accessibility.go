package browser

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/chromedp/cdproto/accessibility"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

const a11yElementCap = 80

func (sbc *SandboxBrowserClient) GetAccessibilityTree(ctx context.Context) (*A11ySnapshot, error) {
	var nodes []*accessibility.Node
	var title, currentURL string

	err := sbc.runOnActiveTab(defaultTimeout, func(actCtx context.Context) error {
		return chromedp.Run(actCtx,
			chromedp.Title(&title),
			chromedp.Location(&currentURL),
			chromedp.ActionFunc(func(ctx context.Context) error {
				var err error
				nodes, err = accessibility.GetFullAXTree().Do(ctx)
				return err
			}),
		)
	})
	if err != nil {
		return nil, fmt.Errorf("get accessibility tree: %w", err)
	}

	elems := buildAccessibleElements(nodes)
	if len(elems) > a11yElementCap {
		elems = elems[:a11yElementCap]
	}

	sbc.mu.Lock()
	sbc.lastA11yElements = elems
	sbc.mu.Unlock()

	return &A11ySnapshot{
		URL:      currentURL,
		Title:    title,
		Elements: elems,
	}, nil
}

func buildAccessibleElements(nodes []*accessibility.Node) []AccessibleElement {
	nodeMap := make(map[accessibility.NodeID]*accessibility.Node, len(nodes))
	for _, n := range nodes {
		nodeMap[n.NodeID] = n
	}

	var elements []AccessibleElement
	refID := 1

	interactiveRoles := map[string]bool{
		"button": true, "link": true, "textbox": true, "searchbox": true,
		"combobox": true, "listbox": true, "menuitem": true, "menuitemcheckbox": true,
		"menuitemradio": true, "option": true, "radio": true, "checkbox": true,
		"switch": true, "tab": true, "slider": true, "spinbutton": true,
		"listitem": true, "treeitem": true, "gridcell": true, "rowheader": true,
		"columnheader": true, "row": true,
	}

	for _, node := range nodes {
		if node.Ignored {
			continue
		}
		role := stringFromValue(node.Role)
		name := stringFromValue(node.Name)
		if role == "" && name == "" {
			continue
		}
		if !interactiveRoles[role] {
			continue
		}

		desc := stringFromValue(node.Description)
		placeholder := propertyValue(node.Properties, "placeholder")
		level := 0
		if lv := propertyValue(node.Properties, "level"); lv != "" {
			fmt.Sscanf(lv, "%d", &level)
		}

		elements = append(elements, AccessibleElement{
			Ref:         fmt.Sprintf("@e%d", refID),
			Role:        role,
			Name:        name,
			Description: desc,
			Tag:         resolveTag(role),
			Selector:    buildNodeSelector(node),
			Value:       stringFromValue(node.Value),
			Placeholder: placeholder,
			Level:       level,
		})
		refID++
	}
	return elements
}

func stringFromValue(v *accessibility.Value) string {
	if v == nil {
		return ""
	}
	return string(v.Value)
}

func propertyValue(props []*accessibility.Property, name string) string {
	for _, p := range props {
		if string(p.Name) == name && p.Value != nil {
			return string(p.Value.Value)
		}
	}
	return ""
}

func resolveTag(role string) string {
	switch role {
	case "button":
		return "button"
	case "link":
		return "a"
	case "textbox", "searchbox":
		return "input"
	case "combobox":
		return "select"
	case "checkbox":
		return "input"
	case "radio":
		return "input"
	default:
		return role
	}
}

func buildNodeSelector(node *accessibility.Node) string {
	if node.BackendDOMNodeID != 0 {
		return fmt.Sprintf("[data-a11y-backend='%d']", node.BackendDOMNodeID)
	}
	return fmt.Sprintf("[data-a11y-id='%s']", node.NodeID.String())
}

func (sbc *SandboxBrowserClient) FindElement(ctx context.Context, criteria FindCriteria) (*FoundElement, error) {
	if criteria.Selector != "" {
		return sbc.findBySelector(ctx, criteria)
	}

	elements, err := sbc.getAccessibleElements(ctx)
	if err != nil {
		return nil, fmt.Errorf("find element: %w", err)
	}

	var matches []AccessibleElement
	for _, el := range elements {
		if matchesCriteria(el, criteria) {
			matches = append(matches, el)
		}
	}
	if len(matches) == 0 {
		return nil, fmt.Errorf("no element found matching criteria: role=%q name=%q label=%q",
			criteria.Role, criteria.Name, criteria.Label)
	}

	matched := matches[0]
	return &FoundElement{
		Ref:      matched.Ref,
		Role:     matched.Role,
		Name:     matched.Name,
		Selector: matched.Selector,
		Text:     matched.Name,
		Tag:      matched.Tag,
		Count:    len(matches),
	}, nil
}

func (sbc *SandboxBrowserClient) getAccessibleElements(ctx context.Context) ([]AccessibleElement, error) {
	var nodes []*accessibility.Node
	err := sbc.runOnActiveTab(defaultTimeout, func(actCtx context.Context) error {
		return chromedp.Run(actCtx, chromedp.ActionFunc(func(ctx context.Context) error {
			var err error
			nodes, err = accessibility.GetFullAXTree().Do(ctx)
			return err
		}))
	})
	if err != nil {
		return nil, err
	}
	return buildAccessibleElements(nodes), nil
}

func matchesCriteria(el AccessibleElement, c FindCriteria) bool {
	if c.Role != "" && !strings.EqualFold(el.Role, c.Role) {
		return false
	}
	if c.Name != "" && !strings.Contains(strings.ToLower(el.Name), strings.ToLower(c.Name)) {
		return false
	}
	if c.Label != "" && !strings.Contains(strings.ToLower(el.Name), strings.ToLower(c.Label)) {
		return false
	}
	if c.Text != "" && !strings.Contains(strings.ToLower(el.Name), strings.ToLower(c.Text)) {
		return false
	}
	if c.Placeholder != "" && !strings.Contains(strings.ToLower(el.Placeholder), strings.ToLower(c.Placeholder)) {
		return false
	}
	return true
}

func (sbc *SandboxBrowserClient) findBySelector(ctx context.Context, criteria FindCriteria) (*FoundElement, error) {
	selector := strings.ReplaceAll(criteria.Selector, "'", "\\'")
	js := fmt.Sprintf(`(function(){
		var el=document.querySelector('%s');
		if(!el) return JSON.stringify({found:false});
		return JSON.stringify({found:true,tag:el.tagName.toLowerCase(),text:(el.textContent||el.value||el.placeholder||'')});
	})()`, selector)

	var resultRaw string
	err := sbc.runOnActiveTab(defaultTimeout, func(actCtx context.Context) error {
		return chromedp.Run(actCtx, chromedp.Evaluate(js, &resultRaw))
	})
	if err != nil {
		return nil, fmt.Errorf("find by selector: %w", err)
	}

	var result struct {
		Found bool   `json:"found"`
		Tag   string `json:"tag"`
		Text  string `json:"text"`
	}
	if json.Unmarshal([]byte(resultRaw), &result) != nil || !result.Found {
		return nil, fmt.Errorf("no element found for selector %q", criteria.Selector)
	}
	return &FoundElement{
		Selector: criteria.Selector,
		Tag:      result.Tag,
		Text:     result.Text,
		Role:     result.Tag,
		Name:     result.Text,
		Count:    1,
	}, nil
}

func (sbc *SandboxBrowserClient) FindAndClick(ctx context.Context, criteria FindCriteria) (*PageInfo, *FoundElement, error) {
	found, err := sbc.FindElement(ctx, criteria)
	if err != nil {
		return nil, nil, err
	}
	found.Action = "click"
	return sbc.clickBySelector(ctx, found.Selector, found)
}

func (sbc *SandboxBrowserClient) FindAndType(ctx context.Context, criteria FindCriteria, text string, submit bool) (*PageInfo, *FoundElement, error) {
	found, err := sbc.FindElement(ctx, criteria)
	if err != nil {
		return nil, nil, err
	}
	found.Action = "type"
	info, err := sbc.typeBySelector(ctx, found.Selector, text, submit)
	return info, found, err
}

func (sbc *SandboxBrowserClient) clickBySelector(ctx context.Context, selector string, found *FoundElement) (*PageInfo, *FoundElement, error) {
	escaped := strings.ReplaceAll(selector, `\`, `\\`)
	escaped = strings.ReplaceAll(escaped, `'`, `\'`)

	var title, currentURL string
	var screenshotBuf []byte
	var elementsJSON string

	clickJS := fmt.Sprintf(`(function(){
		var el=document.querySelector('%s');
		if(el){el.focus();el.click();return true;}
		return false;
	})()`, escaped)

	err := sbc.runOnActiveTab(navigationTimeout, func(actCtx context.Context) error {
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
		return nil, found, fmt.Errorf("click by selector: %w", err)
	}

	elements := parseElements(elementsJSON)
	sbc.updateState(currentURL, title, elements, screenshotBuf)
	found.Action = "clicked"

	return &PageInfo{
		URL:        currentURL,
		Title:      title,
		Elements:   elements,
		Screenshot: base64.StdEncoding.EncodeToString(screenshotBuf),
	}, found, nil
}

func (sbc *SandboxBrowserClient) typeBySelector(ctx context.Context, selector, text string, submit bool) (*PageInfo, error) {
	escaped := strings.ReplaceAll(text, `\`, `\\`)
	escaped = strings.ReplaceAll(escaped, `'`, `\'`)
	escaped = strings.ReplaceAll(escaped, "\n", `\n`)

	escapedSel := strings.ReplaceAll(selector, `\`, `\\`)
	escapedSel = strings.ReplaceAll(escapedSel, `'`, `\'`)

	submitPart := ""
	if submit {
		submitPart = `if(el.form){el.form.submit();}else{el.dispatchEvent(new KeyboardEvent('keydown',{key:'Enter'}));}`
	}

	typeJS := fmt.Sprintf(`(function(){
		var el=document.querySelector('%s');
		if(!el)return false;
		var ns=Object.getOwnPropertyDescriptor(window.HTMLInputElement.prototype,'value').set;
		ns.call(el,'%s');
		el.dispatchEvent(new Event('input',{bubbles:true}));
		el.dispatchEvent(new Event('change',{bubbles:true}));
		%s
		return true;
	})()`, escapedSel, escaped, submitPart)

	timeout := defaultTimeout
	if submit {
		timeout = navigationTimeout
	}

	var title, currentURL string
	var screenshotBuf []byte
	var elementsJSON string

	err := sbc.runOnActiveTab(timeout, func(actCtx context.Context) error {
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
		return nil, fmt.Errorf("type by selector: %w", err)
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

func (sbc *SandboxBrowserClient) EnrichWithA11y(ctx context.Context) error {
	_, err := sbc.GetAccessibilityTree(ctx)
	return err
}
