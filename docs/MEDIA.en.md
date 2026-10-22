# Demo production and upload

[中文](MEDIA.zh-CN.md) · [Home](../README.en.md)

The released demo is rendered offline, frame by frame, by the current six-layer WebGL code with synthetic state. It records no real conversations or credentials. The sequence covers empty entry, model start, 1 → 2 → 4 models, folding back to standby, and full-window dissolution. The final frame was confirmed hidden.

| Deliverable | Specification | File size | Location |
| --- | --- | --- | --- |
| Full video | 3840 × 2160, 60 fps, 36 seconds, 2160 frames; H.264 + AAC stereo | 19,046,727 bytes, about 18.2 MiB | [Release MP4](https://github.com/amanter-Zthen/CodexObservatory/releases/download/v0.4.9/CodexObservatory-0.4.9-4K60.mp4) |
| README GIF | 800 × 450, 8 fps, complete 36 seconds; silent | 6,380,321 bytes, about 6.1 MiB | [preview.gif](../media/preview.gif) |
| Poster and six isolated layers | Actually drawn by the current code | See repository files | [media](../media/) |

The soundtrack is a 36-second excerpt of **DJ烬冷冷、情冷冷 — Claude Remotion（浴室氛围版）**, supplied by the project author, with fades. The FLAC decoded successfully, and both streams of the final MP4 passed complete decode checks. The source repository excludes the original FLAC and album cover; see [acknowledgments](../ACKNOWLEDGMENTS.md) for music credits and license scope.

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
