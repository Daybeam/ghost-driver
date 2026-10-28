# Ghost-Driver Architecture Analysis

## 1. Project Overview

**Ghost-Driver** is a browser automation MCP (Model Context Protocol) server built in Go. It provides a unified interface for controlling web browsers (Chrome/Chromium via CDP and Firefox via WebDriver BiDi) through AI agents like Claude Desktop.

### Primary Use Case
- Browser automation for AI agents (Claude, GPT, etc.)
- Web scraping and data extraction
- Automated testing and form filling
- Visual element location using VLM (Vision Language Models)
- Screen recording and session replay

### Target Users
- AI/ML developers building browser automation agents
- QA engineers needing browser automation tools
- Data extraction specialists
- Developers automating web workflows

---

## 2. Directory Structure

```
ghost-driver/
├── cmd/ghost-driver/          # CLI entry point
│   └── main.go               # Main application entry
├── internal/                  # Internal packages (not importable by external projects)
│   ├── audit/                # Audit logging and telemetry
│   ├── bidi/                 # WebDriver BiDi protocol client (Firefox)
│   ├── browser/              # Browser orchestration and high-level operations
│   ├── cdp/                  # Chrome DevTools Protocol client
│   ├── config/               # Configuration management
│   ├── mcp/                  # MCP server implementation
│   ├── predict/              # Action prediction and pacing
│   ├── profile/              # Site profile management
│   ├── resources/            # Embedded JavaScript resources
│   └── vision/               # VLM-based visual element detection
├── profiles/                 # Site-specific YAML profiles
├── scripts/                  # Build and utility scripts
├── go.mod/go.sum             # Go module definition
├── Makefile                  # Build targets
└── Dockerfile                # Container image definition
```

---

## 3. Key Components

### 3.1 Protocol Layer (`internal/cdp/` and `internal/bidi/`)

**CDP Client** (`internal/cdp/client.go`)
- Raw WebSocket client for Chrome DevTools Protocol
- Handles connection management, message routing, and event dispatching
- Features:
  - Automatic reconnection with backoff
  - Host header injection for Chrome 150+ security bypass
  - Session management for multi-tab support
  - Keep-alive mechanism (25s ping interval)
  - Concurrent event handler dispatch with semaphore-based backpressure

**BiDi Client** (`internal/bidi/client.go`)
- WebDriver BiDi WebSocket client for Firefox
- Implements the same interface as CDP client
- Features:
  - Session initialization and context management
  - Event translation (BiDi → CDP)
  - Origin header suppression for Firefox compatibility

**BiDi Adapter** (`internal/bidi/adapter.go`)
- Translates CDP commands to BiDi equivalents
- Maps CDP events to BiDi events
- Handles Firefox-specific limitations gracefully

### 3.2 Browser Orchestration Layer (`internal/browser/`)

**Manager** (`manager.go`)
- Browser lifecycle management (start, stop, attach)
- Supports two modes:
  - **Attached**: Connect to existing browser instance
  - **Managed**: Start browser automatically
- Auto-detects browser type (Chrome, Firefox, Electron, WebView2)
- Handles CDP proxy for remote/Docker connections

**Browser** (`browser.go`)
- High-level browser operations wrapper
- Key responsibilities:
  - Navigation (navigate, back, forward, reload)
  - Screenshot capture (full page, element, viewport)
  - JavaScript execution
  - DOM extraction and markdown conversion
  - Accessibility tree extraction
  - Stealth mode injection
  - Dialog handling
  - File chooser interception

**Input** (`input.go`)
- Multi-mode input simulation:
  - **L1**: DOM-level events (fastest, least realistic)
  - **L2**: CDP Input protocol (recommended for most sites)
  - **L3**: OS-level physical injection (xdotool, for anti-bot bypass)
- Features smooth cursor animation with configurable jitter and steps

**Recorder** (`recorder.go`)
- Screen recording to GIF/MP4 via FFmpeg
- Frame capture and encoding pipeline

**Virtual List Extraction** (`virtual_list.go`)
- Handles infinite scroll and virtualized lists
- Automatic scroll, collect, and deduplicate

**Smart Waiter** (`waiter.go`)
- SPA-aware waiting mechanism
- Three-condition detection:
  1. Network idle (no pending XHR/fetch)
  2. DOM quiet (no mutations for 300ms)
  3. Visual stable (screenshot hash unchanged)

