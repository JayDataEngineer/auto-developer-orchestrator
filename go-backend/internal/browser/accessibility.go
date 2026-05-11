package browser

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/chromedp/cdproto/accessibility"
	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/dom"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
	"go.uber.org/zap"
)

const a11yElementCap = 80

func (sbc *SandboxBrowserClient) GetAccessibilityTree(ctx context.Context) (*A11ySnapshot, error) {
	var nodes []*accessibility.Node
	var title, currentURL string
	var selectors map[accessibility.NodeID]string

	err := sbc.runOnActiveTab(defaultTimeout, func(actCtx context.Context) error {
		return chromedp.Run(actCtx,
			chromedp.Title(&title),
			chromedp.Location(&currentURL),
			chromedp.ActionFunc(func(ctx context.Context) error {
				var err error
				nodes, err = accessibility.GetFullAXTree().Do(ctx)
				if err != nil {
					return err
				}
				sbc.logger.Info("a11y raw nodes", zap.Int("count", len(nodes)))
				selectors = resolveSelectors(ctx, nodes)
				sbc.logger.Info("a11y selectors resolved", zap.Int("count", len(selectors)))
				return nil
			}),
		)
	})
	if err != nil {
		return nil, fmt.Errorf("get accessibility tree: %w", err)
	}

	elems := buildAccessibleElements(nodes, selectors)
	// Debug: log first few non-interactive roles
	seen := map[string]bool{}
	for _, n := range nodes {
		if n.Ignored {
			continue
		}
		r := stringFromValue(n.Role)
		if !seen[r] {
			seen[r] = true
		}
	}
	roles := make([]string, 0, len(seen))
	for r := range seen {
		roles = append(roles, r)
	}
	sbc.logger.Info("a11y all roles", zap.Strings("roles", roles))
	sbc.logger.Info("a11y interactive elements", zap.Int("count", len(elems)))
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

// resolveSelectors maps each accessibility node's BackendDOMNodeID to a real
// CSS selector using dom.DescribeNode. This runs inside the CDP session.
func resolveSelectors(ctx context.Context, nodes []*accessibility.Node) map[accessibility.NodeID]string {
	result := make(map[accessibility.NodeID]string, len(nodes))
	for _, node := range nodes {
		if node.Ignored || node.BackendDOMNodeID == 0 {
			continue
		}
		domNode, err := dom.DescribeNode().
			WithBackendNodeID(cdp.BackendNodeID(node.BackendDOMNodeID)).
			WithDepth(0).
			Do(ctx)
		if err != nil {
			continue
		}
		sel := selectorFromDOMNode(domNode)
		if sel != "" {
			result[node.NodeID] = sel
		}
	}
	return result
}

// selectorFromDOMNode builds a CSS selector from a real DOM node.
// Prefers id > name > type+placeholder > tag with attribute.
func selectorFromDOMNode(n *cdp.Node) string {
	if n == nil {
		return ""
	}
	tag := strings.ToLower(n.LocalName)
	if tag == "" {
		return ""
	}

	attrs := attrMap(n.Attributes)

	// Best: unique id
	if id, ok := attrs["id"]; ok && id != "" {
		return "#" + cssEscape(id)
	}

	// Good: name attribute (common on inputs)
	if name, ok := attrs["name"]; ok && name != "" {
		return fmt.Sprintf("%s[name='%s']", tag, cssEscape(name))
	}

	// OK: placeholder-based for inputs
	if ph, ok := attrs["placeholder"]; ok && ph != "" {
		return fmt.Sprintf("%s[placeholder='%s']", tag, cssEscape(ph))
	}

	// OK: aria-label
	if al, ok := attrs["aria-label"]; ok && al != "" {
		return fmt.Sprintf("%s[aria-label='%s']", tag, cssEscape(al))
	}

	// OK: href for links
	if tag == "a" {
		if href, ok := attrs["href"]; ok && href != "" {
			return fmt.Sprintf("a[href='%s']", cssEscape(href))
		}
	}

	// OK: type for inputs
	if tag == "input" {
		if typ, ok := attrs["type"]; ok && typ != "" {
			if val, ok2 := attrs["value"]; ok2 && val != "" {
				return fmt.Sprintf("input[type='%s'][value='%s']", cssEscape(typ), cssEscape(val))
			}
			return fmt.Sprintf("input[type='%s']", cssEscape(typ))
		}
	}

	// Fallback: role attribute
	if role, ok := attrs["role"]; ok && role != "" {
		return fmt.Sprintf("%s[role='%s']", tag, cssEscape(role))
	}

	// Last resort: just the tag
	return tag
}

// attrMap converts flat [k1,v1,k2,v2,...] attribute slice to a map.
func attrMap(flat []string) map[string]string {
	m := make(map[string]string, len(flat)/2+1)
	for i := 0; i+1 < len(flat); i += 2 {
		m[flat[i]] = flat[i+1]
	}
	return m
}

// cssEscape does minimal escaping for CSS selector string values inside single quotes.
func cssEscape(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `'`, `\'`)
	return s
}

func buildAccessibleElements(nodes []*accessibility.Node, selectors map[accessibility.NodeID]string) []AccessibleElement {
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

		selector := selectors[node.NodeID]

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
			Selector:    selector,
			Value:       stringFromValue(node.Value),
			Placeholder: placeholder,
			Level:       level,
		})
		refID++
	}
	return elements
}

func stringFromValue(v *accessibility.Value) string {
	if v == nil || len(v.Value) == 0 {
		return ""
	}
	// v.Value is a jsontext.Value (raw JSON). Unmarshal to strip quotes.
	var s string
	if err := json.Unmarshal([]byte(v.Value), &s); err == nil {
		return s
	}
	return strings.Trim(string(v.Value), `"`)
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
	var selectors map[accessibility.NodeID]string
	err := sbc.runOnActiveTab(defaultTimeout, func(actCtx context.Context) error {
		return chromedp.Run(actCtx, chromedp.ActionFunc(func(ctx context.Context) error {
			var err error
			nodes, err = accessibility.GetFullAXTree().Do(ctx)
			if err != nil {
				return err
			}
			selectors = resolveSelectors(ctx, nodes)
			return nil
		}))
	})
	if err != nil {
		return nil, err
	}
	return buildAccessibleElements(nodes, selectors), nil
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
		var proto=el.tagName==='TEXTAREA'?window.HTMLTextAreaElement.prototype:window.HTMLInputElement.prototype;
		var ns=Object.getOwnPropertyDescriptor(proto,'value');
		if(ns&&ns.set){ns.set.call(el,'%s');}else{el.value='%s';}
		el.dispatchEvent(new Event('input',{bubbles:true}));
		el.dispatchEvent(new Event('change',{bubbles:true}));
		%s
		return true;
	})()`, escapedSel, escaped, escaped, submitPart)

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
