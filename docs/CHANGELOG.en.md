# Version history

[中文](CHANGELOG.zh-CN.md)

## 0.4.9-r3 · 2026-11-13

- Fix subagent log ownership: retain the session identity first established for a file, so inherited parent metadata no longer overwrites it and file completion events and network responses belong to the same subagent.
- At startup, compare old caches with the source log's first identity and event ownership; reparse only misattributed caches within the existing 4 MiB incremental chunks and scan budget.
- Preserve the distinction between a single completed response and a completed turn, along with existing semantics for tool waits, later turns, and late frames; add no continuous polling or history.
- Build the fix through the Go overlay while retaining the frozen `native/` originals; the six-layer visuals, flow-light and speed algorithms, display executable, capture watcher, and network protocol remain unchanged.

The release identifier is 0.4.9-r3, the six-layer project remains 0.4.9, the display host is shell.1, and the native source and compatible API baseline remain 0.4.8-nacre. See [validation](VALIDATION.en.md) for local targeted results and public rebuild coverage; the demo continues to show the r1 visual baseline.

## 0.4.9-r2 · 2026-10-31

- Fix the capture launcher's generated TLS trust bundle: preserve both the original observer CA and Windows public TLS roots, preventing the next initialization from breaking public-service certificate verification again.
- Include hybrid speed display: visible text retains the original larger writing estimate; gaps use the latest valid token rate from the current turn. Running rows have decorative variation bounded at about ±11%; tool waits hold the reading. Both sources use the original 0.65-second interpolation.
- Use four normal-token light bands at 20 / 50 / 90 tok/s for the existing model-name, number and /s flow effect, at .22 / .34 / .48 / .62 of full writing light. Sampled anchors select bands; decorative number motion does not change them.
- Preserve the six layers, current-state memory, original full writing effect and static mode. No new GPU textures, render passes, samplers or conversation history.
- Add matching Chinese/English trust, version, build, integration and API documentation. The demo remains the r1 six-layer baseline and is not relabeled as an r2 recording.

The release identifier is 0.4.9-r2, the six-layer project remains 0.4.9, and the retained native source and compatible API identify as 0.4.8-nacre. These describe the release, visual project and interface baseline respectively. Build/archive SHA-256 manifests identify actual binaries. See VERSION.json at the repository root.

Existing processes do not automatically reload updated files. If the Windows launcher file is updated while the previous watcher remains running, report that activation awaits a normal watcher start rather than claiming the new code is active.

## 0.4.9-r1 · 2026-10-23

Fix mouse pass-through outside the wide glow while retaining visible rounded-panel dragging and menus. Drive glass state from the assembled snapshots and rebuild the consistent 4K60 demo. Historical assets remain in their matching release.
