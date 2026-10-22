# Public 0.4.9 validation

[中文](VALIDATION.zh-CN.md) · [Home](../README.en.md)

Validation ran on the Windows development machine on 2026-10-23 using a separate public-source directory. It contained no production instance, credentials, or historical validation material; builds and tests generated fresh results. The publication task did not replace or restart the running production Codex, monitor, watcher, or network configuration.

| Check | Result |
| --- | --- |
| Source identity | 231 native files, 39 visual runtime scripts, HTML, and 6 layer manifests match their pinned hashes |
| Node dependencies | Playwright 1.62.1 pinned; `npm ci` succeeded |
| Overlay Go build | PASS; 26 top-level cases, 77 pass events including subtests, 0 failures; native source tree unchanged before/after |
| Native verifier | 4 groups passed: all-package build, floatui, route/state, and SSE/recovery |
| C++ host | Real MSVC x64 compilation passed with WebView2 SDK 1.0.2903.40; AMD64 GUI subsystem |
| Actual new EXE bridge | Isolated hidden window, synthetic snapshots, system WebView2; `--verify` returned PASS / exit 0 |
| Base visual suite | 7 scripts passed, covering layout, choreography, glass, timing, identity text, ribbon tiers, and six-layer isolation |
| Current visuals | 15 checks passed for camera, text lighting, warm-up, one pending frame, and backpressure |
| Engineering invariants | 21 checks passed for message ordering, stale state, long-text atlases, bounded drawing, and fallback |
| Desktop page controls | 18 checks passed for transparency, zoom, hide/show, replay, scrolling, quality, and render allowance |
| Build-script contracts | 17 synthetic checks passed; fake compilers test script boundaries and do not replace the real compilations above |
| Demo | 4K60, 36 seconds, 2160 frames, final state hidden; full H.264 video and AAC audio decode passed |

Clean-directory validation exposed and fixed two old test dependencies: the removed `pose.rate.snap()` preparation call and pre-existing report directories. Number checks now use the real `snapshot → set → frame → Labels` path, covering the 0.65-second endpoint, continuous retargeting, tangent limits, and left/right anchors. Visual runtime files were not changed to make the tests pass.

SHA-256 of the display executable rebuilt for this release:

```text
c6882f6e3409175e3a8c937dc19ff43ad2d92708dd5eda1a1cb02677896aeeef
```

SHA-256 of the backend executable:

```text
e59d099e4d71ec9b43fadbc8f5976a89a9c70b0afa73ee1cd9db3b7179e3c9a9
```

The runtime package's `manifest.json` and `SHA256SUMS.txt` cover the actual released payload. Build reports remain in locally ignored directories. Reproduce your own checks with the [build commands](BUILD.en.md).

Coverage limits: a hidden isolated-host check does not establish first-time capture installation on another machine. Outer-margin click-through, manual multi-monitor behavior, and sustained use require validation in the corresponding environment. The offline video's 60 fps is not a promise for all hardware. Capture connection effects and enable/disable steps are documented separately in [integration](INTEGRATION.en.md).
