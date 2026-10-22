# 接入你的 Codex

[English](INTEGRATION.en.md) · [项目首页](../README.md) · [构建](BUILD.zh-CN.md) · [六层视觉](LAYERS.zh-CN.md) · [API](API.zh-CN.md)

Codex Observatory 的显示与采集分别配置：你可以先独立预览画面，也可以连接已有兼容后端；首次启用实时采集则需要明确配置本地代理、证书和启动跟随。以下步骤由你手动执行，示例路径请替换为自己的位置。

## 1. 准备文件和目录

桌面目标为 Windows x64，显示使用系统安装的 WebView2 Runtime。源码构建见[构建说明](BUILD.zh-CN.md)。解压 Release 后保留以下布局，尤其保留 `desktop/` 与 `visual/` 的相对位置。

```text
CodexObservatory/
  CodexObservatory.exe
  CodexObservedLauncher.exe
  CodexRoutingBridge.exe
  display-host.example.json
  desktop/
    CodexObservatory.exe
  visual/
```

| 入口 | 用途 |
|---|---|
| 根目录 `CodexObservatory.exe` | Go 后端：状态、接口、托盘和原控制台。 |
| `CodexObservedLauncher.exe` | 采集与启动跟随，正常运行模式为 `--watch`。 |
| `CodexRoutingBridge.exe` | 旧 IPC/CLI 兼容桥；当前 native-home 采集路径不需要将 `CODEX_CLI_PATH` 改成它。 |
| `desktop/CodexObservatory.exe` | C++ 显示宿主：透明悬浮窗与 WebView2。 |
| `visual/` | 完整六层画面与动画源码。 |

另外选择三个目录：真实的 **Codex Home**、独立的 **后端 Data**、独立的 **DisplayState**。Data 必须位于被观察的 Codex Home 之外。下文以 `C:\Tools\CodexObservatory`、`C:\CodexObservatoryData`、`C:\CodexObservatoryState` 为例，不要求使用这些路径。

## 2. 先独立预览

浏览器打开 `visual/index.html`，或从 Release 根目录运行：

```powershell
.\desktop\CodexObservatory.exe --preview --state 'C:\CodexObservatoryPreview'
```

这是合成数据预览，不连接你的 Codex。显示 EXE 不带参数时显示未连接状态；`--fixture <snapshot.json>` 可读取本地测试快照而不访问网络。画面层次和控制项见[六层视觉](LAYERS.zh-CN.md)。

## 3. 选择新的显示宿主

Release 的 `display-host.example.json` 默认停用。先将它复制为根目录的 `display-host.json`；已有配置时编辑原文件。填写规范的 Windows 本地绝对路径和实际 EXE 哈希，再将 `enabled` 设为 `true`。

```powershell
(Get-FileHash -LiteralPath '.\desktop\CodexObservatory.exe' -Algorithm SHA256).Hash.ToLowerInvariant()
```

```json
{
  "enabled": true,
  "executable": "C:\\Tools\\CodexObservatory\\desktop\\CodexObservatory.exe",
  "state": "C:\\CodexObservatoryState",
  "sha256": "REPLACE_WITH_THE_64_HEX_DIGIT_DISPLAY_EXE_HASH"
}
```

配置只接受这四个字段。路径使用本地盘符绝对路径；EXE 与根目录后端不能是同一个文件。配置无效、哈希不符或宿主启动失败时，后端回退旧显示。每次更新显示 EXE 后重新计算并填写哈希。缺少有效选择器时，直接打开根目录后端不会自动切到新显示。

托管启动由后端传入 `--instance`、`--state`、`--owner-pid`。显示验证 owner 确为直接父进程；普通手动启动不需要添加该参数。

## 4. 连接已有兼容后端

如果已有支持 0.4.9 显示接口的后端，可跳过首次采集步骤，直接指定其 Data 目录中的 `instance.json`：

```powershell
.\desktop\CodexObservatory.exe --instance 'C:\CodexObservatoryData\instance.json' --state 'C:\CodexObservatoryState'
```

`instance.json` 由后端生成，含动态本地地址和启动令牌；保持私有，不手工复制其他人的实例文件。正常连接会读取当前状态，默认每五秒发送一次轻量 pulse；手动重同步和退出后端只由相应菜单动作触发。协议见 [API 文档](API.zh-CN.md)。

使用以下检查可验证只读快照到网页的接收确认。它创建隐藏检查窗口，完成后退出，不发送 pulse、resync 或 shutdown：

```powershell
.\desktop\CodexObservatory.exe --instance 'C:\CodexObservatoryData\instance.json' --state 'C:\CodexObservatoryCheck' --live-check 'C:\CodexObservatoryCheck\bridge.json'
```

检查通过证明读取与显示桥接成立，不代表已经验证了你的完整采集和上游连接。

## 5. 首次配置实时采集

### 5.1 核对连接影响

先核对 Codex Desktop EXE、Codex Home、现有代理和证书配置，并安排由你正常退出 Codex 后完成启用、再正常打开。已有代理、证书或其他采集器时先人工检查兼容关系；安装函数会拒绝直接覆盖不属于本程序受管块的网络键。

