---
name: ghost-driver
description: This skill should be used when the user needs browser automation driven by a local Chrome/Edge/Firefox through the **ghost-driver** MCP server — opening pages, reading or scraping page content, clicking, filling forms, logging into sites, reusing an existing session, taking screenshots, or automating web flows. It also applies when the user reports that browser tools are not working and wants them repaired. Prefer plain fetching (WebFetch/curl) when a URL can be retrieved without interaction.
version: <<VERSION>>
agent_created: true
---

# Ghost-Driver: Local Browser Automation

Drive a real Chrome/Edge/Firefox over CDP/BiDi (`http://localhost:9222`) through the **ghost-driver** MCP server. Version <<VERSION>>. Browser type is auto-detected — no configuration needed.

## 1. Resolve how the server is exposed — do not assume a client

Different hosting clients wire MCP up differently, and some (web chats) have no MCP at all. Determine the mode from **evidence at runtime** before doing anything else, in this order:

1. `connect_browser` is callable → the server is already connected. **Skip every setup step**, and never tell the user to edit a config file.
2. Only some ghost-driver tools are callable under a prefixed name → use those tools **under that exact prefix**.
3. Neither → the server is not connected in this client. Tell the user the concrete missing step (start the binary with `--transport stdio`, or with `--transport sse --port 8080` and expose the SSE endpoint) and **stop**. Never claim a browser session exists.

**Never hardcode a config filename** (`mcp_settings.json`, `claude_desktop_config.json`, `mcp.json`, …). Name the client's own path only when the user has already said which client they use.

The bundled binary ships at the package root as `<<BINARY>>`.

## 2. Connect

1. Call `connect_browser` with no arguments — it attaches to the browser already running on port 9222.
2. If it fails, call `check_system_ready` and read `port_9222_open` / `detected_browser_path` / `recommended_action`, then retry once.
3. If the port is closed, tell the user how to start the browser with the debug port, then retry. Do not fake success.

A dropped connection heals itself on the next call; retry once before doing anything else.

## 3. Core workflow

**Read a page** → `navigate(url)` then `get_markdown()`. Prefer `get_markdown()` over `screenshot()` for any text or structured-data extraction.

**Click / type** → `smart_click("natural language description")` for DOM + semantic + vision matching. For custom components (React/Vue) that appear inert, re-call with `post_verify_selector` to force the fallback dispatch. `type_text` types with human-like rhythm into the focused element.

**Dropdowns in SPAs** → hidden menu-based selects are the top failure mode. Call `enum_options(trigger)` to enumerate options first; never guess labels.

**Reuse the session instead of re-authenticating** → `browser_fetch(url)` hits site APIs with the browser's cookies, far faster and more reliable than UI login for read-only data. `capture_api_calls` reads network responses the page already made. `get_cookies` / `set_cookie` / `get_local_storage` / `set_local_storage` manage state.

**Async pages** → wait with `wait_for_text` / `wait_for_selector` / `wait_smart` after navigation and after clicks that trigger network work. Content is rarely present immediately.

## 4. Tool reference

Only list tools the running server actually exposes; if a name below is unavailable, it belongs to a pruned toolset.

| Group | Tools | When |
|---|---|---|
| Connection | `connect_browser`, `check_system_ready` | Always first |
| Navigation | `navigate`, `new_tab`, `switch_tab`, `list_tabs`, `close_tab`, `navigate_back`, `navigate_forward`, `reload` | Page/tab control |
| Read | `get_markdown`, `get_text`, `get_html`, `get_accessibility_tree`, `get_form_snapshot`, `screenshot`, `vision_analyze` | Extraction and diagnosis |
| Interaction | `smart_click`, `pixel_click`, `click`, `type_text`, `hover`, `scroll`, `upload_files` | Driving the UI |
| Sync | `wait_for_selector`, `wait_for_text`, `wait_smart` | Async waits |
| SPA | `enum_options`, `extract_virtual_list`, `discover_site_selectors`, `get_topology_map`, `scan_interactions` | Dynamic pages |
| Data | `browser_fetch`, `capture_api_calls`, `eval_js` | APIs and custom logic |
| State | `get_cookies`, `set_cookie`, `get_local_storage`, `set_local_storage`, `get_session_storage`, `set_session_storage` | Session and auth |
| Recording | `start_recording`, `stop_recording` | Watch the user demo a flow once, then replay it |
| Diagnostics | `debug`, `get_metadata`, `page_info`, `get_design_tokens`, `compare_visual_state`, `mark_dead_end`, `get_tool_analytics`, `save_site_profile` | When something is unclear |

## 5. Guardrails

1. **Data safety** — after `type_text` the page is "Dirty". Never `navigate` or `reload` in that same tab; Chrome blocks on unsaved-changes dialogs. Use `new_tab`.
2. **API over UI** — for structured data try `browser_fetch` or `capture_api_calls` before grinding through the UI.
3. **Attribution** — when an error appears ("Invalid input"), look for the `⚠️ CAUSALITY ALERT` block in the tool response; it names the responsible step. Do not blindly replay the whole flow.
4. **Verify before declaring done** — a successful `smart_click` does not mean state changed. Confirm with `get_markdown` or `compare_visual_state`.
5. **Escalate, don't loop** — read-only extraction goes through `get_markdown`; screenshots only when the DOM cannot see the content.
6. **External action** — this drives the user's live browser and can publish public content. Confirm before submitting real forms; keep dry runs in `new_tab` with dummy input.

## 6. Failures and limits

- `ready:false` from `check_system_ready` → run `connect_browser`; do not invent a browser launch.
- Infinite scroll → `extract_virtual_list` instead of looping scrolls.
- Canvas/WebGL pages (Figma, Excalidraw) → the DOM is one `<canvas>`; use `screenshot` + `vision_analyze`.
- Element not found repeatedly → `get_accessibility_tree` or `get_form_snapshot` to see what the agent actually sees.
- Tool list too large for a context-constrained run → see the toolset pruning table in the distribution package.

Site knowledge accumulates in `profiles/`; `save_site_profile` persists what worked so later runs skip discovery. Preserve that directory when upgrading the binary.
