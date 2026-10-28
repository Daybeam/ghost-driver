package browser

import (
	"context"
	"encoding/json"
	"fmt"
)

// GetCookies returns all cookies for the current page.
func (b *Browser) GetCookies(ctx context.Context) ([]map[string]any, error) {
	result, err := b.Call(ctx, "Network.getCookies", nil)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Cookies []map[string]any `json:"cookies"`
	}
	if err := json.Unmarshal(result, &resp); err != nil {
		return nil, err
	}
	return resp.Cookies, nil
}

// SetCookie sets a cookie.
func (b *Browser) SetCookie(ctx context.Context, name, value, domain, path string, httpOnly, secure bool) error {
	_, err := b.Call(ctx, "Network.setCookie", map[string]any{
		"name":     name,
		"value":    value,
		"domain":   domain,
		"path":     path,
		"httpOnly": httpOnly,
		"secure":   secure,
	})
	return err
}

// GetLocalStorage returns all items in localStorage.
func (b *Browser) GetLocalStorage(ctx context.Context) (map[string]string, error) {
	js := `(function() {
		var items = {};
		for (var i = 0; i < localStorage.length; i++) {
			var key = localStorage.key(i);
			items[key] = localStorage.getItem(key);
		}
		return items;
	})()`
	res, err := b.EvalJS(ctx, js)
	if err != nil {
		return nil, err
	}
	var items map[string]string
	if err := json.Unmarshal(res, &items); err != nil {
		return nil, err
	}
	return items, nil
}

// SetLocalStorage sets an item in localStorage.
func (b *Browser) SetLocalStorage(ctx context.Context, key, value string) error {
	js := fmt.Sprintf(`localStorage.setItem(%q, %q)`, key, value)
	_, err := b.EvalJS(ctx, js)
	return err
}

// RemoveLocalStorage removes an item from localStorage.
func (b *Browser) RemoveLocalStorage(ctx context.Context, key string) error {
	js := fmt.Sprintf(`localStorage.removeItem(%q)`, key)
	_, err := b.EvalJS(ctx, js)
	return err
}

// GetSessionStorage returns all items in sessionStorage.
func (b *Browser) GetSessionStorage(ctx context.Context) (map[string]string, error) {
	js := `(function() {
		var items = {};
		for (var i = 0; i < sessionStorage.length; i++) {
			var key = sessionStorage.key(i);
			items[key] = sessionStorage.getItem(key);
		}
		return items;
	})()`
	res, err := b.EvalJS(ctx, js)
	if err != nil {
		return nil, err
	}
	var items map[string]string
	if err := json.Unmarshal(res, &items); err != nil {
		return nil, err
	}
	return items, nil
}

// SetSessionStorage sets an item in sessionStorage.
func (b *Browser) SetSessionStorage(ctx context.Context, key, value string) error {
	js := fmt.Sprintf(`sessionStorage.setItem(%q, %q)`, key, value)
	_, err := b.EvalJS(ctx, js)
	return err
}

// RemoveSessionStorage removes an item from sessionStorage.
func (b *Browser) RemoveSessionStorage(ctx context.Context, key string) error {
	js := fmt.Sprintf(`sessionStorage.removeItem(%q)`, key)
	_, err := b.EvalJS(ctx, js)
	return err
}
