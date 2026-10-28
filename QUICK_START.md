# Ghost-Driver Quick Start

## 1. Local Setup
1. Extract this ZIP to a folder.
2. Ensure **Google Chrome**, **Microsoft Edge**, or **Mozilla Firefox** is installed.

## 2. Connect your AI (MCP Mode)
- **Claude Desktop**: Add this to your `claude_desktop_config.json`:
  ```json
  "mcpServers": {
    "ghost-driver": {
      "command": "C:/path/to/ghost-driver/bin/ghost-driver.exe",
      "args": ["--transport", "stdio"]
    }
  }
  ```
- **Universal Chat (ChatGPT/Claude.ai)**:
  1. Run `bin/ghost-driver.exe --transport sse --port 8080`.
  2. Copy the content of `GHOST_DRIVER_SKILL.md` into your chat.

## 3. Command Line (CLI Mode)
Run directly from terminal for quick tasks:
```bash
# Take a screenshot of a site
./bin/ghost-driver --action navigate --url https://google.com --headless=false
./bin/ghost-driver --action screenshot --out google.png
```

## 4. Browser Auto-Detection
Ghost-Driver automatically detects whether Chrome (CDP) or Firefox (BiDi) is running on the debug port — no configuration needed.

```bash
# Chrome (CDP)
chrome.exe --remote-debugging-port=9222

# Firefox (WebDriver BiDi)
firefox.exe --remote-debugging-port=9222
```

In `managed` mode, Ghost-Driver auto-launches whichever browser it finds. Use `--browser-path` to specify a custom browser binary.

## 5. How to Upgrade (Keep your data!)
When a new version is released:
1.  Download the new ZIP.
2.  Replace only the `ghost-driver.exe` file in your existing folder.
3.  **DO NOT** delete your `memory/` or `profiles/` folders—these contain your saved logins and learned site knowledge.

## 6. Key Directories
- `profiles/`: Site-specific knowledge (Agent writes here automatically).
- `memory/`: (Generated at runtime) Stores auth tokens and navigation maps.
