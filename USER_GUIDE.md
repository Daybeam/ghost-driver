[English](USER_GUIDE_EN.md) | [中文](USER_GUIDE.md)

# Ghost-Driver 用户指南

Ghost-Driver 是一个浏览器自动化工具，它可以接管你正在运行的浏览器（Chrome 或 Firefox），保留你的登录状态（Cookie）并避开复杂的反爬检测。

## 快速开始

### 1. 安装浏览器
确保你的电脑上安装了 **Google Chrome**、**Microsoft Edge** 或 **Mozilla Firefox**。Ghost-Driver 会自动尝试寻找它们的路径。

### 2. 启动方式

#### 方式 A：接管已打开的浏览器（推荐）
如果你希望保留已登录的网站状态（如 GitHub, Gmail）：
1. 完全关闭所有浏览器窗口。
2. 在终端/命令行中输入以下命令启动浏览器（带调试端口）：
   - **Chrome (Windows)**: `chrome.exe --remote-debugging-port=9222 --user-data-dir="C:\temp\ghost-profile"`
   - **Chrome (macOS)**: `/Applications/Google\ Chrome.app/Contents/MacOS/Google\ Chrome --remote-debugging-port=9222 --user-data-dir="/tmp/ghost-profile"`
   - **Firefox**: `firefox.exe --remote-debugging-port=9222`
3. 运行 Ghost-Driver，它会自动连接到 `127.0.0.1:9222` 并识别浏览器类型。

#### 方式 B：自动模式
如果你不需要保留特定登录状态，Ghost-Driver 会在连接失败时尝试**自动启动**一个新的浏览器窗口。你只需直接运行 Ghost-Driver 并在工具中调用 `connect_browser` 即可。Ghost-Driver 会自动检测系统上安装的 Chrome 或 Firefox 并启动。

#### 方式 C：CLI 非交互执行
专为 LLM 调用或快速脚本设计，直接通过命令行执行指令并获取结果：
- `ghost-driver --action navigate --url "https://google.com"`
- `ghost-driver --action screenshot --out "capture.png"`
- `ghost-driver --action get_text`

#### 方式 D：SSE 远程连接模式 (高兼容)
适用于 ChatGPT、Claude.ai 网页版等无法直接运行本地进程的场景：
1. 在终端运行：`ghost-driver --transport sse --port 8080`。
2. 告知 AI 助手连接到 `http://localhost:8080/sse`。
3. 系统将通过持久化的 HTTP 流维持控制权，即使聊天会话重置，浏览器状态也能保留。

## 浏览器自动检测

Ghost-Driver **无需手动指定浏览器类型**，连接时自动检测：
- **Chrome / Edge / Electron**：通过 CDP (Chrome DevTools Protocol) 通信。
- **Firefox 157+**：通过 WebDriver BiDi 协议通信，内置 CDP→BiDi 翻译层。

所有 MCP 工具对上层透明，无论底层使用哪种浏览器，调用方式完全一致。

### Firefox 能力说明
Firefox (BiDi) 支持所有核心操作：导航、截图、点击、输入、JS 执行、内容提取、Cookie/Storage 管理、标签页管理。以下能力因协议限制不可用：
- `upload_files`（文件选择器拦截）
- `get_accessibility_tree`（完整无障碍树）
- `start_recording` / `stop_recording`（录屏）
- 超视口全页截图（Firefox 截图限于当前视口）

## 核心操作规则 (The Playbook)

为确保 AI 高效稳定地运行，Ghost-Driver 内置了以下自动化最佳实践：

1. **Rule 1 (表单安全)**: 在表单中输入文字后，系统会标记页面为 "Dirty"。AI 将收到警告提示：严禁在当前标签页导航（避免 BeforeUnload 阻塞），必须先提交或使用 `new_tab`。
2. **Rule 2 (API 优先)**: 推荐使用 `browser_fetch` 工具直接调用站点 API，利用浏览器已有的登录态，避开复杂的表单填充。
3. **Shadow DOM 穿透**: 所有的选择器工具（如 `smart_click`, `wait_for_selector`）和 `get_markdown` 现在都支持递归穿透 Shadow Root。

## 兼容性说明

### Chrome 150+ & Windows IPv6
如果你遇到 WebSocket 连接超时或"操作了不可见浏览器"，Ghost-Driver 现在的默认连接地址已从 `localhost` 改为 `127.0.0.1`。这确保了物理层面的 IPv4/IPv6 协议栈对齐，彻底解决了 Windows 系统下的多进程冲突问题。

## 常用功能

- **环境就绪检查 (`check_system_ready`)**: 检查浏览器连接与运行环境是否就绪。
- **持久化地图 (`get_topology_map`)**: 记录所有站点的路径拓扑，Agent 跑得越多越"聪明"，自动避开死胡同。
- **智能点击 (`smart_click`)**: 只需描述你想点击的内容（如 "登录按钮"），AI 会自动帮你定位。
- **文件上传 (`upload_files`)**: 支持处理原生的文件选择对话框（仅 Chrome）。在点击上传按钮触发对话框后，AI 可调用此工具提供本地文件路径完成上传。

---
*Ghost-Driver: 让浏览器自动化变得原子化、确定性、更懂 AI。*
