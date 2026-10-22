# 来源与许可说明

本交付是独立 Go 实现，不是任何上游项目的官方发行版，也不是 OpenAI 产品。
没有捆绑、下载或安装第三方 Codex 可执行文件；主动检查调用使用者已有的官方 native Codex。
没有捆绑字体文件、第三方 JavaScript 框架或外部网络资源。界面使用操作系统字体。

参考的公开接口/实现思路（具体上游状态以其仓库为准）：
- OpenAI Codex protocol： https://github.com/openai/codex/blob/main/codex-rs/protocol/src/protocol.rs
- OpenAI Codex CLI/app-server： https://github.com/openai/codex
- darkdarkcocoa/codex-routing-detector v1.6.1： https://github.com/darkdarkcocoa/codex-routing-detector
  接收侧 tungstenite trace、native exec 和请求/响应范围区分的实现思路。
- harveyxiacn/codex-usage-monitor： https://github.com/harveyxiacn/codex-usage-monitor
  rollout 事件类型和 usage 字段的交叉参考。
- ysh1112/codex-model-watch： https://github.com/ysh1112/codex-model-watch
  历史与仪表盘需求参考；本版没有使用其直连 auth/HTTP 探针或 SQLite 代码。
- kiyoakii/is-gpt-nerfed 和 xqy2006/ModelTrace：行为指纹边界参考；本版未捆绑或实现。
- AndreaZero/codex-usage：Windows 监视界面需求参考；本版未捆绑 Rust/Tauri 组件。

本交付应用源码采用随附 MIT License。编译后二进制包含 Go 标准库/运行时；其许可见 licenses/Go-LICENSE.txt。
这些参考链接不是当前 exe 已被上游审核、签名或背书的声明。

0.2.0 新增直接依赖：github.com/klauspost/compress v1.20.1（BSD 3-Clause），用于只读解码 gzip/zstd/deflate 观测副本，传输原始字节保持原样。完整许可证见 licenses/klauspost-compress-LICENSE.txt。模块源码：https://github.com/klauspost/compress 。上游历史构建记载工具链为 Go 1.27.1；不代表本次构建。本轮随附预览候选使用 Go 1.23.2，详见根构建报告。


Nacre UI 新增参考：LBEILC/RhineLabUI 固定提交 ee5779741c6c0c916e416705fa634c7abf905c73（共享场、转场和材质分层思路）。新增 native/Painter/material 实现为本项目代码；没有捆绑其 Three.js 运行时、PV 素材、字体或标识。

0.4.0 新增参考（2026-09-30 查阅；方法参考，不是运行依赖）：
- https://github.com/LBEILC/RhineLabUI/blob/main/src/motion.ts
  非固定整图位移、连续相位与错峰收敛的运动组织；本项目自行实现曲线。
- https://github.com/pixijs/pixijs/blob/dev/src/scene/particle-container/shared/ParticleContainer.ts
- https://pixijs.com/8.x/guides/components/scene-objects/particle-container
  静态/动态属性分离与轻量粒子设计；未引入 PixiJS。
- https://learn.microsoft.com/en-us/windows/win32/direct2d/improving-direct2d-performance
  资源复用、图集和 A8 字形缓存方法；本版是 CPU 合成，不声称 Direct2D 加速。
- https://developer.nvidia.com/gpugems/gpugems/part-i-natural-effects/chapter-1-effective-water-simulation-physical-models
  几何低频波与表面细节分层；没有实现完整流体模拟。
可选实时代码校验 HTML 包含 Go/WASM 运行时（BSD 许可见 licenses/Go-LICENSE.txt），
不包含第三方 JS 框架，不作为 Windows 桌面程序的运行依赖。不捆绑任何字体文件。


0.4.1 光效参考（2026-10-01 核查；只借鉴工程原则，没有复制上游 shader）：
- https://github.com/mrdoob/three.js/blob/master/examples/jsm/postprocessing/UnrealBloomPass.js
  多尺度辉光、低分辨率模糊与组合的源码参考。本版不是 Three.js 移植。
- https://github.com/pmndrs/postprocessing/blob/main/src/effects/SelectiveBloomEffect.js
  仅选定发光物参与 bloom 的组织方法；没有采用其深度选取或 WebGL 管线。
- https://github.com/pixijs-userland/particle-emitter/blob/master/src/Emitter.ts
  有上限的粒子生命周期/复用思路。本版用固定容量绘制列表和解析式年龄函数，不分配递归子发射器。
本次 light.go、片晶纹理和环形刻度都是本项目自行编写。没有新增运行依赖；
没有提取 PV 素材、概念图像素、字体文件或上游三维模型。概念图仅为审美参照，
当前是二维 CPU 近似，不是体积光、真实折射、全 HDR 显示或物理破碎模拟。
以上检索的是移动分支网页，未声称本轮取得了它们的固定 commit 快照。
