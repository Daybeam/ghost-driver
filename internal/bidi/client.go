package bidi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Daybeam/ghost-driver/internal/cdp"
	"github.com/gorilla/websocket"
)

// handlerEntry pairs a handler with a unique ID for O(1) removal.
type handlerEntry struct {
	id int64
	fn cdp.EventHandler
}

type Client struct {
	conn    *websocket.Conn
	counter atomic.Int64

	pendMu  sync.Mutex
	pending map[int64]chan *bidiMessage

	eventMu        sync.RWMutex
	handlers       map[string][]handlerEntry
	handlerCounter atomic.Int64

	sessionID string
	contextID string

	translate *Translator

	writeMu   sync.Mutex // protects conn.WriteMessage
	handlerSem chan struct{} // bounds concurrent event-handler goroutines
	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}
}

type bidiMessage struct {
	Type      string          `json:"type"`
	ID        int64           `json:"id,omitempty"`
	Method    string          `json:"method,omitempty"`
	Params    json.RawMessage `json:"params,omitempty"`
	Result    json.RawMessage `json:"result,omitempty"`
	Error     string          `json:"error,omitempty"`
	Message   string          `json:"message,omitempty"`
	SessionID string          `json:"sessionId,omitempty"`
}

func Connect(ctx context.Context, wsURL string) (*Client, error) {
	// Firefox BiDi server rejects any non-empty Origin header.
	// Set Origin to an empty slice to suppress the header entirely.
	// gorilla/websocket won't override an existing key, and Go's net/http
	// skips headers with empty value slices.
	header := http.Header{}
	header["Origin"] = []string{}
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, wsURL, header)
	if err != nil {
		return nil, err
	}
	conn.SetReadLimit(64 * 1024 * 1024)

	cctx, cancel := context.WithCancel(ctx)
	c := &Client{
		conn:       conn,
		pending:    make(map[int64]chan *bidiMessage),
		handlers:       make(map[string][]handlerEntry),
		translate:  NewTranslator(),
		handlerSem: make(chan struct{}, 32),
		ctx:        cctx,
		cancel:     cancel,
		done:       make(chan struct{}),
	}

	go c.readLoop()
	go c.keepAlive()

	if err := c.initSession(ctx); err != nil {
		cancel()
		conn.Close()
		select {
		case <-c.done:
		case <-time.After(3 * time.Second):
		}
		return nil, err
	}

	return c, nil
}

// keepAlive sends periodic WebSocket pings to detect half-open connections.
func (c *Client) keepAlive() {
	ticker := time.NewTicker(25 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			c.writeMu.Lock()
			err := c.conn.WriteMessage(websocket.PingMessage, nil)
			c.writeMu.Unlock()
			if err != nil {
				return
			}
		case <-c.ctx.Done():
			return
		}
	}
}

func (c *Client) initSession(ctx context.Context) error {
	resp, err := c.sendRaw(ctx, "session.new", map[string]any{"capabilities": map[string]any{}})
	if err != nil {
		return fmt.Errorf("session.new: %w", err)
	}
	var sr struct {
		SessionID string `json:"sessionId"`
	}
	if err := json.Unmarshal(resp.Result, &sr); err != nil {
		return fmt.Errorf("session.new result: %w", err)
	}
	c.sessionID = sr.SessionID

	resp2, err := c.sendRaw(ctx, "browsingContext.getTree", map[string]any{})
	if err != nil {
		return fmt.Errorf("browsingContext.getTree: %w", err)
	}
	var tr struct {
		Contexts []struct {
			Context string `json:"context"`
		} `json:"contexts"`
	}
	if err := json.Unmarshal(resp2.Result, &tr); err != nil {
		return fmt.Errorf("getTree result: %w", err)
	}
	if len(tr.Contexts) == 0 {
		resp3, err := c.sendRaw(ctx, "browsingContext.create", map[string]any{"type": "tab"})
		if err != nil {
			return fmt.Errorf("browsingContext.create: %w", err)
		}
		var cr struct {
			Context string `json:"context"`
		}
		if err := json.Unmarshal(resp3.Result, &cr); err != nil {
			return fmt.Errorf("browsingContext.create result: %w", err)
		}
		c.contextID = cr.Context
	} else {
		c.contextID = tr.Contexts[0].Context
	}

	// Subscribe to BiDi events that the translator maps to CDP events.
	_, err = c.sendRaw(ctx, "session.subscribe", map[string]any{
		"events": []string{
			"browsingContext.domContentLoaded",
			"browsingContext.load",
			"browsingContext.navigationStarted",
			"browsingContext.userPromptOpened",
			"network.beforeRequestSent",
			"network.responseCompleted",
		},
	})
	if err != nil {
		return fmt.Errorf("session.subscribe: %w", err)
	}

	return nil
}

func (c *Client) sendRaw(ctx context.Context, method string, params any) (*bidiMessage, error) {
	id := c.counter.Add(1)
	msg := map[string]any{"id": id, "method": method, "params": params}
	if c.sessionID != "" {
		msg["sessionId"] = c.sessionID
	}

	ch := make(chan *bidiMessage, 1)
	c.pendMu.Lock()
	c.pending[id] = ch
	c.pendMu.Unlock()

	data, err := json.Marshal(msg)
	if err != nil {
		return nil, fmt.Errorf("marshal bidi message: %w", err)
	}
	c.writeMu.Lock()
	writeErr := c.conn.WriteMessage(websocket.TextMessage, data)
	c.writeMu.Unlock()
	if writeErr != nil {
		c.pendMu.Lock()
		delete(c.pending, id)
		c.pendMu.Unlock()
		return nil, writeErr
	}

	select {
	case resp := <-ch:
		if resp.Type == "error" {
			return resp, fmt.Errorf("%s: %s", resp.Error, resp.Message)
		}
		return resp, nil
	case <-ctx.Done():
		c.pendMu.Lock()
		delete(c.pending, id)
		c.pendMu.Unlock()
		return nil, ctx.Err()
	}
}

