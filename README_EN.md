[English](README_EN.md) | [中文](README.md)

# Ghost-Driver MCP

> A browser automation MCP Server built in Go based on native CDP / WebDriver BiDi.
> Attaches to existing browser sessions, preserving login states, with built-in screen recording, VLM visual positioning, and virtual list extraction.
> Auto-detects and adapts to Chrome (CDP) and Firefox (BiDi) with zero manual configuration.

---

## Installation

### Download prebuilt binary (recommended)

Download the zip for your platform from [GitHub Releases](https://github.com/Daybeam/ghost-driver/releases), unzip, and run:

```bash
./ghost-driver --version  # verify → ghost-driver v1.1.0
```

Platforms: Windows amd64, Linux amd64, macOS Intel, macOS Apple Silicon.

### Build from source

```bash
git clone https://github.com/Daybeam/ghost-driver.git
cd ghost-driver
go build -o ghost-driver ./cmd/ghost-driver
```

### Start browser with debug port

```bash
chrome --remote-debugging-port=9222    # Chrome / Edge
firefox --remote-debugging-port=9222   # Firefox
```

Or skip this — `connect_browser` in `managed` mode auto-launches the browser.

---

## Design Philosophy

Most browser automation tools assume the browser is a "clean container" — they launch a new instance, inject drivers, and then close it.
This fails in real-world scenarios:

- Logged-in sessions (Cookies, Tokens) are required.
- Anti-bot mechanisms detect automation flags.
- Single Page Applications (SPAs) lack traditional `load` events.
- Virtual lists only render visible rows.

Ghost-Driver's approach is to **attach to your already running browser** (Chrome or Firefox) rather than launching a fresh one.

---

## Architecture

```
LLM / Claude
  └── Ghost-Driver MCP (Go binary, stdio or SSE)
        ├── internal/cdp/
        │     ├── client.go      Raw CDP WebSocket with Host header injection
        │     └── targets.go     /json/list target discovery + browser detection
        ├── internal/bidi/
        │     ├── client.go      WebDriver BiDi WebSocket client (Firefox)
        │     └── adapter.go     CDP → BiDi command/event/response translation layer
        ├── internal/browser/
        │     ├── browser.go     Navigation / Screenshot / JS execution / Connection lifecycle
        │     ├── input.go       Dual-track input: CDP smooth animation OR xdotool (OS-level)
        │     ├── indicator.go   Visual control indicator (CDP injection, no extension required)
        │     ├── recorder.go    Frame sequence → FFmpeg GIF / MP4
        │     ├── dom.go         HTML→Markdown / Semantic lookup / Text extraction
        │     ├── waiter.go      SPA smart waiting (MutationObserver + Network silence)
        │     ├── virtual_list.go Virtual list scroll extraction
        │     └── selector.go    Self-healing selector (3-tier fallback)
        ├── internal/vision/
        │     └── vlm.go         VLM (Gemini/OpenAI/Claude) → Pixel coordinates
        ├── internal/predict/
        │     └── predictor.go   Link prefetch / element prediction
        ├── internal/profile/
        │     └── profile.go     Site profile loading and hot-reload
        ├── internal/audit/
        │     └── audit.go       Tool call audit log (JSONL)
        ├── internal/config/
        │     └── config.go      Environment variables and build config
        ├── internal/resources/
        │     └── resources.go   Embedded JS resources (stealth, extraction, recorder)
        ├── internal/mcp/
        │     └── server.go      MCP tool definitions and dispatching
        └── cmd/ghost-driver/
              └── main.go        CLI entrypoint (MCP mode & single-execution CLI mode)
```

### Memory & Long-term Navigation
Ghost-Driver features a built-in SQLite-based topological memory system:
- **Persistent Memory**: All explored states (URL+Title) and actions are automatically saved to `./memory/topology.db`.
- **Domain Isolation**: Supports domain-scoped map retrieval to prevent cross-site logic contamination.
- **Self-Healing Guidance**: The agent automatically bypasses "dead-end" nodes that previously caused loops or errors based on historical records.

---

## Chrome 150+ Compatibility & Stability Enhancements

Ghost-Driver is deeply optimized for recent Chrome security policies and long-connection stability:
- **Zero-Config Connection**: `connect_browser` defaults to `127.0.0.1:9222`, automatically probes port status, and smartly toggles between `Attached` and `Managed` modes.
- **Transparent CDP Proxy**: Automatically detects container environments (e.g. `host.docker.internal`) and silently mounts an embedded reverse proxy to bypass Chrome's Host header validation.
- **Auto-Reconnect Guard**: Performs health checks prior to any tool execution. If Chrome restarts or WebSocket drops, it silently reconnects and restores the previous input mode (l1/l2/l3).
- **BeforeUnload Auto-Defense**: Automatically strips page `onbeforeunload` handlers before navigation to prevent modal popups from blocking CDP.

---

## Browser Support & Auto-Detection

Ghost-Driver **auto-detects** the connected browser type without any manual setup:

| Browser | Protocol | Detection Method | Support Status |
|:---|:---|:---|:---|
| **Chrome / Edge / Chromium** | CDP (Chrome DevTools Protocol) | `/json/version` returns 200 | ✅ Fully Supported |
| **Firefox 157+** | WebDriver BiDi | `/json/version` returns 404 | ✅ Core Features Supported |
| **Electron / Tauri / Wails** | CDP (Embedded Chromium) | Same as Chrome | ✅ Fully Supported |

### How It Works

1. Upon connecting to the debug port, `connect_browser` issues an HTTP request to `/json/version`.
2. **200 OK** → Identified as Chrome/CDP, uses native CDP WebSocket channel.
3. **404 Not Found** → Identified as Firefox/BiDi, automatically switches to WebDriver BiDi adapter layer.
4. All subsequent tool invocations are transparently routed to the corresponding protocol implementation.

### Starting Firefox

```bash
# Launch Firefox with BiDi remote debugging port enabled
firefox.exe --remote-debugging-port 9222 --headless
```

In `managed` mode, Ghost-Driver automatically searches for and launches Firefox if available.

### Firefox (BiDi) Capability Matrix

Since Firefox uses WebDriver BiDi instead of CDP, the following capabilities differ:

| Feature | Chrome (CDP) | Firefox (BiDi) | Impact |
|:---|:---:|:---:|:---|
| Navigation / JS Execution / Screenshot | ✅ | ✅ | — |
| Click / Type / Scroll / Hover | ✅ | ✅ | — |
| `get_content` | ✅ | ✅ | — |
| Cookie / Storage Management | ✅ | ✅ | — |
| Tab Management | ✅ | ✅ | — |
| `smart_click` / `wait_smart` | ✅ | ✅ | — |
| Dialog Handling (`handle_dialog`) | ✅ | ✅ | — |
| Device Emulation / User-Agent Override | ✅ | ✅ | — |
| Screen Recording (`start/stop_recording`) | ✅ | ✅ | Recording relies on `Screenshot` polling, independent of `startScreencast` |
| Full-page Screenshot | ✅ | ✅ | BiDi `captureScreenshot` supports `origin:"document"` + `clip` |
| Accessibility Tree | ✅ | ✅ (polyfill) | Firefox uses JS DOM traversal + ARIA attributes to simulate AX tree |
| File Chooser Interception (`upload_files`) | ✅ | ❌ | BiDi lacks file picker protocol; use `eval_js` to set `<input type=file>` |
| `Network.getResponseBody` | ✅ | ❌ | BiDi lacks response body retrieval; use `browser_fetch` or `eval_js` + `fetch()` |

---

## Operating Modes

### 1. MCP Mode (Standard)

For integration into Claude Desktop or other MCP-compatible IDEs/clients.

```bash
# stdio mode
./ghost-driver --transport stdio

# SSE mode
./ghost-driver --transport sse --port 8080
```

### 2. CLI Mode (Non-interactive)

Designed for direct terminal invocations or LLM scripts:

```bash
# Navigate to URL
./ghost-driver --action navigate --url "https://google.com"

# Extract page text
./ghost-driver --action get_text

# Capture screenshot
./ghost-driver --action screenshot --out "debug.png"

# Execute JS
./ghost-driver --action eval --js "document.title"
```

---

## Docker (Connecting to Host Chrome from Container)

```bash
docker run --rm -i -e ANTHROPIC_API_KEY=$ANTHROPIC_API_KEY ghost-driver --transport stdio
```

Connect to host browser inside an MCP session:

```json
connect_browser { "devtools_http": "http://host.docker.internal:9222" }
```

---

## Claude Desktop Configuration

```json
{
  "mcpServers": {
    "ghost-driver": {
      "command": "/path/to/ghost-driver",
      "args": ["--transport", "stdio"],
      "env": {
        "ANTHROPIC_API_KEY": "sk-ant-..."
      }
    }
  }
}
```

---

## MCP Tools Overview

### Connection Management

| Tool | Parameters | Description |
|---|---|---|
| `connect_browser` | `devtools_http`, `mode`, `input_mode` | Connect to browser. Zero-argument startup supported. Auto handles mode detection, remote proxy, and reconnects. |
| `page_info` | — | Returns current URL, title, and Context Hints (SOP suggestions) |
| `check_system_ready` | — | Diagnoses environment readiness |
| `list_tabs` | — | List all open tabs |
| `new_tab` | `url` | Open a new tab |
| `switch_tab` | `target_id`* | Switch active tab |
| `close_tab` | `target_id` | Close specified tab |

### Navigation

| Tool | Parameters | Description |
|---|---|---|
| `navigate` | `url`* | Navigate and wait for load event |
| `navigate_back` | — | Go back in history |
| `navigate_forward` | — | Go forward in history |
| `reload` | — | Reload page |
| `wait_smart` | `max_ms` | SPA smart wait (triple check: Network + DOM + Visual) |
| `wait_for_selector` | `selector`*, `timeout_ms` | Wait for CSS selector (supports Shadow DOM) |
| `wait_for_text` | `text`*, `timeout_ms` | Wait for text on page |

### Interaction

| Tool | Parameters | Description |
|---|---|---|
| `click` | `x`*, `y`* | Smooth cursor movement + click coordinates |
| `hover` | `x`*, `y`* | Smooth cursor movement (hover without click) |
| `smart_click` | `description`*, `selectors` | 3-tier self-healing click (CSS → DOM penetration → VLM) |
| `type_text` | `text`* | Simulate typing and mark page state as Dirty |
| `browser_fetch` | `url`*, `method`... | Browser context API call (Rule 2) |
| `scroll` | `dy`*, `x`, `y`, `container_selector` | Scroll page or target container |

### Content Extraction & State

| Tool | Parameters | Description |
|---|---|---|
| `get_content` | `format`, `selector`, `mode` | Get page content (text/markdown/html, default markdown) |
| `get_topology_map` | `domain` | Get state transition graph |
| `mark_dead_end` | — | Mark current node as dead end |
| `get_accessibility_tree` | — | Extract Accessibility tree |
| `get_cookies` | — | Get all Cookies |
| `set_cookie` | `name`*, `value`*, `domain`*... | Set Cookie |
| `read_storage` | `scope` | Read LocalStorage/SessionStorage (scope: local/session) |
| `write_storage` | `scope`, `key`*, `value`* | Write LocalStorage/SessionStorage |
| `remove_storage` | `scope`, `key`* | Remove LocalStorage/SessionStorage key |
| `get_metadata` | — | Return title, URL, og: tags, canonical |
| `extract_virtual_list` | `item_selector`*, `container_selector`, `max_items`, `scroll_step`, `settle_ms` | Extract full virtual list items |
| `eval_js` | `expression`* | Execute JS in page context |

### Screenshot & Recording

| Tool | Parameters | Description |
|---|---|---|
| `screenshot` | — | Capture viewport as base64 PNG |
| `start_recording` | `fps` | Start screen recording (default 8fps) |
| `stop_recording` | `out_path`* | Stop and render GIF / MP4 via FFmpeg |

### Vision

| Tool | Parameters | Description |
|---|---|---|
| `vision_analyze` | `prompt`*, `image` | VLM visual analysis, returns element coordinates |
| `pixel_click` | `x`*, `y`* | Click pixel coordinates directly (visual fallback) |

### Site Profiles

| Tool | Parameters | Description |
|---|---|---|
| `discover_site_selectors` | — | Auto-discover input and button selectors on the page |
| `save_site_profile` | `name`*, `yaml_content`* | Save site profile to `./profiles/*.yaml` |

### Tool Merge (`GHOST_LEGACY_TOOLS`)

The following tools have been merged into unified entry points:

| Old Tool | New Tool | Parameters |
|----------|----------|------------|
| `get_text` / `get_markdown` / `get_html` | `get_content` | `format: text\|markdown\|html` |
| `get_local_storage` / `get_session_storage` | `read_storage` | `scope: local\|session` |
| `set_local_storage` / `set_session_storage` | `write_storage` | `scope, key, value` |
| `remove_local_storage` / `remove_session_storage` | `remove_storage` | `scope, key` |

Set `GHOST_LEGACY_TOOLS=1` to keep old tools alongside new ones (backward compat).

---

## Input Modes

| Mode | isTrusted | Use Case |
|---|---|---|
| `cdp` (default) | `false` | Standard websites, SPAs |
| `os` (xdotool) | `true` | Anti-bot detection, strict `isTrusted` verification |

---

## Visual Control Indicator

Upon successful connection, a **glowing blue border** and top-left badge appear on the browser window, requiring no extensions.

| State | Color |
|---|---|
| Idle | 🔵 Blue (`#00d4ff`, pulse animation) |
| Moving Cursor | 🟡 Yellow (`#ffcc00`) |
| Click Moment | 🔴 Red (`#ff4444`) |
| Typing | 🟣 Purple (`#aa44ff`) |
| Recording | 🔴 Red (`#ff2222`, fast pulse) |

---

## Advanced Features & AI Self-Configuration

### 1. Site Profiles
Supports declarative YAML site profiles to store site-specific knowledge. Located at `./profiles/*.yaml`.

### 2. AI Self-Bootstrapping
When an Agent encounters an unconfigured site, it follows this workflow:
1. **Explore**: `navigate` to target site.
2. **Discover**: Call `discover_site_selectors()`.
3. **Verify**: Check best selectors using `get_accessibility_tree()` and `screenshot()`.
4. **Save**: Call `save_site_profile(name, yaml_content)`.

---

## Agent Skill Integration

Ghost-Driver can be loaded as a **Skill plugin** by AI Agent platforms (Claude Code, Hermes, OpenCode, Cursor, etc.).

### Usage

`GHOST_DRIVER_SKILL.md` is a **reference skill file** documenting the tool catalog and operational rules.
Different agent platforms have different skill schemas, prompt styles, and workflow conventions — **a verbatim copy is rarely optimal**.
Use it as a starting point and rewrite it for your agent's specific format:

```bash
cp GHOST_DRIVER_SKILL.md <your_agent_skills_dir>/ghost-driver/
# Then adapt it to your agent's skill schema
```
