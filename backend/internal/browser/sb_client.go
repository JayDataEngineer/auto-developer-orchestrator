package browser

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
)

func (sbc *SandboxBrowserClient) sbScreenshot(ctx context.Context) ([]byte, error) {
	if _, err := sbc.sbCall(ctx, "POST", "/screenshot", map[string]any{"path": "/tmp/sb_shot.png"}); err != nil {
		return nil, fmt.Errorf("screenshot (sb): %w", err)
	}
	fileResp, err := sbc.sbCall(ctx, "GET", "/file//tmp/sb_shot.png", nil)
	if err != nil {
		return nil, fmt.Errorf("screenshot read (sb): %w", err)
	}
	dataURI, _ := fileResp["data_uri"].(string)
	if dataURI == "" {
		return nil, fmt.Errorf("screenshot (sb): no data_uri")
	}
	parts := strings.SplitN(dataURI, ",", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("screenshot (sb): malformed data_uri")
	}
	buf, err := base64.StdEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("screenshot (sb): decode: %w", err)
	}
	// Update cached URL/Title/Elements for downstream callers
	sbc.sbRefreshCache(ctx)
	sbc.mu.Lock()
	sbc.lastScreenshot = buf
	sbc.mu.Unlock()
	return buf, nil
}

func (sbc *SandboxBrowserClient) sbRefreshCache(ctx context.Context) {
	// Read page data (title, URL) and element map
	readResp, err := sbc.sbCall(ctx, "POST", "/read", nil)
	if err != nil {
		return
	}
	sbc.mu.Lock()
	defer sbc.mu.Unlock()
	if p, ok := readResp["page_data"].(map[string]any); ok {
		if t, _ := p["title"].(string); t != "" {
			sbc.lastTitle = t
		}
		if u, _ := p["url"].(string); u != "" {
			sbc.lastURL = u
		}
	}
	// Try element_map from /read first (some sb_server versions include it)
	if em, ok := readResp["element_map"].([]any); ok && len(em) > 0 {
		sbc.lastElements = sbParseElements(em)
		return
	}
	// Fall back to /label to get element_map
	labelResp, err := sbc.sbCall(ctx, "POST", "/label", nil)
	if err != nil {
		return
	}
	if em2, ok := labelResp["element_map"].([]any); ok {
		sbc.lastElements = sbParseElements(em2)
	}
}

func sbParseElements(em []any) []LabeledElement {
	elements := make([]LabeledElement, 0, len(em))
	for _, eAny := range em {
		if e, ok := eAny.(map[string]any); ok {
			le := LabeledElement{
				ID: int(numToFloat(e["index"])),
			}
			le.Tag, _ = e["tag"].(string)
			le.Text, _ = e["text"].(string)
			le.Selector, _ = e["selector"].(string)
			le.X = int(numToFloat(e["x"]))
			le.Y = int(numToFloat(e["y"]))
			le.W = int(numToFloat(e["w"]))
			le.H = int(numToFloat(e["h"]))
			elements = append(elements, le)
		}
	}
	return elements
}

func (sbc *SandboxBrowserClient) sbNavigate(ctx context.Context, url string) (*PageInfo, error) {
	resp, err := sbc.sbCall(ctx, "POST", "/navigate", map[string]any{"url": url})
	if err != nil {
		return nil, err
	}
	sbc.sbRefreshCache(ctx)
	// Also grab the screenshot so PageInfo has it
	shotPath, _ := resp["screenshot_path"].(string)
	info := sbPageInfoFromResp(resp)
	if shotPath != "" {
		fileResp, err := sbc.sbCall(ctx, "GET", "/file/"+shotPath, nil)
		if err == nil {
			if dataURI, _ := fileResp["data_uri"].(string); dataURI != "" {
				parts := strings.SplitN(dataURI, ",", 2)
				if len(parts) == 2 {
					info.Screenshot = parts[1]
				}
			}
		}
	}
	// Cache screenshot
	// Also ensure lastURL/lastTitle are set (sbRefreshCache may not be atomic
	// with sbScreenshot for downstream Snapshot callers)
	sbc.lastURL = info.URL
	sbc.lastTitle = info.Title
	shotBytes, err2 := sbc.sbScreenshot(ctx)
	if err2 == nil {
		sbc.mu.Lock()
		sbc.lastScreenshot = shotBytes
		sbc.mu.Unlock()
	}
	return info, nil
}

func (sbc *SandboxBrowserClient) sbClick(ctx context.Context, elementID int) (*PageInfo, error) {
	resp, err := sbc.sbCall(ctx, "POST", "/click", map[string]any{"index": elementID})
	if err != nil {
		return nil, err
	}
	sbc.sbRefreshCache(ctx)
	return sbPageInfoFromResp(resp), nil
}