func (c *Client) readLoop() {
	defer close(c.done)
	for {
		_, data, err := c.conn.ReadMessage()
		if err != nil {
			c.failPending(err)
			return
		}
		var msg bidiMessage
		if json.Unmarshal(data, &msg) != nil {
			continue
		}
		if msg.Type == "event" {
			c.dispatchEvent(&msg)
			continue
		}
		if msg.ID != 0 {
			c.pendMu.Lock()
			ch, ok := c.pending[msg.ID]
			if ok {
				delete(c.pending, msg.ID)
			}
			c.pendMu.Unlock()
			if ok && ch != nil {
				ch <- &msg
			}
		}
	}
}

func (c *Client) failPending(err error) {
	c.pendMu.Lock()
	for id, ch := range c.pending {
		ch <- &bidiMessage{Type: "error", Error: "connection", Message: err.Error()}
		delete(c.pending, id)
	}
	c.pendMu.Unlock()
}

func (c *Client) dispatchEvent(msg *bidiMessage) {
	cdpEvent, cdpParams := c.translate.Event(msg.Method, msg.Params)
	if cdpEvent == "" {
		return
	}
	c.eventMu.RLock()
	src := c.handlers[cdpEvent]
	handlers := make([]cdp.EventHandler, len(src))
	for i, e := range src {
		handlers[i] = e.fn
	}
	c.eventMu.RUnlock()
	for _, h := range handlers {
		select {
		case c.handlerSem <- struct{}{}:
			go func(h cdp.EventHandler, params json.RawMessage) {
				defer func() { <-c.handlerSem }()
				h(params)
			}(h, cdpParams)
		default:
			// Backpressure: semaphore full (32 in flight). Dispatch inline.
			// Inline handlers must not call Call synchronously (readLoop
			// would block waiting for its own response).
			h(cdpParams)
		}
	}
}

// Call implements cdp.CDPClient — translates CDP method to BiDi method.
func (c *Client) Call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	bidiMethod, bidiParams, err := c.translate.Command(method, params, c.contextID)
	if err != nil {
		return nil, err
	}
	if bidiMethod == "" {
		return json.RawMessage("null"), nil
	}
	resp, err := c.sendRaw(ctx, bidiMethod, bidiParams)
	if err != nil {
		return nil, err
	}
	return c.translate.Response(method, resp.Result), nil
}

func (c *Client) CallSession(ctx context.Context, sessionID, method string, params any) (json.RawMessage, error) {
	return c.Call(ctx, method, params)
}

func (c *Client) SetSessionID(sid string) {}
func (c *Client) SessionID() string       { return c.sessionID }

func (c *Client) On(method string, fn cdp.EventHandler) func() {
	id := c.handlerCounter.Add(1)
	c.eventMu.Lock()
	c.handlers[method] = append(c.handlers[method], handlerEntry{id, fn})
	c.eventMu.Unlock()

	return func() {
		c.eventMu.Lock()
		entries := c.handlers[method]
		for i, e := range entries {
			if e.id == id {
				c.handlers[method] = append(entries[:i], entries[i+1:]...)
				break
			}
		}
		c.eventMu.Unlock()
	}
}

func (c *Client) Off(method string, fn cdp.EventHandler) {
	c.eventMu.Lock()
	entries := c.handlers[method]
	for i, e := range entries {
		if reflect.ValueOf(e.fn).Pointer() == reflect.ValueOf(fn).Pointer() {
			c.handlers[method] = append(entries[:i], entries[i+1:]...)
			break
		}
	}
	c.eventMu.Unlock()
}

func (c *Client) AddBinding(ctx context.Context, name string) error {
	return nil
}

func (c *Client) Close() {
	// Best-effort session.end to avoid leaking sessions on Firefox.
	// Firefox 157 uses "session.end" (not "session.delete" from the spec).
	// Send session.end BEFORE cancel so the connection is still alive to
	// deliver the request and receive the response.
	if c.sessionID != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		c.sendRaw(ctx, "session.end", map[string]any{})
		cancel()
	}
	c.cancel()
	c.writeMu.Lock()
	c.conn.WriteMessage(websocket.CloseMessage,
		websocket.FormatCloseMessage(websocket.CloseNormalClosure, "bye"))
	c.writeMu.Unlock()
	c.conn.Close()
	// Wait for readLoop to exit, but don't block forever — if ReadMessage
	// is stuck on a zombie connection, abandon after 3s.
	select {
	case <-c.done:
	case <-time.After(3 * time.Second):
	}
}

// WaitForAttach is a no-op in BiDi (no target attach mechanism).
func (c *Client) WaitForAttach(ctx context.Context, targetID string) string {
	_ = ctx
	_ = targetID
	return ""
}

// ContextID returns the default browsing context ID.
func (c *Client) ContextID() string { return c.contextID }

// Ping sends a no-op to check connection health.
func (c *Client) Ping(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	_, err := c.sendRaw(ctx, "session.status", map[string]any{})
	return err
}
