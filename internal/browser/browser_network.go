package browser

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Daybeam/ghost-driver/internal/cdp"
)

// InterceptNetworkStream starts listening for network chunks matching a keyword.
// The returned cleanup func is safe to call via defer: it stops new dispatches,
// waits for in-flight handlers to observe cancellation, and only then closes
// the channel (preventing send-on-closed-channel panics).
func (b *Browser) InterceptNetworkStream(ctx context.Context, keyword string) (<-chan string, func(), error) {
	ch := make(chan string, 200)
	hctx, hcancel := context.WithCancel(context.Background())

	var mu sync.Mutex
	activeReqs := make(map[string]bool)
	closed := false
	var wg sync.WaitGroup

	// wrap guards an event handler against the cleanup race: after cleanup
	// flips `closed`, no new invocation is tracked; wg.Wait() then guarantees
	// every in-flight handler has returned before close(ch).
	wrap := func(fn func(json.RawMessage)) cdp.EventHandler {
		return func(params json.RawMessage) {
			mu.Lock()
			if closed {
				mu.Unlock()
				return
			}
			wg.Add(1)
			mu.Unlock()
			defer wg.Done()

			select {
			case <-hctx.Done():
				return
			default:
			}
			fn(params)
		}
	}

	reqHandler := wrap(func(params json.RawMessage) {
		var p struct {
			Request struct {
				URL string `json:"url"`
			} `json:"request"`
			RequestID string `json:"requestId"`
		}
		if err := json.Unmarshal(params, &p); err == nil {
			if strings.Contains(p.Request.URL, keyword) {
				mu.Lock()
				activeReqs[p.RequestID] = true
				mu.Unlock()
			}
		}
	})

	sseHandler := wrap(func(params json.RawMessage) {
		var p struct {
			RequestID string `json:"requestId"`
			Data      string `json:"data"`
		}
		if err := json.Unmarshal(params, &p); err == nil {
			mu.Lock()
			matched := activeReqs[p.RequestID]
			mu.Unlock()
			if matched {
				// Never block forever on a full buffer: if the consumer is
				// slow, the chunk is dropped instead of leaking a goroutine.
				select {
				case ch <- p.Data:
				case <-hctx.Done():
				default:
				}
			}
		}
	})

	bt := b.BrowserType()
	reqCancel := b.On("Network.requestWillBeSent", reqHandler)
	var sseCancel func()
	if bt != "firefox" {
		sseCancel = b.On("Network.eventSourceMessageReceived", sseHandler)
	}

	cleanup := func() {
		if reqCancel != nil {
			reqCancel()
		}
		if sseCancel != nil {
			sseCancel()
		}
		mu.Lock()
		closed = true
		mu.Unlock()
		hcancel()
		wg.Wait()
		close(ch)
	}

	return ch, cleanup, nil
}

// HandleDialog allows manual control over an open JavaScript dialog.
func (b *Browser) HandleDialog(ctx context.Context, accept bool, promptText string) error {
	params := map[string]any{
		"accept": accept,
	}
	if promptText != "" {
		params["promptText"] = promptText
	}
	_, err := b.Call(ctx, "Page.handleJavaScriptDialog", params)
	return err
}

// UploadFiles handles an open file chooser dialog by providing the specified file paths.
// selector optionally overrides the CSS selector for the file input (default: "input[type=file]").
func (b *Browser) UploadFiles(ctx context.Context, filePaths []string, selector string) error {
	// Convert relative paths to absolute paths
	var absPaths []string
	for _, p := range filePaths {
		abs, err := filepath.Abs(p)
		if err != nil {
			abs = p // Fallback
		}
		absPaths = append(absPaths, abs)
	}

	// Try Page.handleFileChooser first (older Chrome / standard CDP).
	_, err := b.Call(ctx, "Page.handleFileChooser", map[string]any{
		"action": "accept",
		"files":  absPaths,
	})
	if err == nil {
		b.mu.Lock()
		b.HasSystemDialog = false
		b.mu.Unlock()
		b.fileChooserBackendNode.Store(0)
		return nil
	}

	// Page.handleFileChooser was removed in newer Chrome (JSON-RPC -32601 method-not-found).
	// Fall back to DOM.setFileInputFiles which sets files directly on the <input type=file>.
	if !cdp.IsMethodNotFound(err) {
		return err
	}
	return b.uploadFilesViaDOM(ctx, absPaths, selector)
}

