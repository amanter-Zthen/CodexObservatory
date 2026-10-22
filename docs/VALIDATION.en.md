# Public 0.4.9-r1 validation

[中文](VALIDATION.zh-CN.md) · [Home](../README.en.md)

This revision rebuilds the C++ display host and validates the input-window fix and changed visual path in a separate public-source directory on the Windows development machine. The Go backend and native build retain the same inputs and artifacts. Prior validation is listed separately below rather than counted as rerun. Publication work did not restart running production applications or change their networking, backend, or watcher.

## Revalidated for r1

| Check | Result |
| --- | --- |
| C++ host | Real compilation through the new public build entry PASS; MSVC x64, WebView2 SDK 1.0.2903.40, AMD64 GUI subsystem |
| CPU input geometry | 12 checks and 60 geometry cases PASS |
| Outer-margin and rounded-corner pass-through | Real cross-process left/right clicks, wheel input, and corner checks PASS at 216 DPI and 100% / 125% zoom |
| In-panel interaction | Dragging, menus, and hide/reopen PASS |
| Native-host lifecycle and fallback | 6 suites PASS |
| Base visual suite | 7 scripts rerun PASS for layout, choreography, glass, timing, identity text, ribbon tiers, and six-layer isolation |
| Current visuals | 15 checks rerun PASS for camera, text lighting, warm-up, one pending frame, and backpressure |
| Engineering invariants | 21 checks rerun PASS for message ordering, stale state, long-text atlases, bounded drawing, and fallback |
| Desktop page controls | 18 checks rerun PASS for transparency, zoom, hide/show, replay, scrolling, quality, and render allowance |
| Assembled-page glass state | 9 new checks PASS for the actual data-driving path and glass-state linkage |
| Stress run | 120 seconds PASS; 601 state inputs, at most one pending frame and one fence, zero playback long tasks |
| Build-script contracts | 17 synthetic checks rerun PASS; fake compilers test script boundaries and do not replace real compilation |
| Demo | 3840 × 2160 @ 60 fps, 37 seconds, 2220 frames, final state hidden; complete H.264 video and AAC audio decode PASS |

The r1 input implementation makes the large visual HWND pass input through and uses a separate small rounded input HWND covering only the panel. Outer-margin pass-through was confirmed by actual input received in a separate underlying process, rather than substituting hidden-window checks or geometry calculations for desktop interaction acceptance.

The demo uses the actual assembled-page `SnapshotFeed → app.step → Animator → six layers` path, updating offline glass on every frame. It covers idle, one green row, two rows including yellow, four rows including red and `gpt-6-astra`, a new all-green turn, folding, and dissolution. State linkage affects pane markers, ink colors, and the corresponding archive while retaining the main warm glass material.

## Retained prior validation

The following results cover the same Go/native inputs and artifacts from the previous public version. These complete checks were not rerun for this revision.

| Check | Retained result |
| --- | --- |
| Overlay Go build | PASS; 26 top-level cases, 77 pass events including subtests, 0 failures; native source tree unchanged before/after |
| Go/native verifier | 4 groups PASS: all-package build, floatui, route/state, and SSE/recovery |

## Artifacts and coverage limits

SHA-256 of the display executable rebuilt for this release:

```text
f1edec4f6fd2d54617877a236c6a173d8aab1fe44eaa17f1405304c0068af833
```

SHA-256 of the unchanged backend executable:

```text
e59d099e4d71ec9b43fadbc8f5976a89a9c70b0afa73ee1cd9db3b7179e3c9a9
```

The runtime package's `manifest.json` and `SHA256SUMS.txt` cover the actual released payload. Build reports remain in locally ignored directories. Reproduce your own checks with the [build commands](BUILD.en.md).

Coverage limits: outer-margin and rounded-corner pass-through were verified at the DPI and zoom levels above. Physical multi-monitor crossing, sustained use, and first-time capture installation on another machine have not yet received corresponding acceptance. The offline video's 60 fps is not a promise for all hardware. Capture connection effects and enable/disable steps are documented separately in [integration](INTEGRATION.en.md).
