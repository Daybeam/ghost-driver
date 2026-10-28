package cdp

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestReverseProxy_Rewrites(t *testing.T) {
	// 1. Setup mock upstream server
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check Host header
		if !strings.HasPrefix(r.Host, "localhost:") {
			t.Errorf("expected Host to start with localhost:, got %s", r.Host)
		}

		if r.URL.Path == "/json/version" {
			w.Header().Set("Content-Type", "application/json")
			// Return a body with ws urls matching the current server address
			io.WriteString(w, `{"webSocketDebuggerUrl": "ws://`+r.Host+`/devtools/browser/123"}`)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	upstreamAddr := strings.TrimPrefix(upstream.URL, "http://")

	// 2. Start proxy
	proxy := NewProxy(ProxyConfig{
		ListenAddr: "127.0.0.1:0",
		Upstream:   upstreamAddr,
	})
	go proxy.Start()
	defer proxy.Stop()

	// Wait for proxy to start
	time.Sleep(100 * time.Millisecond)
	proxyAddr := proxy.Addr()

	// 3. Make request through proxy
	client := &http.Client{}
	resp, err := client.Get("http://" + proxyAddr + "/json/version")
	if err != nil {
		t.Fatalf("failed to get /json/version through proxy: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read body: %v", err)
	}

	expectedWS := "ws://" + proxyAddr + "/devtools"
	if !bytes.Contains(body, []byte(expectedWS)) {
		t.Errorf("expected body to contain %s, got %s", expectedWS, string(body))
	}
}
