# 构建与打包

[English](BUILD.en.md) · [首页](../README.md) · [接入](INTEGRATION.zh-CN.md)

桌面交付目标是 Windows x64。构建和测试在自己的 checkout 内生成文件；这些命令不安装采集、不修改 Codex 连接，也不启动正式监视器。

## 1. 依赖

| 组件 | 要求 |
| --- | --- |
| Go | 1.25 或以上；本次使用 1.25.14；模块依赖已包含在 `native/vendor/` |
| Node.js | 20 或以上；用于覆盖层构建、打包与测试脚本 |
| MSVC + Windows SDK | C++17、x64 工具链；本次使用 Visual Studio 2019 Build Tools |
| WebView2 SDK | 固定 `Microsoft.Web.WebView2` **1.0.2903.40** |
| WebView2 Runtime | 显示运行时由系统提供，不随包复制整个浏览器 |
| 浏览器测试 | `npm ci` 安装锁定的 Playwright；使用本机 Chrome/Edge，或 `npx playwright install chromium` |

从 [Microsoft 官方 NuGet 包](https://www.nuget.org/packages/Microsoft.Web.WebView2/1.0.2903.40) 下载 SDK 并解压；`-Sdk` 目录中应有 `Microsoft.Web.WebView2.nuspec` 和 `build/native/`。脚本会核对版本、头文件与静态 loader。构建工具不自动下载 SDK。

## 2. 构建完整后端

从仓库根目录执行。Go 在 PATH 时无需设置第一行；也可以使用自己的编译器完整路径。

```powershell
$env:OBSERVATORY_GO = 'C:\Tools\Go\bin\go.exe'
node .\deployment\build-overlay.cjs
```

脚本核对 `deployment/native-source-manifest.json`，将 0.4.9 修改投影到 Go overlay，在隔离夹具中验证，再生成后端、watcher 和兼容桥。`native/` 原文件在构建前后保持一致。依赖使用 vendor，编译时关闭模块联网下载。`OBSERVATORY_GO_CACHE` 可选择构建缓存位置；省略时使用 Go 的正常缓存。

输出为 `deployment/build/CodexObservatory.exe`、`CodexObservedLauncher.exe`、`CodexRoutingBridge.exe`，报告为 `deployment/build-report.json`。单独 `go build native` 不包含新显示接口；它用于保留的旧基线。

## 3. 构建透明显示

```powershell
.\tools\build-desktop.ps1 -Sdk 'C:\Tools\WebView2-1.0.2903.40' -VcVars 'C:\Path\To\VisualStudio\VC\Auxiliary\Build\vcvars64.bat'
```

也可用 `OBSERVATORY_WEBVIEW2_SDK` 和 `OBSERVATORY_VCVARS`，或脚本支持的常见 VS / NuGet 位置发现。路径允许普通 Unicode 字符；包含 `%`、`!`、双引号或换行的构建路径不受支持。输出为 `desktop/CodexObservatory.exe`，报告为 `desktop/build/build-report.json`。链接静态 WebView2 loader；系统 Runtime 仍是运行前提。

## 4. 检查

```powershell
npm ci
npm test
npm run test:current
npm run test:engineering
npm run test:controls
node .\tools\check-publication.cjs
.\tools\verify-native.ps1 -GoExe 'C:\Tools\Go\bin\go.exe'
```

`OBSERVATORY_CHROME` 可指向浏览器 EXE，`OBSERVATORY_PLAYWRIGHT` 可指向已有 Playwright 包。长压力检查另用 `npm run test:stress`。这些测试使用构造状态和隔离本地服务；具体覆盖与限制见 [验证记录](VALIDATION.zh-CN.md)。结果写到忽略的 `verification/` 或 `tests/` 输出文件。

## 5. 生成运行包

```powershell
node .\tools\package-release.cjs --check
node .\tools\package-release.cjs
Compress-Archive -LiteralPath '.\dist\CodexObservatory' -DestinationPath '.\dist\CodexObservatory-0.4.9-windows-x64.zip'
```

打包读取本次构建报告和实际文件哈希，输出 `dist/CodexObservatory/`；已有输出不会被覆盖。需要另一个干净输出时使用 `--out dist/NAME`。包内包含四个运行 EXE、当前视觉源、双语指南、图层预览和许可证，以及默认停用的显示配置示例。配置自己的实例见 [接入指南](INTEGRATION.zh-CN.md)。

源码中的旧原生 README 和默认目录候选保留基线语义；公开接入使用你显式填写的 `roots` / `codex_home`。本仓库不包含生产状态、凭据、CA 身份或作者机器的部署记录。
