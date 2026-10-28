---
name: ghost-driver
description: Unified Browser Automation Skill for Stdio, SSE, and CLI modes. Includes automatic connection healing, complex form automation, and human learning mode.
version: 1.1.0
authors:
  - connor
---

# Ghost-Driver: Unified AI Browser Skill

You are a browser automation expert. You control the user's browser via the **Ghost-Driver MCP Server**.

## 🛠 Connection & Self-Healing (IMPORTANT)

If you cannot reach the tools, analyze the environment and guide the user:

### 1. Stdio Mode (Optimized for Local IDE / Claude Desktop)
- **Use Case**: Highest performance, direct filesystem access.
- **Config**: Instruct the user to add this to `mcp_settings.json`:
  ```json
  "ghost-driver": {
    "command": "[FULL_PATH_TO_EXE]",
    "args": ["--transport", "stdio"]
  }
  ```

### 2. SSE Mode (Best for Web Chat: ChatGPT / Claude.ai)
- **Use Case**: Universal compatibility, bypasses local process limits.
- **Downgrade Path**: If Stdio hangs or fails, ask the user to run:
  `ghost-driver --transport sse --port 8080`
- **Connection**: You will then connect via the SSE endpoint: `http://localhost:8080/sse`.

### 3. CLI Mode (Emergency / One-off)
- **Use Case**: Stdio/SSE protocols are blocked.
- **Guidance**: Tell the user to run commands directly:
  `ghost-driver --action navigate --url https://google.com`

---

## 🧰 Core Tools

- `connect_browser`: **Call this FIRST**. No args needed. Auto-detects Chrome (CDP) or Firefox (BiDi) — no manual configuration required.
- `navigate(url)`: Open a URL or known site alias.
- `get_content(format=markdown)`: Read page as structured, noise-reduced Markdown. Use `format=text` for plain text, `format=html` for raw HTML.
- `smart_click(description)`: Click elements by natural language. Supports `post_verify_selector` for React/Vue apps.
- `type_text(text)`: Type into fields with human-like rhythm.
- `enum_options(trigger)`: **CRITICAL for SPAs**. Enumerate hidden dropdown menus.
- `upload_files(files)`: Provide files to an open file chooser dialog (call after smart_click on an upload button).
- `get_tool_analytics`: Check your success rate and latency.

---

## 🎛 Toolset Selection (Token Optimization)

The `tools/list` response advertises dozens of browser-automation tools (~3,500 tokens). For token-constrained clients, selectively expose toolsets via the `GHOST_TOOLSETS` env var:

| Preset / Value | Toolsets | Use Case |
|----------------|----------|----------|
| `base` *(default)* / `all` / `dev` | 10 base toolsets (11 with `dev`) | `base` full automation; `all`/`dev` adds UI inspection tools (Claude Desktop, ample context) |
| `minimal` | basic, navigation, interaction, extract, page | Read-only browsing, no forms/auth |
| `basic,navigation,interaction,extract` | custom | Lightweight scraping |

**Available toolsets:** `basic`, `navigation`, `interaction`, `content`, `extract`, `page`, `advanced`, `auth`, `form`, `replay` (and `dev` for UI inspection tools)

```bash
# Minimal (saves ~2,000 tokens per tools/list)
GHOST_TOOLSETS=minimal ghost-driver --transport stdio

# Custom: scraping + forms, no auth
GHOST_TOOLSETS=basic,navigation,interaction,extract,form ghost-driver --transport stdio
```

> **Note:** `connect_browser` lives in the `basic` toolset — always include it.

---

## 🖥 Desktop App Automation (Electron / Tauri / Wails)

Ghost-Driver can automate **Web-based desktop apps** via the same CDP protocol used for Chrome. These apps embed Chromium/WebView2, so once the debug port is open, the DOM, JS injection, and all tools work identically. Firefox (BiDi) is also supported for web automation via a built-in CDP→BiDi translation layer.

### Supported frameworks

| Framework | `app_type` | Mechanism | Platform |
|-----------|-----------|-----------|----------|
| **Electron** (VSCode, Slack, Discord) | `electron` | `--remote-debugging-port=9222` CLI flag passes through to embedded Chromium | All OS |
| **Tauri / Wails** (Windows) | `webview2` | `WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS` env var enables CDP in Edge WebView2 | Windows |
| Already-running app with debug port | *(any)* | `mode=attached` connects to any CDP endpoint | All OS |

### Usage

```jsonc
// Auto-detect: just pass browser_path, app_type is inferred from the binary
connect_browser({
  "browser_path": "C:/Users/.../my-app.exe",
  "mode": "managed"
})
// Response: "connected (auto_mode=managed, app_type=webview2) — ..."

// Explicit Electron
connect_browser({
  "app_type": "electron",
  "browser_path": "C:/Users/.../Code.exe",
  "mode": "managed"
})

// Already running (any framework): just attach
connect_browser({
  "mode": "attached",
  "devtools_http": "http://127.0.0.1:9222"
})
```

### Limitations
- **Canvas/WebGL apps** (Figma, Excalidraw): DOM is a single `<canvas>` — non-visual tools can't see internal elements. Fall back to `vision_analyze` / screenshot-based clicking.
- **macOS Tauri/Wails (WKWebView)**: No external escape hatch — the app must be built with `developerExtrasEnabled = YES`. Use `mode=attached` if the app already has a debug port.
- **Linux Tauri (WebKitGTK)**: Uses WebKit Inspector Protocol, not CDP — not supported by Ghost-Driver.

---

## 🚦 Operational Guardrails

1. **Stability**: If a connection drops, simply retry. Ghost-Driver heals sessions automatically.
2. **Rule 1 (Data Safety)**: After `type_text`, the page is "Dirty". **DO NOT** use `navigate` in the same tab to avoid blocking dialogs. Use `new_tab`.
3. **Form Attribution**: If an error message appears (e.g., "Invalid input"), look for the `⚠️ CAUSALITY ALERT` in tool responses to find the responsible step.
4. **UI Verification**: If a click seems to have no effect on custom components, use `post_verify_selector` to trigger the "Force Dispatch" fallback.

---
*Ghost-Driver: The Atomic Bridge between LLMs and the Web.*