启用会在 Codex Home 的 `.env` 中维护 `HTTP_PROXY`、`HTTPS_PROXY`、`SSL_CERT_FILE` 和 `NO_PROXY`，保存本地 CA／代理身份和恢复资料，并注册当前用户的 Run 启动跟随项。这改变了 Codex 的信任与连接路径；它不是单纯的画面设置，也不会默认把局部 CA 导入系统全局根证书存储。

### 5.2 填写采集配置

在 Release 根目录、也就是 `CodexObservedLauncher.exe` 同目录创建 `bridge-config.json`。填写自己实际安装的 Desktop 和核心路径，以及自己的 Home 和 Data。

```json
{
  "native_codex": "C:\\Path\\To\\Codex\\codex.exe",
  "observatory_data": "C:\\CodexObservatoryData",
  "capture_mode": "local-proxy",
  "loopback_fixture": false,
  "desktop_app": "C:\\Path\\To\\Codex\\ChatGPT.exe",
  "monitor_exe": "C:\\Tools\\CodexObservatory\\CodexObservatory.exe",
  "bridge_exe": "C:\\Tools\\CodexObservatory\\CodexRoutingBridge.exe",
  "codex_home": "C:\\Users\\YOUR_NAME\\.codex"
}
```

`capture_mode` 使用 `local-proxy`，`loopback_fixture` 使用 `false`。`native_codex` 和 `bridge_exe` 保留旧桥兼容信息；当前 native-home 路径的关键字段是 Desktop、Home、Data 和 Monitor。Codex 应用包或核心路径更新后重新核对这些位置。

### 5.3 填写后端扫描根

创建独立的 Data 目录。在新的 `settings.json` 中填写下列配置；已有文件时保留其他设置，只调整相应字段，也可通过后端控制台保存。

```json
{
  "roots": ["C:\\Users\\YOUR_NAME\\.codex"],
  "poll_seconds": 3,
  "aliases": {}
}
```

这一项与 launcher 的 `codex_home` 分开：watcher 以 `--no-open --data <Data>` 启动后端，不会将 `--home` 转发为扫描根。明确保存 `roots` 才能让后端读取你选择的本地生命周期和额度记录。

### 5.4 启用、检查、正常打开

确认上述连接改动后，从 Release 根目录手动执行：

```powershell
.\CodexObservedLauncher.exe --enable-follow --data 'C:\CodexObservatoryData' --home 'C:\Users\YOUR_NAME\.codex'
```

它会配置采集身份和受管环境块，注册启动跟随并启动 watcher。首次安装使用 `--enable-follow`；`--watch` 仅用于身份已存在时的跟随运行。

随后执行只读健康检查：

```powershell
.\CodexObservedLauncher.exe --health-check --data 'C:\CodexObservatoryData' --home 'C:\Users\YOUR_NAME\.codex'
```

监听器就绪后，由你正常打开 Codex。启动顺序为：**watcher／采集监听器 → Codex 正常启动 → watcher 启动后端 → 后端读取有效选择器并启动显示 → 快照进入六层画面**。没有活动会话时零行待机是有效状态。

首次采集使用系统分配的 loopback 端口，保存到 Data 的 `native-capture/identity.json`，以后复用；后端 API 与元数据 feed 各有另外的动态本地端点。无需把采集端口固定为 10854，也不要把采集地址当成显示 API。

## 6. 日常操作与停用

右键可切换光效、重播、缩放、打开当前会话或控制台、启停自动 pulse、手动重同步和柔和隐藏。柔和隐藏先完成收场，再暂停绘图，后端继续运行；从原托盘恢复显示。新悬浮层不恢复完成历史列表。

“退出监视器”停止后端；watcher 仍存在且 Codex 仍运行时，可以按原跟随规则再次拉起后端。完整停用应先由你正常结束 Codex，再执行：

```powershell
.\CodexObservedLauncher.exe --disable-follow --data 'C:\CodexObservatoryData' --home 'C:\Users\YOUR_NAME\.codex'
```

停用移除本程序的受管 `.env` 块和匹配的用户 Run 项。正在运行的核心可能仍持有启动时代理配置，因此不要通过杀死 watcher 或直接删除 Data 代替停用；连接迁移完成前保留身份与恢复资料。

## 7. 数据与验证范围

新显示宿主仅保留当前快照、最新动画目标和有限绘制资源；DisplayState 保存窗口偏好及 WebView2 缓存，不保存会话正文、会话历史或完成收件箱。原 Go 后端仍读取本地 Codex 记录并保存索引、路由证据、完成状态和可选导出。保持 Data、真实配置、实例令牌、CA 私钥与 `.env` 备份私有。

采集会在内存解析受支持的请求／响应以提取元数据。显示的模型名称是请求与服务报告的标签，不是实际模型权重的独立认证。速度、颜色、时效和断线行为详见 [API 契约](API.zh-CN.md)。

上述流程和相关测试已在开发环境验证，尚未在另一台机器完成首次安装复验。
