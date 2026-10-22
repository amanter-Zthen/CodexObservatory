# 0.4.9-r1 演示制作与上传

[English](MEDIA.en.md) · [首页](../README.md)

发布演示由实际组装页面逐帧离线渲染，合成快照经过 `SnapshotFeed → app.step → Animator → 六层 WebGL`，没有录入真实会话或凭据。离线玻璃每帧更新，覆盖 idle、green、yellow、red 状态，最后一帧处于 hidden。演示模型使用 `gpt-6-astra`，已替换原 mini 示例。

| 交付 | 规格 | 文件大小 | 位置 |
| --- | --- | --- | --- |
| 完整视频 | 3840 × 2160、60 fps、37 秒、2220 帧；H.264 + AAC 立体声 | 20,733,919 字节，约 19.8 MiB | [Release MP4](https://github.com/amanter-Zthen/CodexObservatory/releases/download/v0.4.9-r1/CodexObservatory-0.4.9-4K60.mp4) |
| README 动图 | 800 × 450、名义 8 fps、296 帧、37.01 秒；无声音 | 6,706,297 字节，约 6.4 MiB | [preview.gif](../media/preview.gif) |
| 海报及六层独显 | 当前代码实际绘制 | 以仓库文件为准 | [media](../media/) |

配乐采用项目作者提供的 **DJ烬冷冷、情冷冷 — Claude Remotion（浴室氛围版）** 的 37 秒节选，带淡入淡出。最终 MP4 的视频和音轨已完整解码检查。原始 FLAC 与专辑封面不放进源码仓库；音乐署名和许可边界见 [致谢](../ACKNOWLEDGMENTS.md)。

## 时间线与状态

| 时间 | 场景 |
| --- | --- |
| 0–6 秒 | 空闲；0.5 秒触发入场，先展示空档案与玻璃。 |
| 6–12 秒 | 一行绿色模型，开始输出与数字运动。 |
| 12–18 秒 | 两行模型，包含黄色待核验状态。 |
| 18–25 秒 | 四行模型，包含红色异常与 `gpt-6-astra`。 |
| 25–28 秒 | 四行进入新回合，全绿；不是抹去旧回合异常。 |
| 28–32 秒 | 回到空闲，信息收拢。 |
| 32–37 秒 | 整体消散，结束于 hidden。 |

颜色联动体现在玻璃片状态标记、墨色和对应档案。玻璃主体仍保持暖色光学材质，不把整个阵列染成状态色。

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
