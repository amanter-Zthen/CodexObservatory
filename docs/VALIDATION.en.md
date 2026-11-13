# Public 0.4.9-r3 validation

[中文](VALIDATION.zh-CN.md) · [Home](../README.en.md)

The r3 revision fixes the subagent lifecycle; local targeted results and public-source rebuilds are recorded separately. The r2, r1, and earlier results below remain historical validation and are not counted as r3 reruns.

## r3 subagent lifecycle validation · 2026-11-13

- The old-version negative control reproduces inherited metadata overwriting session identity and completed subagent rows being left behind.
- The locally fixed Go backend and floatui suites pass 172 top-level checks and 231 including subtests, covering nested inheritance, parent/child isolation, later turns, cancellation, late responses, tool waits, and old-cache migration.
- Read-only replay of 18 real subagent logs leaves zero completed rows behind; two completed residual rows also disappear in the production API check. The public repository includes neither those logs nor their path list.
- All 47 visual runtime files match. The six-layer visuals, flow-light and speed algorithms, display executable, capture watcher, and network protocol were unchanged, with no new continuous polling or history.
- A probe of the same two sessions against the pinned [codex-statusbar commit `17f0ddd`](https://github.com/dongyue-19/codex-statusbar/tree/17f0ddd25512b9a823b56a37da325d36ca53971f) returned idle and completed, respectively, without reproducing continuous running; this conclusion is limited to that commit and those two sessions.
- The public-source rebuild passes 171 top-level checks and 230 including subtests; real-log replay is skipped by default. Four additional collector trust-bundle targeted checks pass, and the `native/` originals remain unchanged. See [build instructions](BUILD.en.md) for optional replay.

## r2 targeted validation · 2026-10-31

- Collector trust bundle: four top-level checks pass, including four Windows EKU subcases, repeated initialization, signing-identity preservation and failed-write recovery boundaries.
- Hybrid speed: 11 checks pass; the original writing-number interpolation has zero comparison error and row churn retains bounded memory.
- Normal-token light: 14 checks pass. Actual WebGL model-name and number pixels change with the effect, with zero new textures, GL errors or page errors.
- Public build entry: the retained display overlay passes 26 top-level checks and 77 including subtests, plus the four collector checks above; retained native source files remain unchanged.
- Independent Windows/Python ROOT enumeration produces the same 57 public TLS roots as the bundle that restored a real connection. This count belongs to the verification computer and is not hard-coded.
- At the r2 validation, the new watcher required its next normal start to load. File installation and live activation are recorded separately. Public artifacts include no author-machine certificates, identities, configuration or raw logs.

The r1 records below are retained historical validation. The r2 release did not rerun the full long stress, cross-process mouse or video-render suites.

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

The following results are historical validation retained from the r1 / r2 public releases; they do not represent an r3 backend rebuild.

| Check | Retained result |
| --- | --- |
| Overlay Go build | PASS; 26 top-level cases, 77 pass events including subtests, 0 failures; native source tree unchanged before/after |
| Go/native verifier | 4 groups PASS: all-package build, floatui, route/state, and SSE/recovery |

## Artifacts and coverage limits

SHA-256 of the display executable validated for r1 (historical artifact):

```text
f1edec4f6fd2d54617877a236c6a173d8aab1fe44eaa17f1405304c0068af833
```

SHA-256 of the backend executable recorded for r2 (historical artifact):

```text
e59d099e4d71ec9b43fadbc8f5976a89a9c70b0afa73ee1cd9db3b7179e3c9a9
```

The public rebuild and locally deployed r3 fixed backend executable have the same SHA-256:

```text
8a509a66ada88009830432e78ccf578ca48a4331154b9d0c3a94ac91a1e14a13
```

Use each artifact's manifest for public packages and other rebuilt binaries.

The runtime package's `manifest.json` and `SHA256SUMS.txt` cover the actual released payload. Build reports remain in locally ignored directories. Reproduce your own checks with the [build commands](BUILD.en.md).

Coverage limits: r1 verified outer-margin and rounded-corner pass-through at the DPI and zoom levels above. Physical multi-monitor crossing, sustained use, and first-time capture installation on another machine have not yet received corresponding acceptance. The offline video's 60 fps is not a promise for all hardware. Capture connection effects and enable/disable steps are documented separately in [integration](INTEGRATION.en.md).
