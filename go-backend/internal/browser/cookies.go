package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
)

func (sbc *SandboxBrowserClient) GetCookies(ctx context.Context, urls []string) ([]Cookie, error) {
	var cookies []*network.Cookie

	err := sbc.runOnActiveTab(defaultTimeout, func(actCtx context.Context) error {
		return chromedp.Run(actCtx, chromedp.ActionFunc(func(ctx context.Context) error {
			params := network.GetCookies()
			if len(urls) > 0 {
				params = params.WithURLs(urls)
			}
			var err error
			cookies, err = params.Do(ctx)
			return err
		}))
	})
	if err != nil {
		return nil, fmt.Errorf("get cookies: %w", err)
	}

	result := make([]Cookie, len(cookies))
	for i, c := range cookies {
		result[i] = Cookie{
			Name:     c.Name,
			Value:    c.Value,
			Domain:   c.Domain,
			Path:     c.Path,
			Secure:   c.Secure,
			HTTPOnly: c.HTTPOnly,
			SameSite: c.SameSite.String(),
			Expires:  c.Expires,
		}
	}
	return result, nil
}

func (sbc *SandboxBrowserClient) SetCookie(ctx context.Context, cookie Cookie) error {
	err := sbc.runOnActiveTab(defaultTimeout, func(actCtx context.Context) error {
		return chromedp.Run(actCtx, chromedp.ActionFunc(func(ctx context.Context) error {
			params := network.SetCookie(cookie.Name, cookie.Value).
				WithDomain(cookie.Domain).
				WithPath(cookie.Path).
				WithSecure(cookie.Secure).
				WithHTTPOnly(cookie.HTTPOnly)
			if cookie.URL() != "" {
				params = params.WithURL(cookie.URL())
			}
			return params.Do(ctx)
		}))
	})
	if err != nil {
		return fmt.Errorf("set cookie: %w", err)
	}
	return nil
}

func (sbc *SandboxBrowserClient) ClearCookies(ctx context.Context) error {
	return sbc.runOnActiveTab(defaultTimeout, func(actCtx context.Context) error {
		return chromedp.Run(actCtx, chromedp.ActionFunc(func(ctx context.Context) error {
			return network.ClearBrowserCookies().Do(ctx)
		}))
	})
}

func (sbc *SandboxBrowserClient) ClearCookiesForURL(ctx context.Context, url string) error {
	var cookies []*network.Cookie
	err := sbc.runOnActiveTab(defaultTimeout, func(actCtx context.Context) error {
		return chromedp.Run(actCtx, chromedp.ActionFunc(func(ctx context.Context) error {
			var err error
			cookies, err = network.GetCookies().WithURLs([]string{url}).Do(ctx)
			return err
		}))
	})
	if err != nil {
		return fmt.Errorf("clear cookies for url: %w", err)
	}

	for _, c := range cookies {
		err := sbc.runOnActiveTab(defaultTimeout, func(actCtx context.Context) error {
			return chromedp.Run(actCtx, chromedp.ActionFunc(func(ctx context.Context) error {
				return network.DeleteCookies(c.Name).
					WithDomain(c.Domain).
					WithPath(c.Path).
					Do(ctx)
			}))
		})
		if err != nil {
			return fmt.Errorf("delete cookie %s: %w", c.Name, err)
		}
	}
	return nil
}

func (sbc *SandboxBrowserClient) GetLocalStorage(ctx context.Context) (map[string]string, error) {
	var result string
	err := sbc.runOnActiveTab(defaultTimeout, func(actCtx context.Context) error {
		return chromedp.Run(actCtx, chromedp.Evaluate(
			`JSON.stringify(Object.entries(localStorage).reduce((o,[k,v])=>{o[k]=v;return o},{}))`,
			&result,
		))
	})
	if err != nil {
		return nil, fmt.Errorf("get localStorage: %w", err)
	}
	var data map[string]string
	if err := json.Unmarshal([]byte(result), &data); err != nil {
		return nil, fmt.Errorf("parse localStorage: %w", err)
	}
	return data, nil
}

func (sbc *SandboxBrowserClient) SetLocalStorageItem(ctx context.Context, key, value string) error {
	escapedKey := strings.ReplaceAll(key, `\`, `\\`)
	escapedKey = strings.ReplaceAll(escapedKey, `'`, `\'`)
	escapedVal := strings.ReplaceAll(value, `\`, `\\`)
	escapedVal = strings.ReplaceAll(escapedVal, `'`, `\'`)
	return sbc.runOnActiveTab(defaultTimeout, func(actCtx context.Context) error {
		return chromedp.Run(actCtx, chromedp.Evaluate(
			fmt.Sprintf(`localStorage.setItem('%s', '%s')`, escapedKey, escapedVal),
			nil,
		))
	})
}

func (sbc *SandboxBrowserClient) ClearLocalStorage(ctx context.Context) error {
	return sbc.runOnActiveTab(defaultTimeout, func(actCtx context.Context) error {
		return chromedp.Run(actCtx, chromedp.Evaluate(`localStorage.clear()`, nil))
	})
}

func (sbc *SandboxBrowserClient) GetSessionStorage(ctx context.Context) (map[string]string, error) {
	var result string
	err := sbc.runOnActiveTab(defaultTimeout, func(actCtx context.Context) error {
		return chromedp.Run(actCtx, chromedp.Evaluate(
			`JSON.stringify(Object.entries(sessionStorage).reduce((o,[k,v])=>{o[k]=v;return o},{}))`,
			&result,
		))
	})
	if err != nil {
		return nil, fmt.Errorf("get sessionStorage: %w", err)
	}
	var data map[string]string
	if err := json.Unmarshal([]byte(result), &data); err != nil {
		return nil, fmt.Errorf("parse sessionStorage: %w", err)
	}
	return data, nil
}

func (c Cookie) URL() string {
	if c.Domain == "" {
		return ""
	}
	scheme := "http"
	if c.Secure {
		scheme = "https"
	}
	return fmt.Sprintf("%s://%s%s", scheme, c.Domain, c.Path)
}
