// Package cdp implements a minimal Chrome DevTools Protocol client.
// Key design: injects Host: localhost at dial time to bypass Chrome's
// remote-debugging host validation (needed when connecting from Docker).
package cdp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

type Message struct {
	ID        int64           `json:"id,omitempty"`
	Method    string          `json:"method,omitempty"`
	Params    json.RawMessage `json:"params,omitempty"`
	Result    json.RawMessage `json:"result,omitempty"`
	Error     *ProtocolError  `json:"error,omitempty"`
	SessionID string          `json:"sessionId,omitempty"`
}

// requestMessage is the wire format for sending CDP commands. Using `any` for
// Params lets json.Marshal handle params in a single pass, eliminating the
// previous double-marshal (params → RawMessage → Message).
type requestMessage struct {
	ID        int64  `json:"id"`
	Method    string `json:"method"`
	Params    any    `json:"params,omitempty"`
	SessionID string `json:"sessionId,omitempty"`
}

type ProtocolError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *ProtocolError) Error() string {
	return fmt.Sprintf("CDP error %d: %s", e.Code, e.Message)
}

// IsMethodNotFound reports whether err is a JSON-RPC "method not found" error (-32601).
// Used to detect CDP methods removed in newer browser versions (e.g. Page.handleFileChooser).
func IsMethodNotFound(err error) bool {
	var pe *ProtocolError
	return errors.As(err, &pe) && pe.Code == -32601
}

// EventHandler handles incoming CDP events.
type EventHandler func(params json.RawMessage)

// CDPClient defines the interface for CDP interactions.
type CDPClient interface {
	Call(ctx context.Context, method string, params any) (json.RawMessage, error)
	CallSession(ctx context.Context, sessionID, method string, params any) (json.RawMessage, error)
	SetSessionID(sid string)
	SessionID() string
	On(method string, fn EventHandler) func()
	Off(method string, fn EventHandler)
	AddBinding(ctx context.Context, name string) error
	Close()
}

// handlerEntry pairs a handler with a unique ID for O(1) removal.
type handlerEntry struct {
	id int64
	fn EventHandler
}

type Client struct {
	conn    *websocket.Conn
	counter atomic.Int64

	pendMu  sync.Mutex
	pending map[int64]chan *Message

	eventMu        sync.RWMutex
	handlers       map[string][]handlerEntry
	handlerCounter atomic.Int64

	sessionMu sync.RWMutex
	sessionID string

	attachMu sync.Mutex
	attachCh map[string]chan string // targetID -> chan that receives sessionID

	writeMu sync.Mutex // writeMu protects conn.WriteMessage and conn.SetWriteDeadline

	// handlerSem bounds concurrent event-handler goroutines. When full, events
	// are dispatched inline (backpressure) instead of spawning unbounded goroutines.
	handlerSem chan struct{}

	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}
}

var (
	// DefaultHostMap is used to replace "localhost" or "127.0.0.1" in CDP URLs.
	// Initialized with a Docker heuristic but can be overridden.
	DefaultHostMap = ""

	// chanPool recycles the per-call response channels (chan *Message, cap 1).
	// Every CallSession allocates one; pooling reduces GC pressure on the
	// hottest path. Channels are only returned on the success path — error
	// paths may have a stale in-flight write from readLoop.
	chanPool = sync.Pool{
		New: func() any { return make(chan *Message, 1) },
	}
)

func init() {
	if _, err := os.Stat("/.dockerenv"); err == nil {
		DefaultHostMap = "host.docker.internal"
	}
}

