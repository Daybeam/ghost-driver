[English](USER_GUIDE_EN.md) | [中文](USER_GUIDE.md)

# Ghost-Driver User Guide

Ghost-Driver is a browser automation tool that attaches to your already running browser (Chrome or Firefox), preserving your logged-in session state (Cookies) while avoiding complex anti-bot detections.

## Quick Start

### 1. Install Browser
Ensure **Google Chrome**, **Microsoft Edge**, or **Mozilla Firefox** is installed on your machine. Ghost-Driver will automatically attempt to locate their executable paths.

### 2. Startup Modes

#### Mode A: Attach to Already Open Browser (Recommended)
If you wish to retain logged-in web sessions (e.g. GitHub, Gmail):
1. Completely close all running browser instances.
2. Launch the browser with remote debugging port enabled via terminal/command line:
   - **Chrome (Windows)**: `chrome.exe --remote-debugging-port=9222 --user-data-dir="C:\temp\ghost-profile"`
   - **Chrome (macOS)**: `/Applications/Google\ Chrome.app/Contents/MacOS/Google\ Chrome --remote-debugging-port=9222 --user-data-dir="/tmp/ghost-profile"`
   - **Firefox**: `firefox.exe --remote-debugging-port=9222`
3. Run Ghost-Driver. It automatically connects to `127.0.0.1:9222` and identifies the browser type.

#### Mode B: Auto Mode
If you do not need to preserve a specific login session, Ghost-Driver automatically attempts to **launch a new browser window** upon connection failure. Simply launch Ghost-Driver and invoke `connect_browser`. Ghost-Driver automatically detects and launches Chrome or Firefox installed on your system.

#### Mode C: CLI Non-Interactive Mode
Designed for LLM execution or quick scripting, performing actions directly via command line arguments:
- `ghost-driver --action navigate --url "https://google.com"`
- `ghost-driver --action screenshot --out "capture.png"`
- `ghost-driver --action get_text`

#### Mode D: SSE Remote Connection Mode (High Compatibility)
Ideal for web environments such as ChatGPT or Claude.ai web interfaces that cannot run local processes directly:
1. Run in terminal: `ghost-driver --transport sse --port 8080`.
2. Direct the AI assistant to connect to `http://localhost:8080/sse`.
3. Control is maintained via a persistent HTTP stream, preserving browser session state even if chat history resets.

## Automatic Browser Detection

Ghost-Driver **requires no manual browser configuration**:
- **Chrome / Edge / Electron**: Communicates via CDP (Chrome DevTools Protocol).
- **Firefox 157+**: Communicates via WebDriver BiDi protocol with built-in CDP→BiDi translation layer.

All MCP tools are transparent to callers regardless of the underlying browser protocol.

### Firefox Capability Notes
Firefox (BiDi) supports all core operations: Navigation, Screenshot, Click, Type, JS execution, Content extraction, Cookie/Storage management, and Tab management. The following capabilities are restricted due to protocol boundaries:
- `upload_files` (File picker interception)
- `get_accessibility_tree` (Full accessibility tree)
- `start_recording` / `stop_recording` (Screen recording)
- Full-page screenshot beyond viewport (Firefox screenshot is constrained to current viewport)

## Core Operational Rules (The Playbook)

To ensure reliable AI automation, Ghost-Driver incorporates the following best practices:

1. **Rule 1 (Form Safety)**: After typing into a form, the page state is marked as "Dirty". The AI will receive a safety warning prohibiting direct `navigate` calls on the current tab to prevent `BeforeUnload` blocking. Submit the form first or use `new_tab`.
2. **Rule 2 (API First)**: Using `browser_fetch` to interact directly with backend APIs via existing browser session cookies is recommended over filling out complex visual forms.
3. **Shadow DOM Penetration**: All selector tools (such as `smart_click`, `wait_for_selector`) and `get_markdown` recursively penetrate Shadow Roots.

## Compatibility & Troubleshooting

### Chrome 150+ & Windows IPv6
If you experience WebSocket connection timeouts or "operated on an invisible browser", Ghost-Driver defaults to `127.0.0.1` instead of `localhost`. This ensures physical IPv4/IPv6 alignment, resolving multi-process binding conflicts on Windows.

## Common Features

- **Environment Readiness Check (`check_system_ready`)**: Checks browser connection and runtime environment readiness.
- **Topological Map (`get_topology_map`)**: Records page transition graphs. The agent learns over time and avoids known dead ends.
- **Smart Click (`smart_click`)**: Describe what you want to click (e.g. "Login button"), and Ghost-Driver locates it automatically.
- **File Upload (`upload_files`)**: Handles native file selection dialogs (Chrome only). Invoke after triggering a file chooser to upload local files.

---
*Ghost-Driver: Atomic, deterministic, and AI-native browser automation.*