// uploadFilesViaDOM sets files on an <input type=file> element via DOM.setFileInputFiles,
// used as a fallback when Page.handleFileChooser is unavailable (Chrome 131+).
func (b *Browser) uploadFilesViaDOM(ctx context.Context, absPaths []string, selector string) error {
	// Prefer the backendNodeId captured from the Page.fileChooserOpened event.
	backendNode := b.fileChooserBackendNode.Load()
	if backendNode > 0 {
		if _, err := b.Call(ctx, "DOM.setFileInputFiles", map[string]any{
			"files":         absPaths,
			"backendNodeId": backendNode,
		}); err == nil {
			b.mu.Lock()
			b.HasSystemDialog = false
			b.mu.Unlock()
			b.fileChooserBackendNode.Store(0)
			return nil
		}
	}

	// Query the DOM for the file input element.
	doc, err := b.Call(ctx, "DOM.getDocument", map[string]any{"depth": 0})
	if err != nil {
		return fmt.Errorf("DOM.getDocument: %w", err)
	}
	var docResp struct {
		Root struct {
			NodeID int `json:"nodeId"`
		} `json:"root"`
	}
	if err := json.Unmarshal(doc, &docResp); err != nil {
		return fmt.Errorf("parse DOM.getDocument: %w", err)
	}
	if docResp.Root.NodeID == 0 {
		return fmt.Errorf("DOM.getDocument returned no root nodeId")
	}

	sel := selector
	if sel == "" {
		sel = "input[type=file]"
	}
	qRes, err := b.Call(ctx, "DOM.querySelector", map[string]any{
		"nodeId":   docResp.Root.NodeID,
		"selector": sel,
	})
	if err != nil {
		return fmt.Errorf("DOM.querySelector: %w", err)
	}
	var qResp struct {
		NodeID int `json:"nodeId"`
	}
	if err := json.Unmarshal(qRes, &qResp); err != nil {
		return fmt.Errorf("parse DOM.querySelector: %w", err)
	}
	if qResp.NodeID == 0 {
		return fmt.Errorf("no element matching '%s' found on page", sel)
	}

	if _, err := b.Call(ctx, "DOM.setFileInputFiles", map[string]any{
		"files":  absPaths,
		"nodeId": qResp.NodeID,
	}); err != nil {
		return fmt.Errorf("DOM.setFileInputFiles: %w", err)
	}

	b.mu.Lock()
	b.HasSystemDialog = false
	b.mu.Unlock()
	b.fileChooserBackendNode.Store(0)
	return nil
}

// CaptureNetworkCall waits for a network request matching the pattern and returns its response body.
func (b *Browser) CaptureNetworkCall(ctx context.Context, pattern string, timeout time.Duration) (string, error) {
	type result struct {
		body string
		err  error
	}
	resCh := make(chan result, 1)
	var matched atomic.Bool

	// Register listener
	handler := func(params json.RawMessage) {
		var p struct {
			Response struct {
				URL string `json:"url"`
			} `json:"response"`
			RequestID string `json:"requestId"`
		}
		if err := json.Unmarshal(params, &p); err == nil {
			if strings.Contains(p.Response.URL, pattern) {
				if !matched.CompareAndSwap(false, true) {
					return
				}
				go func() {
					resp, err := b.Call(ctx, "Network.getResponseBody", map[string]string{"requestId": p.RequestID})
					if err != nil {
						select {
						case resCh <- result{err: err}:
						default:
						}
						return
					}
					var r struct {
						Body          string `json:"body"`
						Base64Encoded bool   `json:"base64Encoded"`
					}
					if err := json.Unmarshal(resp, &r); err == nil {
						body := r.Body
						if r.Base64Encoded {
							if decoded, err := base64.StdEncoding.DecodeString(r.Body); err == nil {
								body = string(decoded)
							}
						}
						select {
						case resCh <- result{body: body}:
						default:
						}
					}
				}()
			}
		}
	}

	cancelHandler := b.On("Network.responseReceived", handler)
	defer cancelHandler()

	t := time.NewTimer(timeout)
	defer t.Stop()

	select {
	case res := <-resCh:
		return res.body, res.err
	case <-t.C:
		return "", fmt.Errorf("timeout waiting for network call matching: %s", pattern)
	case <-ctx.Done():
		return "", ctx.Err()
	}
}
