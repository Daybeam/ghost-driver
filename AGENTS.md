# AGENTS.md

> Guide for AI agents using ghost-driver.

## What is ghost-driver

A browser automation MCP server (single Go binary) that attaches to your
already-running Chrome or Firefox via CDP / WebDriver BiDi. It preserves
login state (cookies, tokens), supports recording, VLM visual targeting,
and virtual-list extraction.

## Getting the binary

**Option A — Download prebuilt** (recommended):

Grab the zip for your platform from
<https://github.com/Daybeam/ghost-driver/releases>, unzip, and you have
the `ghost-driver` (or `ghost-driver.exe`) binary ready to run.

**Option B — Build from source**:

```bash
git clone https://github.com/Daybeam/ghost-driver.git
cd ghost-driver
go build -o ghost-driver ./cmd/ghost-driver
```

Verify: `./ghost-driver --version` → `ghost-driver v1.1.0`

## Starting the server

```bash
# MCP stdio mode (for Claude Desktop, Cursor, etc.)
./ghost-driver --transport stdio

# MCP SSE mode (for web-based clients)
./ghost-driver --transport sse --port 8080

# One-shot CLI mode (script / single action)
./ghost-driver --action navigate --url "https://example.com"
./ghost-driver --action screenshot --out page.png
```

## Browser setup

Start Chrome (or Edge) with a debug port:

```bash
chrome --remote-debugging-port=9222
```

Or call `connect_browser` with no arguments — in `managed` mode
ghost-driver auto-launches whichever browser it finds.

Firefox: `firefox --remote-debugging-port=9222` (uses WebDriver BiDi,
auto-detected).

## Connecting & basic workflow

1. `connect_browser` — attaches to the browser on port 9222 (zero args OK).
2. `navigate` — go to a URL.
3. `get_content` — read the page (format: text / markdown / html).
4. `screenshot` — capture the viewport as base64 PNG.
5. `smart_click` — click by natural-language description (3-layer fallback:
   CSS → DOM traversal → VLM).
6. `type_text` — type into the focused element.
7. `wait_smart` — SPA-aware wait (network silence + DOM stable + visual stable).

## Key tools (59 total across 11 toolsets)

| Category | Tools |
|----------|-------|
| Connection | `connect_browser`, `page_info`, `check_system_ready`, `list_tabs`, `new_tab`, `switch_tab`, `close_tab` |
| Navigation | `navigate`, `navigate_back`, `navigate_forward`, `reload`, `wait_smart`, `wait_for_selector`, `wait_for_text` |
| Interaction | `click`, `hover`, `smart_click`, `type_text`, `scroll`, `browser_fetch` |
| Content | `get_content`, `get_metadata`, `eval_js`, `extract_virtual_list` |
| State | `get_cookies`, `set_cookie`, `read_storage`, `write_storage`, `remove_storage` |
| Capture | `screenshot`, `start_recording`, `stop_recording` |
| Memory | `get_topology_map`, `mark_dead_end` |
| Vision | `vision_analyze`, `pixel_click` |
| Profiles | `discover_site_selectors`, `save_site_profile` |

Default toolset is `base` (excludes `dev` inspection tools).
Set `GHOST_LEGACY_TOOLS=1` to also expose pre-merge individual tools.

## SKILL.md — adapt, don't copy

`GHOST_DRIVER_SKILL.md` is a **reference skill file** that documents the
tool catalog and operational rules. Treat it as a starting point:
**rewrite and adapt it** to match your agent's skill format, prompt style,
and workflow conventions. Different agent platforms have different skill
schemas — a verbatim copy will rarely be optimal.

## Environment variables

| Variable | Purpose | Default |
|----------|---------|---------|
| `GEMINI_API_KEY` / `OPENAI_API_KEY` / `ANTHROPIC_API_KEY` | VLM provider key for visual targeting | — |
| `GHOST_STEALTH` | Enable anti-detection scripts | `false` |
| `GHOST_LEGACY_TOOLS` | Expose old individual tools alongside consolidated ones | unset |
| `GHOST_AUDIT_LOG` | Path for JSONL audit log of tool calls | `./memory/audit.jsonl` |
| `GHOST_INPUT_MODE` | Input mode: `cdp` (default) or `os` (xdotool, Linux only) | `cdp` |

## Behavioral notes

- **Session takeover**: ghost-driver attaches to your existing browser,
  preserving logins. It does not start a fresh isolated instance.
- **Auto-reconnect**: if the browser crashes or restarts, the next tool
  call silently reconnects and resumes.
- **Auto-detect**: Chrome/Edge → CDP, Firefox → BiDi. No manual config.
- **Dirty flag**: after `type_text`, the page is marked dirty. Navigation
  is blocked until you submit or open a new tab (prevents BeforeUnload
  dialogs from hanging CDP).
- **Topology memory**: explored URLs and transitions are stored in
  `./memory/topology.db` (SQLite). Query with `get_topology_map` to
  avoid revisiting dead-end pages.
