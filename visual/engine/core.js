// Shared math for the Rhine-style renderer. Pure functions + a damped spring.
// Everything here maps 1:1 to Go (no closures over DOM, no allocation per frame).
(function (NS) {
  'use strict';
  const clamp = (x, a, b) => (x < a ? a : x > b ? b : x);
  const clamp01 = (x) => (x <= 0 ? 0 : x >= 1 ? 1 : x);
  const lerp = (a, b, t) => a + (b - a) * t;
  const smooth = (e0, e1, x) => { const t = clamp01((x - e0) / (e1 - e0)); return t * t * (3 - 2 * t); };
  const smoother = (x) => { x = clamp01(x); return x * x * x * (x * (x * 6 - 15) + 10); };
  const easeOutExpo = (x) => (x >= 1 ? 1 : 1 - Math.pow(2, -10 * clamp01(x)));
  const easeInOutSine = (x) => 0.5 - 0.5 * Math.cos(Math.PI * clamp01(x));
  // Progress of a sub-window [a,b] inside a 0..1 timeline, eased.
  const win = (p, a, b, ease) => (ease || smoother)((p - a) / (b - a));

  // Cheap deterministic hash, identical formula usable in GLSL/Go.
  const hash1 = (n) => { const s = Math.sin(n * 127.1 + 311.7) * 43758.5453; return s - Math.floor(s); };
  const strHash = (s) => { let h = 2166136261 >>> 0; for (let i = 0; i < s.length; i++) { h ^= s.charCodeAt(i); h = Math.imul(h, 16777619) >>> 0; } return (h % 100000) / 100000; };

  // Critically damped (or slightly under-damped) spring. Re-targeting keeps
  // position AND velocity, so bursty snapshots never restart an animation.
  class Spring {
    constructor(value, freq, damping) {
      this.x = value; this.v = 0; this.target = value;
      this.freq = freq || 2.2;      // Hz-ish; per property so nothing moves in lockstep
      this.damping = damping == null ? 1 : damping;
    }
    set(t) { this.target = t; return this; }
    snap(t) { this.x = this.target = t; this.v = 0; return this; }
    step(dt) {
      if (dt <= 0) return this.x;
      const w = 2 * Math.PI * this.freq, z = this.damping;
      // Sub-step for stability on long frames (tab switch, slow GPU).
      const n = Math.min(8, Math.max(1, Math.ceil(dt / 0.016)));
      const h = dt / n;
      for (let i = 0; i < n; i++) {
        const a = -2 * z * w * this.v - w * w * (this.x - this.target);
        this.v += a * h; this.x += this.v * h;
      }
      return this.x;
    }
    get settled() { return Math.abs(this.x - this.target) < 1e-3 && Math.abs(this.v) < 1e-3; }
  }

  // Fast-attack / slow-release envelope (RhineLabUI archive-play-motion idea):
  // output energy rises quickly when tokens flow and keeps a long soft tail.
  class Envelope {
    constructor(rise, fall) { this.v = 0; this.rise = rise || 12; this.fall = fall || 3.2; }
    step(target, dt) {
      const rate = target > this.v ? this.rise : this.fall;
      this.v += (target - this.v) * (1 - Math.exp(-dt * rate));
      return this.v;
    }
  }

  NS.core = { clamp, clamp01, lerp, smooth, smoother, easeOutExpo, easeInOutSine, win, hash1, strHash, Spring, Envelope };
})(window.RA = window.RA || {});