func (sbc *SandboxBrowserClient) sbClickXY(ctx context.Context, x, y float64) (*PageInfo, error) {
	if _, err := sbc.sbCall(ctx, "POST", "/evaluate", map[string]any{
		"code": fmt.Sprintf("document.elementFromPoint(%f,%f)&&document.elementFromPoint(%f,%f).click()", x, y, x, y),
	}); err != nil {
		return nil, err
	}
	readResp, err := sbc.sbCall(ctx, "POST", "/read", nil)
	if err != nil {
		return nil, err
	}
	return sbPageInfoFromResp(readResp), nil
}

func (sbc *SandboxBrowserClient) sbType(ctx context.Context, elementID int, text string, submit bool) (*PageInfo, error) {
	resp, err := sbc.sbCall(ctx, "POST", "/type", map[string]any{
		"index":  elementID,
		"text":   text,
		"submit": submit,
	})
	if err != nil {
		return nil, err
	}
	sbc.sbRefreshCache(ctx)
	return sbPageInfoFromResp(resp), nil
}

func (sbc *SandboxBrowserClient) sbScroll(ctx context.Context, direction string, amount int) (*PageInfo, error) {
	resp, err := sbc.sbCall(ctx, "POST", "/scroll", map[string]any{
		"direction": direction,
		"amount":    amount,
	})
	if err != nil {
		return nil, err
	}
	sbc.sbRefreshCache(ctx)
	return sbPageInfoFromResp(resp), nil
}

func (sbc *SandboxBrowserClient) sbEvaluate(ctx context.Context, code string) (string, string, error) {
	resp, err := sbc.sbCall(ctx, "POST", "/evaluate", map[string]any{"code": code})
	if err != nil {
		return "", "", err
	}
	result, _ := resp["result"].(string)
	errorText, _ := resp["error"].(string)
	return result, errorText, nil
}

func (sbc *SandboxBrowserClient) sbReadPage(ctx context.Context) (*PageData, error) {
	resp, err := sbc.sbCall(ctx, "POST", "/read", nil)
	if err != nil {
		return nil, err
	}
	return sbPageDataFromResp(resp), nil
}

func (sbc *SandboxBrowserClient) sbUpload(ctx context.Context, selector string, filePaths []string) (map[string]string, error) {
	if len(filePaths) == 0 {
		return nil, fmt.Errorf("upload (sb): no file paths")
	}
	resp, err := sbc.sbCall(ctx, "POST", "/upload", map[string]any{
		"selector":  selector,
		"file_path": filePaths[0],
	})
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	uploaded, _ := resp["uploaded"].(bool)
	if uploaded {
		out["status"] = "uploaded"
	} else {
		out["status"] = "failed"
	}
	return out, nil
}

func (sbc *SandboxBrowserClient) sbGetSnapshot() *PageInfo {
	sbc.mu.RLock()
	defer sbc.mu.RUnlock()
	if sbc.lastURL == "" && sbc.lastScreenshot == nil {
		return nil
	}
	return &PageInfo{
		URL:        sbc.lastURL,
		Title:      sbc.lastTitle,
		Elements:   sbc.lastElements,
		Screenshot: base64.StdEncoding.EncodeToString(sbc.lastScreenshot),
	}
}

func sbPageInfoFromResp(resp map[string]any) *PageInfo {
	info := &PageInfo{}
	if pd, ok := resp["page_data"].(map[string]any); ok {
		info.Title, _ = pd["title"].(string)
		info.URL, _ = pd["url"].(string)
	}
	if em, ok := resp["element_map"].([]any); ok {
		info.Elements = sbParseElements(em)
	}
	if title, _ := resp["title"].(string); title != "" && info.Title == "" {
		info.Title = title
	}
	if u, _ := resp["url"].(string); u != "" && info.URL == "" {
		info.URL = u
	}
	for i := range info.Elements {
		info.Elements[i].DisplayTag = info.Elements[i].Tag
	}
	return info
}

func sbPageDataFromResp(resp map[string]any) *PageData {
	pd := &PageData{}
	if pageData, ok := resp["page_data"].(map[string]any); ok {
		pd.Title, _ = pageData["title"].(string)
		pd.URL, _ = pageData["url"].(string)
		pd.Text, _ = pageData["text"].(string)
		if images, _ := pageData["images"].([]any); len(images) > 0 {
			for _, imgAny := range images {
				if img, ok := imgAny.(map[string]any); ok {
					src, _ := img["src"].(string)
					if src != "" {
						pd.Images = append(pd.Images, PageImage{Src: src})
					}
				} else if s, ok := imgAny.(string); ok {
					pd.Images = append(pd.Images, PageImage{Src: s})
				}
			}
		}
		if links, _ := pageData["links"].([]any); len(links) > 0 {
			for _, lAny := range links {
				if l, ok := lAny.(map[string]any); ok {
					text, _ := l["text"].(string)
					url, _ := l["url"].(string)
					pd.Links = append(pd.Links, PageLink{Text: text, URL: url})
				}
			}
		}
	}
	return pd
}

func numToFloat(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case float32:
		return float64(n)
	case int:
		return float64(n)
	case int64:
		return float64(n)
	case json.Number:
		f, _ := n.Float64()
		return f
	default:
		return 0
	}
}