**Topology Memory** (`topology.go`)
- SQLite-based navigation graph
- Tracks page transitions and state changes
- Supports domain isolation and dead-end marking

### 3.3 MCP Server Layer (`internal/mcp/`)

**Server** (`server.go`)
- MCP protocol implementation using `mark3labs/mcp-go`
- Two transport modes:
  - **stdio**: For CLI integration (Claude Desktop)
  - **SSE**: For HTTP-based clients
- Tool registration with configurable toolsets
- Audit wrapper for all tool calls
- Connection state management with auto-reconnect

**Tools** (tools_*.go)
- 40+ MCP tools organized by category:
  - **Basic**: connect_browser, pixel_click, cookies, debug
  - **Navigation**: navigate, reload, wait_smart, wait_for_selector
  - **Interaction**: click, hover, type_text, scroll, smart_click
  - **Content**: get_text, get_markdown, get_html, eval_js
  - **Extract**: extract_virtual_list, get_accessibility_tree
  - **Page**: screenshot, start_recording, stop_recording
  - **Advanced**: browser_fetch, eval_js
  - **Auth**: OAuth callback handling
  - **Replay**: Scenario recording and playback

### 3.4 Vision Layer (`internal/vision/`)

**VLM Finder** (`vlm.go`)
- Visual element detection using Vision Language Models
- Supports multiple providers:
  - Gemini (default)
  - OpenAI GPT-4o
  - Anthropic Claude
- Returns pixel coordinates from screenshot + text description
- Falls back to screenshot when API key is unavailable

### 3.5 Supporting Components

**Configuration** (`internal/config/config.go`)
- Environment variable-based configuration
- Tuning parameters for mouse, typing, viewport, and wait conditions

**Audit** (`internal/audit/audit.go`)
- Structured logging of all tool calls
- Telemetry and health monitoring
- JSONL output to `./memory/audit.jsonl`

**Profile Registry** (`internal/profile/profile.go`)
- Site-specific YAML profiles
- Hot-reloading via fsnotify
- URL pattern matching for automatic profile selection
- Selector storage and fingerprinting

**Resources** (`internal/resources/`)
- Embedded JavaScript libraries:
  - Stealth scripts (Chrome & Firefox)
  - DOM extraction utilities
  - Accessibility tree generation
  - Wait harness
  - Teaching mode

---

## 4. Entry Points

### 4.1 CLI Entry Point (`cmd/ghost-driver/main.go`)

Two operational modes:

**MCP Mode** (stdio/SSE):
```bash
./ghost-driver --transport stdio
./ghost-driver --transport sse --port 8080
```

**CLI Mode** (non-interactive):
```bash
./ghost-driver --action navigate --url "https://google.com"
./ghost-driver --action screenshot --out debug.png
./ghost-driver --action eval --js "document.title"
```

### 4.2 External Input Boundaries

1. **MCP Protocol**: JSON-RPC over stdio or HTTP/SSE
2. **CDP WebSocket**: Browser debugging protocol
3. **BiDi WebSocket**: Firefox WebDriver BiDi
4. **HTTP Endpoints**:
   - `/auth/callback` - OAuth callback handler
   - `/sse` - SSE transport endpoint
5. **File System**:
   - `./profiles/*.yaml` - Site profiles
   - `./memory/` - Audit logs, topology DB, action cache
   - `./memory/auth.json` - Persisted authentication

---

## 5. Data Flow

### 5.1 MCP Tool Call Flow

```
MCP Client (Claude)
    │
    ▼
MCP Server (stdio/SSE)
    │
    ▼
State Manager (connection tracking)
    │
    ▼
Browser Manager (lifecycle)
    │
    ▼
Protocol Layer (CDP/BiDi)
    │
    ▼
Browser WebSocket
```

### 5.2 Navigation Flow

```
User → navigate(url)
    │
    ├─→ Browser.Navigate()
    │       │
    │       ├─→ CDP Page.navigate
    │       │
    │       └─→ SmartWait()
    │               │
    │               ├─→ Network idle check
    │               ├─→ DOM quiet check
    │               └─→ Visual stability check
    │
    └─→ Return to MCP client
```

