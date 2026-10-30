[English](README_EN.md) | [中文](README.md)

# Ghost-Driver MCP

> 一个基于原生 CDP / WebDriver BiDi 的浏览器自动化 MCP Server，用 Go 编写。
> 接管已有浏览器会话，保留登录状态，支持录屏、VLM 视觉定位与虚拟列表提取。
> 自动检测并适配 Chrome (CDP) 与 Firefox (BiDi)，无需手动配置。

---

## 安装

### 下载预编译二进制（推荐）

从 [GitHub Releases](https://github.com/Daybeam/ghost-driver/releases) 下载对应平台的 zip 包，解压后即可使用：

```bash
./ghost-driver --version  # 验证安装 → ghost-driver v1.1.0
```

支持平台：Windows amd64、Linux amd64、macOS Intel、macOS Apple Silicon。

### 从源码编译

```bash
git clone https://github.com/Daybeam/ghost-driver.git
cd ghost-driver
go build -o ghost-driver ./cmd/ghost-driver
```

### 启动浏览器调试端口

```bash
# Chrome / Edge
chrome --remote-debugging-port=9222

# Firefox
firefox --remote-debugging-port=9222
```

也可以不手动启动——`connect_browser` 在 `managed` 模式下会自动查找并启动浏览器。

---

## 设计理念

大多数浏览器自动化工具假设浏览器是一个"干净容器"——它们启动新实例、注入驱动、然后关闭。
这在现实场景中行不通：

- 需要已登录的会话（Cookie、Token）
- 反爬机制检测自动化特征
- SPA 页面没有传统的 load 事件
- 虚拟列表只渲染可见行

Ghost-Driver 的做法是**接管你正在运行的浏览器**（Chrome 或 Firefox），而不是启动新的。

---

## 架构

```
LLM / Claude
  └── Ghost-Driver MCP（Go 二进制，stdio 或 SSE）
        ├── internal/cdp/
        │     ├── client.go      原始 CDP WebSocket，含 Host header 注入
        │     └── targets.go     /json/list 目标发现 + 浏览器类型探测
        ├── internal/bidi/
        │     ├── client.go      WebDriver BiDi WebSocket 客户端 (Firefox)
        │     └── adapter.go     CDP → BiDi 命令/事件/响应翻译层
        ├── internal/browser/
        │     ├── browser.go     导航 / 截图 / JS 执行 / 连接生命周期
        │     ├── input.go       双轨输入：CDP 平滑动画 OR xdotool (OS级)
        │     ├── indicator.go   视觉控制指示器（CDP 注入，无需插件）
        │     ├── recorder.go    帧序列 → FFmpeg GIF / MP4
        │     ├── dom.go         HTML→Markdown / 语义查找 / 文本提取
        │     ├── waiter.go      SPA 智能等待（MutationObserver + 网络静默）
        │     ├── virtual_list.go 虚拟列表滚动提取
        │     └── selector.go    自愈选择器（3层降级）
        ├── internal/vision/
        │     └── vlm.go         VLM (Gemini/OpenAI/Claude) → 像素坐标
        ├── internal/predict/
        │     └── predictor.go   链接预取 / 元素预测
        ├── internal/profile/
        │     └── profile.go     站点配置文件加载与热更新
        ├── internal/audit/
        │     └── audit.go       工具调用审计日志 (JSONL)
        ├── internal/config/
        │     └── config.go      环境变量与构建配置
        ├── internal/resources/
        │     └── resources.go   内嵌 JS 资源 (stealth, extraction, recorder)
        ├── internal/mcp/
        │     └── server.go      MCP tool 定义与分发
        └── cmd/ghost-driver/
              └── main.go        CLI 入口（支持 MCP 模式与单次执行 CLI 模式）
```

### 6. Memory & Long-term Navigation (记忆与长期导航)
Ghost-Driver 内置了基于 SQLite 的拓扑记忆系统：
- **持久化记忆**：所有探索过的状态（URL+Title）和动作自动存入 `./memory/topology.db`。
- **域名隔离**：支持按域名检索地图，避免不同站点间的逻辑干扰。
- **自愈引导**：Agent 会根据历史记录自动绕过曾导致死循环或错误的“死胡同”节点。

---

## Chrome 150+ 兼容性与稳定性增强

Ghost-Driver 已针对最新 Chrome 安全策略与长连接稳定性进行了深度优化：
- **零配置连接 (Zero-Config)**：`connect_browser` 默认连接 `127.0.0.1:9222`，并自动探测端口开放情况，智能切换 `Attached` 或 `Managed` 模式。
- **透明 CDP 代理**：自动识别容器环境（如 `host.docker.internal`），静默挂载内嵌反向代理，绕过 Chrome 的 Host header 校验。
- **自动重连卫士 (Auto-Reconnect Guard)**：在任何工具调用前自动执行连接健康检查。若 Chrome 意外重启或 WebSocket 断开，系统会自动静默重连并恢复之前的输入模式（l1/l2/l3）。
- **BeforeUnload 自动防御**：在导航前自动抹除页面 `onbeforeunload` 处理器，防止弹窗阻塞 CDP。

---

## 浏览器支持与自动检测

Ghost-Driver **自动检测**连接的浏览器类型，无需任何手动配置：

| 浏览器 | 协议 | 检测方式 | 支持状态 |
|:---|:---|:---|:---|
| **Chrome / Edge / Chromium** | CDP (Chrome DevTools Protocol) | `/json/version` 返回 200 | ✅ 完整支持 |
| **Firefox 157+** | WebDriver BiDi | `/json/version` 返回 404 | ✅ 核心功能支持 |
| **Electron / Tauri / Wails** | CDP (内嵌 Chromium) | 同 Chrome | ✅ 完整支持 |

### 工作原理

1. `connect_browser` 连接到调试端口后，向 `/json/version` 发送 HTTP 请求。
2. **200 响应** → 识别为 Chrome/CDP，使用原生 CDP WebSocket 通道。
3. **404 响应** → 识别为 Firefox/BiDi，自动切换到 WebDriver BiDi 适配层。
4. 所有后续工具调用透明地路由到对应的协议实现，上层 MCP 工具无感知。

### Firefox 启动方式

```bash
# Firefox 需使用 --remote-debugging-port 启动 BiDi 服务
firefox.exe --remote-debugging-port 9222 --headless
```

在 `managed` 模式下，Ghost-Driver 也会自动查找并启动 Firefox（无需指定路径）。

### Firefox (BiDi) 能力差异

由于 Firefox 使用 WebDriver BiDi 协议而非 CDP，以下能力存在差异：

| 能力 | Chrome (CDP) | Firefox (BiDi) | 影响 |
|:---|:---:|:---:|:---|
| 导航 / JS 执行 / 截图 | ✅ | ✅ | — |
| 点击 / 输入 / 滚动 / 悬停 | ✅ | ✅ | — |
| `get_content` | ✅ | ✅ | — |
| Cookie / Storage 管理 | ✅ | ✅ | — |
| 标签页管理 | ✅ | ✅ | — |
| `smart_click` / `wait_smart` | ✅ | ✅ | — |
| 对话框处理 (`handle_dialog`) | ✅ | ✅ | — |
| 设备模拟 / User-Agent 覆盖 | ✅ | ✅ | — |
| 录屏 (`start/stop_recording`) | ✅ | ✅ | 录屏基于 `Screenshot` 轮询，不依赖 `startScreencast` |
| 超视口全页截图 | ✅ | ✅ | BiDi `captureScreenshot` 支持 `origin:"document"` + `clip` |
| Accessibility Tree | ✅ | ✅ (polyfill) | Firefox 使用 JS 遍历 DOM + ARIA 属性模拟 AX 树 |
| 文件选择器拦截 (`upload_files`) | ✅ | ❌ | BiDi 无文件选择器协议；可用 `eval_js` 设置 `<input type=file>` |
| `Network.getResponseBody` | ✅ | ❌ | BiDi 无响应体获取；可用 `browser_fetch` 或 `eval_js` + `fetch()` |

> **注意**：Firefox 的反检测脚本 (`stealth_firefox.js`) 与 Chrome 的 (`stealth.js`) 不同，已自动适配。
> Firefox 的 Accessibility Tree 为 JS polyfill（基于 DOM + ARIA 属性），覆盖常见角色与状态，但不包含 CDP 的计算属性（如 `posinset`、`setsize`）。

---

## 运行模式

### 1. MCP 模式 (Standard)

用于集成到 Claude Desktop 或其他支持 MCP 协议的 IDE。

```bash
# stdio 模式
./ghost-driver --transport stdio

# SSE 模式
./ghost-driver --transport sse --port 8080
```

### 2. CLI 模式 (Non-interactive)

专为 LLM 或脚本化调用设计，执行完即退出。

```bash
# 导航到网页
./ghost-driver --action navigate --url "https://google.com"

# 获取页面文本
./ghost-driver --action get_text

# 截屏并保存
./ghost-driver --action screenshot --out "debug.png"

# 执行自定义 JS
./ghost-driver --action eval --js "document.title"
```

使用 `./ghost-driver --help` 查看完整参数。

---

## Docker（容器内连宿主机 Chrome）

```bash
docker run --rm -i -e ANTHROPIC_API_KEY=$ANTHROPIC_API_KEY ghost-driver --transport stdio
```

然后在 MCP 会话里连接宿主机浏览器，无需特殊配置：

```json
connect_browser { "devtools_http": "http://host.docker.internal:9222" }
```

**为什么不需要配置代理：** Ghost-Driver 会自动探测到非本地地址，并在底层透明挂载一个反向代理，将 `Host` 头改写为 `localhost`，绕过 Chrome 150+ 的安全限制。

---

## Claude Desktop 配置

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

## MCP 工具一览

### 连接管理

| 工具 | 参数 | 说明 |
|---|---|---|
| `connect_browser` | `devtools_http`, `mode`, `input_mode` | 连接浏览器。支持**零参数**启动。自动处理模式探测、远程代理与断线重连。 |
| `page_info` | — | 返回 URL、标题及 **Context Hints (SOP 建议)** |
| `check_system_ready` | — | 诊断环境就绪状态 |
| `list_tabs` | — | 列出所有打开的标签页 |
| `new_tab` | `url` | 打开新标签页 |
| `switch_tab` | `target_id`* | 切换当前活动标签页 |
| `close_tab` | `target_id` | 关闭指定标签页 |

### 导航

| 工具 | 参数 | 说明 |
|---|---|---|
| `navigate` | `url`* | 跳转并等待 load 事件 |
| `navigate_back` | — | 后退 |
| `navigate_forward` | — | 前进 |
| `reload` | — | 刷新 |
| `wait_smart` | `max_ms` | SPA 智能等待（网络 + DOM + 视觉三重检测） |
| `wait_for_selector` | `selector`*, `timeout_ms` | 等待 CSS 选择器出现（含 Shadow DOM） |
| `wait_for_text` | `text`*, `timeout_ms` | 等待文本出现在页面 |

### 交互

| 工具 | 参数 | 说明 |
|---|---|---|
| `click` | `x`*, `y`* | 平滑光标动画 + 点击坐标 |
| `hover` | `x`*, `y`* | 平滑光标动画（不点击） |
| `smart_click` | `description`*, `selectors` | 3 层自愈点击（CSS → DOM 穿透 → VLM） |
| `type_text` | `text`* | 模拟打字，并标记页面为 Dirty 状态 |
| `browser_fetch` | `url`*, `method`... | **[NEW]** 浏览器上下文 API 调用 (Rule 2) |
| `scroll` | `dy`*, `x`, `y`, `container_selector` | 滚动页面或指定容器 |

### 内容提取与状态

| 工具 | 参数 | 说明 |
|---|---|---|
| `get_content` | `format`, `selector`, `mode` | 获取页面内容（text/markdown/html，默认 markdown） |
| `get_topology_map` | `domain` | 获取状态转换地图 |
| `mark_dead_end` | — | 标记死胡同节点 |
| `get_accessibility_tree` | — | 提取 Accessibility 语义树 |
| `get_cookies` | — | 获取所有 Cookie |
| `set_cookie` | `name`*, `value`*, `domain`*... | 设置 Cookie |
| `read_storage` | `scope` | 读取 LocalStorage/SessionStorage（scope: local/session） |
| `write_storage` | `scope`, `key`*, `value`* | 写入 LocalStorage/SessionStorage |
| `remove_storage` | `scope`, `key`* | 删除 LocalStorage/SessionStorage 键 |
| `get_metadata` | — | 返回 title、URL、og: 标签、canonical |
| `extract_virtual_list` | `item_selector`*, `container_selector`, `max_items`, `scroll_step`, `settle_ms` | 滚动提取虚拟列表全量数据 |
| `eval_js` | `expression`* | 在页面上下文执行 JS |

### 截图与录制

| 工具 | 参数 | 说明 |
|---|---|---|
| `screenshot` | — | 截取当前视口，返回 base64 PNG |
| `start_recording` | `fps` | 开始录屏（默认 8fps） |
| `stop_recording` | `out_path`* | 停止并通过 FFmpeg 渲染为 `.gif` 或 `.mp4` |

### 视觉

| 工具 | 参数 | 说明 |
|---|---|---|
| `vision_analyze` | `prompt`*, `image` | VLM 视觉分析，返回元素坐标 |
| `pixel_click` | `x`*, `y`* | 直接点击像素坐标（视觉降级方案） |

### 站点配置

| 工具 | 参数 | 说明 |
|---|---|---|
| `discover_site_selectors` | — | 自动发现页面的输入框和按钮选择器 |
| `save_site_profile` | `name`*, `yaml_content`* | 保存站点配置到 `./profiles/*.yaml` |

### 工具合并 (`GHOST_LEGACY_TOOLS`)

以下工具已按「能力族」合并为统一入口：

| 旧工具 | 新工具 | 参数 |
|--------|--------|------|
| `get_text` / `get_markdown` / `get_html` | `get_content` | `format: text\|markdown\|html` |
| `get_local_storage` / `get_session_storage` | `read_storage` | `scope: local\|session` |
| `set_local_storage` / `set_session_storage` | `write_storage` | `scope, key, value` |
| `remove_local_storage` / `remove_session_storage` | `remove_storage` | `scope, key` |

设置 `GHOST_LEGACY_TOOLS=1` 可同时保留旧工具（向后兼容）。

---

## 输入模式

```
connect_browser { "devtools_http": "...", "input_mode": "cdp" }   // 默认
connect_browser { "devtools_http": "...", "input_mode": "os"  }   // xdotool
```

| 模式 | isTrusted | 适用场景 |
|---|---|---|
| `cdp`（默认） | `false` | 绝大多数网站、SPA、标准自动化 |
| `os`（xdotool） | `true` | 反爬检测、isTrusted 强校验（如 WeChat 编辑器） |

OS 模式需要 Linux + xdotool，且浏览器必须在同一个 X display 会话中运行。

---

## 视觉控制指示器

连接成功后，浏览器窗口会出现一个**蓝色发光边框**和左上角徽章，无需安装任何插件。
边框通过 CDP 注入一个 `pointer-events:none` 的 DOM 覆盖层实现，不影响任何点击操作。

| 状态 | 颜色 |
|---|---|
| 待机 | 🔵 蓝（`#00d4ff`，脉冲动画） |
| 移动光标 | 🟡 黄（`#ffcc00`） |
| 点击瞬间 | 🔴 红（`#ff4444`） |
| 打字中 | 🟣 紫（`#aa44ff`） |
| 录屏中 | 🔴 红（`#ff2222`，加速脉冲） |

---

## 虚拟列表提取示例

TradingView 的 Symbol 搜索列表、AG-Grid、React-Window 等虚拟化列表场景：

```json
extract_virtual_list {
  "container_selector": "[data-name='symbol-search-items-dialog']",
  "item_selector": "[data-name='list-item']",
  "max_items": 200,
  "scroll_step": 300,
  "settle_ms": 500
}
```

Ghost-Driver 会自动滚动、收集、去重，直到连续 3 次滚动没有新 item 为止。

---

## 智能等待原理

`wait_smart` 同时运行三个检测条件，全部满足才返回：

```
① 网络静默：XHR/fetch 飞行中数量 = 0，且最后一个请求结束超过 500ms
② DOM 安静：MutationObserver 超过 300ms 没有任何 DOM 变化
③ 视觉稳定：连续两帧截图 SHA-256 哈希相同
```

这三个条件通过 JS monkey-patch 注入页面实现，不依赖 CDP 的 `Page.loadEventFired`（该事件在 SPA 路由切换时不触发）。

---

## 高级特性与 AI 自配置

### 1. 站点配置文件 (Site Profiles)
Ghost-Driver 支持声明式站点配置，用于固化特定站点的先验知识（如选择器、网络过滤规则）。
- **路径**：`./profiles/*.yaml`
- **特性**：支持**热加载**，修改 YAML 后立即生效。

### 2. AI 助手自举 (Self-Bootstrapping)
当 Agent 遇到不支持的站点时，可以按照以下流程实现“自配置”：
1. **探索**：调用 `navigate` 到达目标站点。
2. **发现**：调用 `discover_site_selectors()` 获取输入框和按钮的建议选择器。
3. **确认**：结合 `get_accessibility_tree()` 和 `screenshot()` 确认最佳选择器。
4. **保存**：调用 `save_site_profile(name, yaml_content)` 永久保存该站点规则。

### 3. 高级脚本与插件支持
你可以直接调用本地 JavaScript 库（如 TradingView 自动化脚本）来执行复杂逻辑：
- `eval_js(expression)`: 注入并执行 JavaScript 表达式。
- **自动预加载**：在 `SiteProfile` 的 `preload_scripts` 中配置脚本路径，导航时会自动注入。

### 4. 通用视觉降级 (Universal Fallback)
所有视觉定位工具均支持“委派模式”：
- 如果 VLM (Gemini) 无法定位，工具会自动返回当前**页面截图**。
- Agent 可以根据图片通过 `pixel_click` 或辅助推理完成后续操作。



## Agent Skill 集成 (Agent Skill Integration)

Ghost-Driver 支持以 **Skill 插件** 形式被 AI Agent 平台（Claude Code / Hermes / OpenCode / Cursor 等）加载调用。

### 使用方式

`GHOST_DRIVER_SKILL.md` 是一份**参考技能文件**，文档化了工具目录和操作规则。
不同 Agent 平台的 Skill 格式、Prompt 风格和工作流约定各不相同，**直接复制通常不是最优选择**。
建议以此为起点，根据你的 Agent 的具体格式改写：

```bash
# 以参考文件为基础，改写为你的 Agent 所需的格式
cp GHOST_DRIVER_SKILL.md <your_agent_skills_dir>/ghost-driver/
# 然后根据你的 Agent 的 Skill schema 进行改写
```

### Skill 内容概览

`GHOST_DRIVER_SKILL.md` 包含：
- **Frontmatter**：name, description, version, authors
- **Quick Start**：连接 → 导航 → 提取 → 拓扑图查询
- **Tools 列表**：connect_browser, navigate, get_content, screenshot, smart_click, type_text, browser_fetch, get_topology_map, mark_dead_end
- **Operational Rules**：连接优先、拓扑感知、提取优先级、自愈能力

### 为什么选择 Skill 分发？

1. **零依赖**：不需要安装 Playwright / Selenium，纯 Go 二进制 + SKILL.md 即可运行
2. **拓扑记忆**：内置页面跳转图数据库，Agent 可查询浏览足迹和路径
3. **解压即用**：无需编译或配置环境变量

