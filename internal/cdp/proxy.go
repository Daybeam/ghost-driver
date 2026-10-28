package cdp

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
)

// ProxyConfig holds CDP reverse proxy configuration.
type ProxyConfig struct {
	ListenAddr  string
	Upstream    string
	RewriteHost string // e.g. "localhost:9222"
	Logf        func(string, ...any)
}

// ReverseProxy is a built-in CDP reverse proxy to bypass Host header validation.
type ReverseProxy struct {
	cfg      ProxyConfig
	ln       net.Listener
	lnMu     sync.RWMutex
	wg       sync.WaitGroup
	quit     chan struct{}
	stopOnce sync.Once
	activeMu sync.Mutex
	active   map[net.Conn]struct{}
}

// NewProxy creates a new ReverseProxy.
func NewProxy(cfg ProxyConfig) *ReverseProxy {
	if cfg.RewriteHost == "" {
		cfg.RewriteHost = "localhost"
		if parts := strings.Split(cfg.Upstream, ":"); len(parts) > 1 {
			cfg.RewriteHost += ":" + parts[1]
		}
	}
	if cfg.Logf == nil {
		cfg.Logf = func(string, ...any) {}
	}
	return &ReverseProxy{
		cfg:    cfg,
		quit:   make(chan struct{}),
		active: make(map[net.Conn]struct{}),
	}
}

// Start begins listening and forwarding.
func (p *ReverseProxy) Start() error {
	ln, err := net.Listen("tcp", p.cfg.ListenAddr)
	if err != nil {
		return err
	}
	p.lnMu.Lock()
	p.ln = ln
	p.lnMu.Unlock()
	p.cfg.Logf("CDP proxy started: %s -> %s (rewrite Host: %s)", ln.Addr(), p.cfg.Upstream, p.cfg.RewriteHost)

	for {
		conn, err := ln.Accept()
		if err != nil {
			select {
			case <-p.quit:
				return nil
			default:
				return err
			}
		}

		p.addConn(conn)
		p.wg.Add(1)
		go func() {
			defer p.wg.Done()
			defer p.removeConn(conn)
			p.handleConn(conn)
		}()
	}
}

func (p *ReverseProxy) addConn(c net.Conn) {
	p.activeMu.Lock()
	defer p.activeMu.Unlock()
	p.active[c] = struct{}{}
}

func (p *ReverseProxy) removeConn(c net.Conn) {
	p.activeMu.Lock()
	defer p.activeMu.Unlock()
	delete(p.active, c)
	c.Close()
}

// Stop shuts down the proxy. Safe to call multiple times.
func (p *ReverseProxy) Stop() {
	p.stopOnce.Do(func() { close(p.quit) })
	p.lnMu.RLock()
	if p.ln != nil {
		p.ln.Close()
	}
	p.lnMu.RUnlock()
	p.activeMu.Lock()
	for c := range p.active {
		c.Close()
	}
	p.activeMu.Unlock()
	p.wg.Wait()
}

func (p *ReverseProxy) handleConn(client net.Conn) {
	upstream, err := net.Dial("tcp", p.cfg.Upstream)
	if err != nil {
		p.cfg.Logf("proxy: dial upstream %s failed: %v", p.cfg.Upstream, err)
		return
	}
	defer upstream.Close()

	reader := bufio.NewReader(client)
	upReader := bufio.NewReader(upstream)
	for {
		// 1. Read request headers
		req, err := http.ReadRequest(reader)
		if err != nil {
			if err != io.EOF {
				p.cfg.Logf("proxy: read request error: %v", err)
			}
			return
		}

		// 2. Rewrite Host header
		req.Host = p.cfg.RewriteHost
		req.URL.Host = p.cfg.Upstream // for Proxy transport if needed, but we write raw
		req.RequestURI = ""           // must be empty for Client.Do

		// Check if it's a WebSocket upgrade
		isWS := strings.ToLower(req.Header.Get("Upgrade")) == "websocket"

		// 3. Forward request
		// We use a custom round tripper or just write back to upstream
		if err := req.Write(upstream); err != nil {
			p.cfg.Logf("proxy: write request to upstream failed: %v", err)
			return
		}

		// 4. Handle Response
		resp, err := http.ReadResponse(upReader, req)
		if err != nil {
			p.cfg.Logf("proxy: read response from upstream failed: %v", err)
			return
		}

		// Rewrite body if it's JSON (targets list)
		if !isWS && strings.Contains(resp.Header.Get("Content-Type"), "application/json") {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()

			// Rewrite ws_urls
			body = p.rewriteWSUrls(body)
			resp.Body = io.NopCloser(bytes.NewReader(body))
			resp.ContentLength = int64(len(body))
			resp.Header.Set("Content-Length", fmt.Sprintf("%d", len(body)))
		}

		// Write response back to client
		if err := resp.Write(client); err != nil {
			p.cfg.Logf("proxy: write response to client failed: %v", err)
			return
		}

		// 5. If WebSocket, enter tunnel mode
		if resp.StatusCode == http.StatusSwitchingProtocols {
			p.tunnel(client, upstream, reader, upReader)
			return
		}

		if resp.Close || req.Close {
			return
		}
	}
}

func (p *ReverseProxy) rewriteWSUrls(body []byte) []byte {
	p.lnMu.RLock()
	proxyAddr := p.ln.Addr().String()
	p.lnMu.RUnlock()

	// We replace localhost, 127.0.0.1 and the actual upstream host
	replacements := []string{
		"ws://" + p.cfg.Upstream + "/devtools",
	}
	// Extract the port from RewriteHost; skip if it has none (avoids
	// producing "ws://localhost:localhost/devtools" when Index returns -1).
	if idx := strings.LastIndex(p.cfg.RewriteHost, ":"); idx >= 0 {
		replacements = append(replacements,
			"ws://localhost:"+p.cfg.RewriteHost[idx+1:]+"/devtools",
		)
	}
	replacements = append(replacements,
		"ws://localhost/devtools",
		"ws://127.0.0.1/devtools",
	)

	for _, old := range replacements {
		body = bytes.ReplaceAll(body, []byte(old), []byte("ws://"+proxyAddr+"/devtools"))
	}
	return body
}

func (p *ReverseProxy) tunnel(client, upstream net.Conn, clientBuf *bufio.Reader, upBuf *bufio.Reader) {
	errCh := make(chan error, 2)

	cp := func(dst io.Writer, src io.Reader, buf *bufio.Reader) {
		var err error
		if buf != nil && buf.Buffered() > 0 {
			// Flush buffered data first
			_, err = io.CopyN(dst, buf, int64(buf.Buffered()))
		}
		if err == nil {
			_, err = io.Copy(dst, src)
		}
		errCh <- err
	}

	go cp(upstream, client, clientBuf)
	go cp(client, upstream, upBuf)

	// Wait for the first goroutine to finish, then close both connections
	// to unblock the other goroutine. Wait for it too to avoid leaks.
	<-errCh
	client.Close()
	upstream.Close()
	<-errCh
}

func (p *ReverseProxy) Addr() string {
	p.lnMu.RLock()
	defer p.lnMu.RUnlock()
	if p.ln == nil {
		return ""
	}
	return p.ln.Addr().String()
}