// Connect dials the CDP WebSocket URL.
func Connect(ctx context.Context, wsURL string) (*Client, error) {
	u, err := url.Parse(wsURL)
	if err != nil {
		return nil, fmt.Errorf("invalid wsURL: %w", err)
	}

	dialer := websocket.Dialer{HandshakeTimeout: 15 * time.Second}
	hdr := http.Header{}

	// CRITICAL for Chrome 150+: The Host header MUST match the URL host.
	// We force the Host header to be what's in the URL.
	hostHeader := u.Host

	// Exception: If we are in Docker (DefaultHostMap setup in init),
	// and the URL contains that map, we might need to lie to Chrome.
	if DefaultHostMap != "" && strings.Contains(u.Host, DefaultHostMap) {
		_, port, _ := net.SplitHostPort(u.Host)
		if port != "" {
			hostHeader = "localhost:" + port
		} else {
			hostHeader = "localhost"
		}
	}
	hdr.Set("Host", hostHeader)

	conn, _, err := dialer.DialContext(ctx, wsURL, hdr)
	if err != nil {
		return nil, fmt.Errorf("cdp dial %s: %w", wsURL, err)
	}

	cctx, cancel := context.WithCancel(ctx)
	c := &Client{
		conn:       conn,
		pending:    make(map[int64]chan *Message),
		handlers:   make(map[string][]handlerEntry),
		attachCh:   make(map[string]chan string),
		handlerSem: make(chan struct{}, 32),
		ctx:        cctx,
		cancel:     cancel,
		done:       make(chan struct{}),
	}
	go c.readLoop()
	go c.keepAlive()
	return c, nil
}