### 5.3 Visual Element Detection Flow

```
smart_click(description)
    │
    ├─→ Layer 1: CSS selector match
    │       └─→ Click if found
    │
    ├─→ Layer 2: DOM traversal with fingerprints
    │       └─→ Click if found
    │
    └─→ Layer 3: VLM visual detection
            │
            ├─→ Screenshot
            ├─→ VLM API (Gemini/OpenAI/Anthropic)
            └─→ Pixel coordinate → Click
```

---

## 6. Technology Stack

### Primary Language
- **Go 1.25.5**

### Core Dependencies
| Package | Purpose |
|---------|---------|
| `github.com/mark3labs/mcp-go` | MCP protocol implementation |
| `github.com/gorilla/websocket` | WebSocket client for CDP/BiDi |
| `github.com/fsnotify/fsnotify` | File system watcher for hot-reload |
| `gopkg.in/yaml.v3` | YAML parsing for site profiles |
| `modernc.org/sqlite` | SQLite driver (pure Go) |

### External Tools
- **FFmpeg**: Screen recording to GIF/MP4
- **xdotool**: OS-level input injection (L3 mode, Linux only)

### Browser Support
| Browser | Protocol | Status |
|---------|----------|--------|
| Chrome/Chromium | CDP | Full support |
| Firefox 157+ | WebDriver BiDi | Core support |
| Electron | CDP | Full support |
| WebView2 (Tauri/Wails) | CDP | Full support |
| Edge/Brave/Opera | CDP | Full support |

---

## 7. Trust Boundaries & Security Considerations

### 7.1 Input Validation
- **MCP Arguments**: Parsed and validated via mcp-go schema
- **URLs**: Validated via `net/url` parsing
- **File Paths**: Sanitized to prevent path traversal (profile registry)
- **JavaScript**: Executed in browser context (potential XSS if browser is compromised)

### 7.2 Authentication
- OAuth callback handler at `/auth/callback`
- API keys stored in environment variables or `./memory/auth.json`
- No built-in authentication for SSE transport (relies on network isolation)

### 7.3 Network Boundaries
- **Outbound**: VLM API calls (Gemini/OpenAI/Anthropic)
- **Inbound**: MCP connections (stdio/SSE)
- **Browser**: WebSocket connections to CDP/BiDi endpoints

### 7.4 Data Persistence
- `./memory/topology.db` - Navigation graph (SQLite)
- `./memory/audit.jsonl` - Tool call audit log
- `./memory/action_cache.json` - Cached action results
- `./memory/scenarios.json` - Recorded scenarios
- `./memory/auth.json` - Persisted authentication tokens

### 7.5 Security Features
- **Stealth Mode**: Anti-bot evasion scripts
- **Host Header Injection**: Chrome 150+ compatibility
- **Auto-Reconnect**: Graceful recovery from browser crashes
- **Dialog Handling**: Auto-dismiss of beforeunload and alerts

---

## 8. Architecture Diagrams

### 8.1 Component Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                        MCP Client Layer                         │
│  (Claude Desktop / IDE / CLI)                                   │
└───────────────────────────────┬─────────────────────────────────┘
                                │ JSON-RPC (stdio/SSE)
                                ▼
┌─────────────────────────────────────────────────────────────────┐
│                      MCP Server Layer                           │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────────────────┐  │
│  │ Tool Router │  │ State Mgr   │  │ Audit Logger            │  │
│  └─────────────┘  └─────────────┘  └─────────────────────────┘  │
└───────────────────────────────┬─────────────────────────────────┘
                                │ Browser API
                                ▼
┌─────────────────────────────────────────────────────────────────┐
│                     Browser Orchestration                       │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────────────────┐  │
│  │ Manager     │  │ Browser     │  │ Input (L1/L2/L3)        │  │
│  └─────────────┘  └─────────────┘  └─────────────────────────┘  │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────────────────┐  │
│  │ Recorder    │  │ Waiter      │  │ Topology Memory         │  │
│  └─────────────┘  └─────────────┘  └─────────────────────────┘  │
└───────────────────────────────┬─────────────────────────────────┘
                                │ Protocol
                                ▼
