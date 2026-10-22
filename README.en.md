# Codex Observatory

**Turn every generation into a performance of light.**

Make monitoring desktop art, and put routing state in plain sight. Six layers of glass, ribbons, particles, and animated text turn a working Codex session into a luminous observatory—beautiful to watch, with evidence to read.

[中文 — matching content](README.md) · [Download 0.4.9](https://github.com/amanter-Zthen/CodexObservatory/releases/tag/v0.4.9) · [Full 4K / 60 fps demo](https://github.com/amanter-Zthen/CodexObservatory/releases/download/v0.4.9/CodexObservatory-0.4.9-4K60.mp4)

[![Codex Observatory six-layer visual demo](media/preview.gif)](https://github.com/amanter-Zthen/CodexObservatory/releases/download/v0.4.9/CodexObservatory-0.4.9-4K60.mp4)

*The GIF is a lightweight preview. The full video is 3840 × 2160, 60 fps, and 36 seconds long, with music. It uses synthetic data and shows the complete sequence: empty entry → model start → parallel states → folding → dissolution.*

## Beautiful, and legible

- **Make routing state visible.** Compare requested and response-reported model labels. Green, yellow, and red distinguish a match, pending verification, and a label anomaly observed in the current turn. A later match does not erase an anomaly from that turn.
- **Give state changes a visual rhythm.** Glass rises, the camera pulls back, model names arrive character by character, and guides expand sideways. Light and numbers move during output; information folds before dissolving.
- **All six layers are open.** Glass background, flowing ribbons, floating particles, frame guides, information text, and foreground bloom each have source, a solo preview, and editing entry points.
- **Float beside your work.** A native transparent Windows host reuses system WebView2, with topmost mode, dragging, zoom, quality settings, current-state menus, and routing synchronization.
- **Near real time, with smoothness first.** Latest-state coalescing, bounded caches, warm-up, single-frame GPU backpressure, and smoothed rate estimates prioritize complete animation and responsiveness over millisecond chasing. Numbers retain the 0.4.8 0.65-second interpolation cadence.
- **A lightweight five-second trigger.** Automatic pulse wakes the existing incremental reader without building a catch-up queue. The floating layer has no conversation history; the original backend retains local evidence and record features.

Model-label comparison provides routing clues; it cannot independently authenticate the model weights a service actually runs. Output speed is a near-real-time estimate. See the [API and state contract](docs/API.en.md).

## See the scene first

After downloading the source, open [`visual/index.html`](visual/index.html) in a WebGL2-capable browser. The controls select model count, status colors, output, quota, camera, quality, and each layer in isolation. This synthetic preview needs neither a backend nor Node.

For the [Windows x64 release](https://github.com/amanter-Zthen/CodexObservatory/releases/tag/v0.4.9), extract it and run:

```powershell
.\desktop\CodexObservatory.exe --preview
```

The executable inside `desktop/` is the transparent display; the executable with the same name at the package root is the Go backend. The display requires system WebView2 Runtime. See [Connect your own Codex](docs/INTEGRATION.en.md) for live capture; preview does not enable capture for you.

## The six-layer project

| Layer | Visual role | Source and preview |
| --- | --- | --- |
| 01 · Glass background | Array, rising/falling, optical material, empty and active camera views | [Manifest](layers/01-background.json) · [Preview](media/layer-background.png) |
| 02 · Flowing ribbons | Long waves and branches, with stronger count changes at 2 / 4 sessions | [Manifest](layers/02-curves.json) · [Preview](media/layer-curves.png) |
| 03 · Floating particles | Light points, fragments, and bounded particles | [Manifest](layers/03-particles.json) · [Preview](media/layer-particles.png) |
| 04 · Frame guides | Frame, endpoints, ticks, and identity bars | [Manifest](layers/04-frame.json) · [Preview](media/layer-frame.png) |
| 05 · Information text | Model names, estimated output rates, quota, per-character motion, and flowing light | [Manifest](layers/05-info.json) · [Preview](media/layer-info.png) |
| 06 · Foreground bloom | Warm entry light, emission composition, and wide soft falloff | [Manifest](layers/06-glow.json) · [Preview](media/layer-glow.png) |

The layers share one timeline, layout, and compositor. Manifests point to the actual source files so six duplicate projects do not drift apart. SHA-256 values for the current 39 runtime scripts are pinned in [`layers.lock.json`](layers.lock.json). See [Layers, parameters, and animation order](docs/LAYERS.en.md).

## Interfaces, building, and verification

| What you want to do | Documentation |
| --- | --- |
| Connect your Codex and configure Home, capture, and display | [Integration](docs/INTEGRATION.en.md) |
| Understand snapshots, route colors, output, freshness, and synchronization | [API and state contract](docs/API.en.md) |
| Build the backend, companions, and transparent host | [Build and package](docs/BUILD.en.md) |
| Adjust the six visual layers and motion order | [Six-layer project](docs/LAYERS.en.md) |
| Review verification coverage for this public version | [Validation](docs/VALIDATION.en.md) |
| Reproduce the high-resolution demo and understand upload size | [Demo production](docs/MEDIA.en.md) |

```text
native/              0.4.8-nacre source, tests, and vendored dependencies
deployment/overlay/  0.4.9 backend overlay and state interfaces
desktop/             C++ / WebView2 transparent floating host
visual/              The single current six-layer visual project
layers/              Per-layer source and hash manifests
interfaces/          Snapshot field structure
tests/               Synthetic scenarios and regression checks
tools/               Portable building, packaging, and offline rendering
docs/                Matching Chinese and English documentation
```

Building `native/` alone produces the old display baseline. Use the overlay build together with the `desktop/` host for 0.4.9. The backend API retains its `0.4.8-nacre` version identifier. Full capture requires explicit local proxy, certificate, and follow configuration for Codex; installation is documented separately from visual preview.

## Thanks and sources

Special thanks to **路北路陈 (LBEILC)** for [RhineLabUI](https://github.com/LBEILC/RhineLabUI). Part of this project's visual inspiration comes from that work, and its public source directly helped with parts of the layout and motion mechanisms. We retain the adaptation credits and MIT license in the relevant source. The referenced revision is pinned to [`ee5779741c6c0c916e416705fa634c7abf905c73`](https://github.com/LBEILC/RhineLabUI/tree/ee5779741c6c0c916e416705fa634c7abf905c73).

We also thank the visual language of the Arknights special film “Rhine Life: Visit” (「莱茵生命：访问」). Codex Observatory is an independent, unofficial tool. See the [bilingual acknowledgments](ACKNOWLEDGMENTS.md) and [third-party notices](THIRD_PARTY_NOTICES.md).

Demo music: **DJ烬冷冷、情冷冷 — Claude Remotion（浴室氛围版）**. Project code is [MIT licensed](LICENSE). Third-party code, music, names, and brands retain their respective rights; the music is not relicensed under the project's code license.