func (c *Client) keepAlive() {
	ticker := time.NewTicker(25 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			c.writeMu.Lock()
			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
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

// Call sends a CDP command and blocks until the response arrives.
// Uses the current sessionID if set (flat mode).
func (c *Client) Call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	c.sessionMu.RLock()
	sid := c.sessionID
	c.sessionMu.RUnlock()
	return c.CallSession(ctx, sid, method, params)
}

// CallSession sends a CDP command with an explicit session ID (flat mode).
func (c *Client) CallSession(ctx context.Context, sessionID, method string, params any) (json.RawMessage, error) {
	id := c.counter.Add(1)

	msg := requestMessage{ID: id, Method: method, Params: params}
	if sessionID != "" {
		msg.SessionID = sessionID
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return nil, fmt.Errorf("marshal message: %w", err)
	}
	ch := chanPool.Get().(chan *Message)

	c.pendMu.Lock()
	c.pending[id] = ch
	c.pendMu.Unlock()

	c.writeMu.Lock()
	c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	err = c.conn.WriteMessage(websocket.TextMessage, data)
	c.writeMu.Unlock()
	if err != nil {
		c.pendMu.Lock()
		delete(c.pending, id)
		c.pendMu.Unlock()
		return nil, fmt.Errorf("write: %w", err)
	}

	select {
	case <-ctx.Done():
		c.pendMu.Lock()
		delete(c.pending, id)
		c.pendMu.Unlock()
		return nil, ctx.Err()
	case resp := <-ch:
		chanPool.Put(ch) // channel is empty, safe to recycle
		if resp.Error != nil {
			return nil, resp.Error
		}
		return resp.Result, nil
	}
}

// SetSessionID sets the active session ID for flat-mode CDP calls.
func (c *Client) SetSessionID(sid string) {
	c.sessionMu.Lock()
	c.sessionID = sid
	c.sessionMu.Unlock()
}

// SessionID returns the current active session ID.
func (c *Client) SessionID() string {
	c.sessionMu.RLock()
	defer c.sessionMu.RUnlock()
	return c.sessionID
}

// WaitForAttach waits for a Target.attachedToTarget event for the given targetID
// and returns the sessionID. Returns "" if the context times out.
func (c *Client) WaitForAttach(ctx context.Context, targetID string) string {
	c.attachMu.Lock()
	ch, ok := c.attachCh[targetID]
	if !ok {
		ch = make(chan string, 1)
		c.attachCh[targetID] = ch
	}
	c.attachMu.Unlock()

	select {
	case sid := <-ch:
		return sid
	case <-ctx.Done():
		return ""
	}
}

// On registers an event handler for a named CDP event and returns a cleanup
// function that removes it. The cleanup function is safe to call multiple times.
func (c *Client) On(method string, fn EventHandler) func() {
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

// Off removes an event handler for a named CDP event.
// Deprecated: use the cleanup function returned by On instead.
func (c *Client) Off(method string, fn EventHandler) {
	c.eventMu.Lock()
	defer c.eventMu.Unlock()
	entries := c.handlers[method]
	for i, e := range entries {
		if reflect.ValueOf(e.fn).Pointer() == reflect.ValueOf(fn).Pointer() {
			c.handlers[method] = append(entries[:i], entries[i+1:]...)
			return
		}
	}
}

// AddBinding registers a CDP binding that JS can call via window.<name>.
func (c *Client) AddBinding(ctx context.Context, name string) error {
	_, err := c.Call(ctx, "Runtime.addBinding", map[string]string{"name": name})
	return err
}

func (c *Client) Close() {
	c.cancel()
	c.conn.Close()
	// Wait for readLoop to exit, but don't block forever — if the WebSocket
	// ReadMessage is stuck on a zombie/half-open connection, conn.Close() may
	// not interrupt it. Abandon after 3s to avoid deadlocking the caller
	// (which typically holds a mutex, e.g. the server's s.mu).
	select {
	case <-c.done:
	case <-time.After(3 * time.Second):
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
		var msg Message
		if err := json.Unmarshal(data, &msg); err != nil {
			continue
		}
		if msg.ID != 0 {
			c.pendMu.Lock()
			ch, ok := c.pending[msg.ID]
			if ok {
				delete(c.pending, msg.ID)
			}
			c.pendMu.Unlock()
			if ok {
				ch <- &msg
			}
		} else if msg.Method != "" {
			if msg.Method == "Target.attachedToTarget" {
				var attach struct {
					SessionID  string `json:"sessionId"`
					TargetInfo struct {
						TargetID string `json:"targetId"`
						Type     string `json:"type"`
						URL      string `json:"url"`
					} `json:"targetInfo"`
				}
				if err := json.Unmarshal(msg.Params, &attach); err == nil && attach.SessionID != "" {
					// Only adopt the sessionID from an autoAttach event if we don't
					// already have one. The explicit Target.attachToTarget in
					// reconnect() sets the intended sessionID; autoAttach events for
					// other targets (iframes, new tabs) must NOT overwrite it — doing
					// so sends subsequent CDP calls to the wrong session and hangs.
					if attach.TargetInfo.Type == "page" {
						c.sessionMu.Lock()
						if c.sessionID == "" {
							c.sessionID = attach.SessionID
						}
						c.sessionMu.Unlock()
					}
					c.attachMu.Lock()
					if ch, ok := c.attachCh[attach.TargetInfo.TargetID]; ok {
						select {
						case ch <- attach.SessionID:
						default:
						}
					}
					c.attachMu.Unlock()
				}
			}
			c.eventMu.RLock()
			src := c.handlers[msg.Method]
			fns := make([]EventHandler, len(src))
			for i, e := range src {
				fns[i] = e.fn
			}
			c.eventMu.RUnlock()
			for _, fn := range fns {
				select {
				case c.handlerSem <- struct{}{}:
					go func(fn EventHandler, params json.RawMessage) {
						defer func() { <-c.handlerSem }()
						fn(params)
					}(fn, msg.Params)
				default:
					// Backpressure: semaphore full (32 handlers in flight).
					// Dispatch inline instead of dropping the event or
					// spawning unbounded goroutines. Inline handlers must not
					// call CDPClient.Call synchronously (readLoop would block
					// waiting for its own response).
					fn(msg.Params)
				}
			}
		}
	}
}

func (c *Client) failPending(err error) {
	c.pendMu.Lock()
	for id, ch := range c.pending {
		ch <- &Message{Error: &ProtocolError{Code: -1, Message: err.Error()}}
		delete(c.pending, id)
	}
	c.pendMu.Unlock()
}