┌─────────────────────────────────────────────────────────────────┐
│                      Protocol Layer                             │
│  ┌─────────────────────────┐  ┌───────────────────────────────┐ │
│  │ CDP Client (Chrome)     │  │ BiDi Client (Firefox)         │ │
│  │  - WebSocket            │  │  - WebSocket                  │ │
│  │  - Session Management   │  │  - Event Translation          │ │
│  └─────────────────────────┘  └───────────────────────────────┘ │
└───────────────────────────────┬─────────────────────────────────┘
                                │ WebSocket
                                ▼
┌─────────────────────────────────────────────────────────────────┐
│                      Browser Process                            │
│  (Chrome / Firefox / Electron / WebView2)                       │
└─────────────────────────────────────────────────────────────────┘
```

### 8.2 Data Flow for Smart Click

```
┌──────────────────────────────────────────────────────────────┐
│                  smart_click(description)                    │
└──────────────────────────────┬───────────────────────────────┘
                               │
                               ▼
                    ┌─────────────────────┐
                    │  Layer 1: CSS       │
                    │  Selector Match     │
                    └──────────┬──────────┘
                               │ Found?
                    ┌──────────┴──────────┐
                    │ Yes                 │ No
                    ▼                     ▼
            ┌─────────────┐       ┌─────────────────────┐
            │ Click &     │       │ Layer 2: DOM        │
            │ Return      │       │ Traversal +         │
            └─────────────┘       │ Fingerprint Match   │
                                  └──────────┬──────────┘
                                             │ Found?
                                  ┌──────────┴──────────┐
                                  │ Yes                 │ No
                                  ▼                     ▼
                          ┌─────────────┐     ┌─────────────────────┐
                          │ Click &     │     │ Layer 3: VLM        │
                          │ Return      │     │ Visual Detection    │
                          └─────────────┘     └──────────┬──────────┘
                                                         │
                                                         ▼
                                              ┌─────────────────────┐
                                              │ 1. Screenshot       │
                                              │ 2. VLM API Call     │
                                              │ 3. Parse Coords     │
                                              │ 4. Pixel Click      │
                                              └─────────────────────┘
```

---

## 9. Performance Characteristics

### 9.1 Concurrency Model
- **Connection State**: Protected by RWMutex for concurrent reads
- **Connect Operations**: Serialized via Mutex (prevents race conditions)
- **Event Handlers**: Bounded goroutine pool (semaphore with 32 slots)
- **CDP Calls**: Synchronous with context cancellation

### 9.2 Resource Management
- **WebSocket Connections**: One per browser tab
- **Memory**: Channel pooling for CDP responses
- **File Handles**: Audit log kept open for performance
- **Goroutines**: One readLoop + one keepAlive per connection

### 9.3 Latency Considerations
- **CDP Round-trip**: ~1-10ms (local), ~50-100ms (remote/Docker)
- **VLM API Call**: ~1-5s (depends on provider and network)
- **Smart Wait**: Up to 15s (configurable)
- **Screenshot**: ~50-200ms (depends on page size)

---

## 10. Extensibility Points

### 10.1 Site Profiles
- YAML-based configuration in `./profiles/`
- Hot-reloaded via fsnotify
- Custom selectors, hints, and preload scripts

### 10.2 Custom Scripts
- `eval_js()` tool for injecting and executing JavaScript expressions
- Preload scripts in site profiles
- Stealth mode injection

### 10.3 VLM Providers
- Pluggable architecture (Gemini, OpenAI, Anthropic)
- Configurable via environment variables
- Fallback to screenshot when API key unavailable

### 10.4 Toolsets
- Configurable via `GHOST_TOOLSETS` environment variable
- Presets: `base`, `dev`, `all`, `minimal`
- Custom comma-separated list

---

## 11. Conclusion

Ghost-Driver is a browser automation server with:

- **Clean separation of concerns**: Protocol layer, browser orchestration, MCP server
- **Multi-browser support**: CDP for Chrome, BiDi for Firefox
- **Robust error handling**: Auto-reconnect, graceful degradation
- **Extensible design**: Site profiles, custom scripts, pluggable VLM providers
- **Security awareness**: Stealth mode, input validation, path sanitization

The codebase demonstrates solid Go practices with proper context propagation, mutex protection, and resource cleanup. The architecture supports both interactive (MCP) and batch (CLI) use cases effectively.
