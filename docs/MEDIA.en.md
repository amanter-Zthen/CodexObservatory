# 0.4.9-r1 demo production and upload

[中文](MEDIA.zh-CN.md) · [Home](../README.en.md)

The released demo renders the actual assembled page offline, frame by frame. Synthetic snapshots pass through `SnapshotFeed → app.step → Animator → six-layer WebGL`, with no real conversations or credentials recorded. Offline glass updates every frame, covering idle, green, yellow, and red states, and the final frame is hidden. The demo uses `gpt-6-astra`, replacing the previous mini example.

| Deliverable | Specification | File size | Location |
| --- | --- | --- | --- |
| Full video | 3840 × 2160, 60 fps, 37 seconds, 2220 frames; H.264 + AAC stereo | 20,733,919 bytes, about 19.8 MiB | [Release MP4](https://github.com/amanter-Zthen/CodexObservatory/releases/download/v0.4.9-r1/CodexObservatory-0.4.9-4K60.mp4) |
| README GIF | 800 × 450, nominally 8 fps, 296 frames, 37.01 seconds; silent | 6,706,297 bytes, about 6.4 MiB | [preview.gif](../media/preview.gif) |
| Poster and six isolated layers | Actually drawn by the current code | See repository files | [media](../media/) |

The soundtrack is a 37-second excerpt of **DJ烬冷冷、情冷冷 — Claude Remotion（浴室氛围版）**, supplied by the project author, with fades. Both streams of the final MP4 passed complete decode checks. The source repository excludes the original FLAC and album cover; see [acknowledgments](../ACKNOWLEDGMENTS.md) for music credits and license scope.

## Timeline and state

| Time | Scene |
| --- | --- |
| 0–6 seconds | Idle; entry begins at 0.5 seconds, showing the empty archive and glass first. |
| 6–12 seconds | One green model row, with output and moving numbers. |
| 12–18 seconds | Two model rows, including yellow pending verification. |
| 18–25 seconds | Four model rows, including a red anomaly and `gpt-6-astra`. |
| 25–28 seconds | Four rows enter a new turn, all green; the old turn's anomaly is not erased. |
| 28–32 seconds | Return to idle and fold the information. |
| 32–37 seconds | Full dissolution, ending hidden. |

State colors affect pane status markers, ink colors, and the corresponding archive. The main glass retains its warm optical material rather than tinting the entire array with the status color.

## Upload path

GitHub's official limits were checked on 2026-10-23: ordinary Git rejects files above 100 MiB, browser uploads to repositories are limited to 25 MiB per file, and comment video attachments have 10 MB / 100 MB plan-based limits. Each Release asset must be smaller than 2 GiB. The full video therefore goes to Release, while the source repository carries lightweight previews and code, without requiring Git LFS.

Sources: [repository file limits](https://docs.github.com/en/repositories/working-with-files/managing-large-files/about-large-files-on-github) · [attachment limits](https://docs.github.com/en/get-started/writing-on-github/working-with-advanced-formatting/attaching-files) · [Release limits](https://docs.github.com/en/repositories/releasing-projects-on-github/about-releases).

## Reproduce

Prepare Node / Playwright and a browser as described in the build guide. Supply your FFmpeg executable and a music file you have the right to use:

```powershell
node .\tools\render-public-demo.cjs --ffmpeg 'C:\Tools\FFmpeg\ffmpeg.exe' --music 'C:\Media\soundtrack.flac' --out '.\artifacts\demo'
```

Omit `--music` for a silent version. Use `--probe` first for representative frames and isolated layers. Rendering allows one frame in flight, uses four encoder CPU threads, and waits for GPU completion before reading a frame. The offline video's 60 fps is its delivery frame rate, not a promise of real-time performance on every machine.

Tools: [FFmpeg](https://ffmpeg.org/); this render used a Windows FFmpeg distribution from [imageio-ffmpeg](https://github.com/imageio/imageio-ffmpeg). Tool binaries are not distributed with the application.
