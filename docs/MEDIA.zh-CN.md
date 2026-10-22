# 演示制作与上传

[English](MEDIA.en.md) · [首页](../README.md)

发布演示由当前六层 WebGL 代码逐帧离线渲染，使用合成状态，没有录入真实会话或凭据。完整流程为无模型入场、模型启动、1 → 2 → 4 个模型、收拢回待机、整窗消散。最后一帧已确认处于 hidden 状态。

| 交付 | 规格 | 文件大小 | 位置 |
| --- | --- | --- | --- |
| 完整视频 | 3840 × 2160、60 fps、36 秒、2160 帧；H.264 + AAC 立体声 | 19,046,727 字节，约 18.2 MiB | [Release MP4](https://github.com/amanter-Zthen/CodexObservatory/releases/download/v0.4.9/CodexObservatory-0.4.9-4K60.mp4) |
| README 动图 | 800 × 450、8 fps、完整 36 秒；无声音 | 6,380,321 字节，约 6.1 MiB | [preview.gif](../media/preview.gif) |
| 海报及六层独显 | 当前代码实际绘制 | 以仓库文件为准 | [media](../media/) |

配乐采用项目作者提供的 **DJ烬冷冷、情冷冷 — Claude Remotion（浴室氛围版）** 的 36 秒节选，带淡入淡出。FLAC 已成功解码，最终 MP4 的视频和音轨也已完整解码检查。原始 FLAC 与专辑封面不放进源码仓库；音乐署名和许可边界见 [致谢](../ACKNOWLEDGMENTS.md)。

## 上传路径

2026-10-23 核对 GitHub 官方限制：普通 Git 单文件超过 100 MiB 会被阻止，浏览器上传仓库单文件上限 25 MiB；评论区视频附件按套餐有 10 MB / 100 MB 限制。Release 每个附件须小于 2 GiB。因此完整视频放 Release，源码仓库仅放轻量预览和源代码，不需要 Git LFS。

来源：[仓库文件限制](https://docs.github.com/en/repositories/working-with-files/managing-large-files/about-large-files-on-github) · [附件限制](https://docs.github.com/en/get-started/writing-on-github/working-with-advanced-formatting/attaching-files) · [Release 限制](https://docs.github.com/en/repositories/releasing-projects-on-github/about-releases)。

## 复现

先按构建文档准备 Node / Playwright 和浏览器。使用自己的 FFmpeg 可执行文件及有权使用的音乐文件：

```powershell
node .\tools\render-public-demo.cjs --ffmpeg 'C:\Tools\FFmpeg\ffmpeg.exe' --music 'C:\Media\soundtrack.flac' --out '.\artifacts\demo'
```

省略 `--music` 可生成无声版本；`--probe` 先生成代表帧和六层独显。渲染限制为一个在途帧，编码使用四个 CPU 线程，并等待 GPU 完成后才取帧。离线输出的 60 fps 是交付视频帧率，不代表任意硬件实时运行都达到该帧率。

工具来源：[FFmpeg](https://ffmpeg.org/)；本次使用 [imageio-ffmpeg](https://github.com/imageio/imageio-ffmpeg) 的 Windows 发行包取得 FFmpeg。工具二进制不随应用分发。
