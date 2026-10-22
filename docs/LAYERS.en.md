# Six-layer project

[中文](LAYERS.zh-CN.md) · [Home](../README.en.md)

All six layers belong to one 0.4.9 project and share the timeline and data layout. Open `visual/index.html` and use the layer controls to toggle or isolate each group; isolation preserves motion state. Foreground-bloom isolation still consumes the other layers' emission inputs, but outputs the light layer rather than the full base color.

| Layer | Editing entry points | Main content |
| --- | --- | --- |
| 01 Glass background | `visual/background/engine/`, `visual/engine/glass_background.js` | Geometry, materials, optics, rising/falling, array, and camera |
| 02 Flowing ribbons | `engine/flow.js`, `ribbon_art.js`, `ribbons.js`, `shaders/scene_rib.js` | Long waves, branches, count tiers, and flow |
| 03 Floating particles | `engine/particles.js`, `shaders/post.js` | Bounded light points and fragments |
| 04 Frame guides | `engine/hud_motion.js`, `shaders/scene_hud.js` | Frame, identity bars, ticks, and expanding endpoints |
| 05 Information text | `engine/atlas.js`, `labels.js`, `row_presentation.js`, `shaders/text.js` | Character entry, model/rate layout, flowing light, and glyph atlas |
| 06 Foreground bloom | `engine/renderer.js`, `shaders/post.js`, `presentation.js` | Emission, multilevel bloom, warm entry light, and edge falloff |

Except for the first table entry, relative paths are under `visual/`. The six [`layers/`](../layers/) manifests list complete shared-file relationships and SHA-256 values; [`layers.lock.json`](../layers.lock.json) pins all 39 runtime scripts. Shared files have one maintained copy.

## Parameters and motion order

Primary parameters are in [`presentation.js`](../visual/engine/presentation.js), and glass curves are in [`appearance.js`](../visual/background/engine/appearance.js). The table records current values; a local parameter is not necessarily the total duration of the complete animation.

| Item | Current value or behavior |
| --- | --- |
| Actual render allowance | 128 DIP on each side |
| Edge feathering | Quintic falloff across 120 DIP outside the panel |
| Warm entry light | Original field peak retained; 0.42-second gathering; no independent exit warm light |
| Model row | Hold for 0.18 seconds after the full name appears, then expand |
| Expansion / collapse | 0.60 / 0.50 seconds; endpoints move from near the name toward both sides |
| Output start / pause hold | 0.12 / 0.80 seconds |
| Numbers | Bounded-tangent 0.65-second interpolation, one decimal place per presented frame |
| Output text lighting | Broad soft aura, primary/companion flows, and phase-offset shimmer reusing the glyph atlas |
| Empty camera | Automatically choose the empty archive view, at 1.08 close-up zoom; smoothly pull back for active models |
| Glass | Local rise of 1.22 seconds, 0.70-second depth-delay spread, and bounded settling motion |
| Ribbon count | Preserve long waves and strengthen visible growth at 2 and 4 sessions |

`animator.js`, `stage.js`, `row_presentation.js`, and the glass controller jointly enforce: complete model name → expand when needed → collapse after output stops → dissolve after collapse. A brief glass anticipation may overlap the end of collapse; actual falling waits for it. Continuous changes merge into the latest target, while active clips finish at bounded endpoints without an unbounded history queue.

## Editing and checking

Edit the single source and run the base visual, current visual, and engineering checks. Record the corresponding artistic change when updating the lock, then regenerate the six layer manifests. Matching hashes prove file identity; browser checks still establish pixels and motion order.

```powershell
npm test
npm run test:current
npm run test:engineering
```

The runtime uses a shared WebGL2 context, bounded texture/atlas resources, incremental glyph preparation, and one pending draw. Canvas fallback preserves information display but does not promise the full WebGL artwork. Static mode continues state updates while stopping decorative motion.
