// Bounded particle pool (MAX + AMB). Nothing accumulates: dead slots are reused.
// Motes ride the analytic ribbon field; shards/glitter come from transitions.
(function (NS) {
  'use strict';
  const { clamp01, hash1 } = NS.core;
  const MAX = 192, AMB = 18, TAU = Math.PI * 2, FLOATS = 12;

  const ribY = NS.Flow.ribbonY;

  class Particles {
    constructor() {
      this.p = Array.from({ length: MAX }, () => ({ age: 1, life: 0 }));
      this.cur = 0; this.inst = new Float32Array((MAX + AMB) * FLOATS); this.count = 0; this.acc = 0; this.accR = 0; this.accH = 0;
      this.emission=new Map();
    }
    spawn(x, y, vx, vy, size, kind, col, glow, life) {
      for (let n = 0; n < MAX; n++) {
        const q = this.p[(this.cur + n) % MAX];
        if (q.age < q.life) continue;
        this.cur = (this.cur + n + 1) % MAX;
        Object.assign(q, { x, y, vx, vy, size, kind, r: col[0], g: col[1], b: col[2], glow, life, age: 0, rot: Math.random() * 6.28, vr: (Math.random() - 0.5) * 6, seed: Math.random() });
        return;
      }
    }
    // emitters: [{x,y,w,h,rate,col}] from dissolving labels.
    update(dt, s, emitters, quality) {
      if(quality===3)return;
      const W = s.W, H = s.H, T = s.T, R = s.ribbons, rnd = Math.random;
      const lim = quality >= 2 ? 0.45 : quality >= 1 ? 0.75 : 1;
      const active=new Set();
      for (const e of emitters) {
        const key=e.id||[e.x,e.y,e.w].join(':');active.add(key);
        let remainder=(this.emission.get(key)||0)+e.rate*dt*lim;
        while (remainder >= 1) {
          remainder--;
          const gold = rnd() < 0.45, col = gold ? [1, 0.9, 0.72] : e.col;
          this.spawn(e.x + rnd() * e.w, e.y + rnd() * e.h, 10 + rnd() * 26, -6 - rnd() * 18, 0.45 + rnd() * 0.9, rnd() < 0.7 ? 1 : rnd() < 0.5 ? 0 : 2, col, gold ? 1.2 : 0.35, 0.8 + rnd() * 1.0);
        }
        this.emission.set(key,remainder);
      }
      for(const key of this.emission.keys())if(!active.has(key))this.emission.delete(key);
      // Shards along the slab's breaking frontier (matches slabVis sweep, mean terms).
      if (T.slabOut > 0.001 && T.slabOut < 0.999) {
        this.acc += dt * 150 * lim;
        for (let tries = 0; this.acc >= 1 && tries < 60; tries++) {
          const x = rnd() * W, y = rnd() * H, sw = 0.6 * (1 - x / W) + 0.22 * (y / H) + 0.21;
          if (Math.abs(sw - T.slabOut * 1.32) > 0.07) continue;
          this.acc--;
          const big = rnd() < 0.3;
          this.spawn(x, y, 14 + rnd() * 40, -10 - rnd() * 30, big ? 1.3 + rnd() * 1.4 : 0.5 + rnd() * 0.7, big ? 1 : rnd() < 0.6 ? 0 : 2, [0.99, 0.97, 0.93], 0.9, 0.9 + rnd() * 1.1);
        }
        this.acc = Math.min(this.acc, 4);
      }
      // Glitter shed by ribbon tails while they snake out, light at entering heads.
      for (let i = 0; i < (R.count || 0); i++) {
        const o = i * 4, head = R.C[o], tail = R.C[o + 1], op = R.C[o + 2], dir = R.B[o + 2];
        if (op <= 0) continue;
        const tx = (u) => (dir > 0 ? u : 1 - u) * W;
        if (tail > -0.3 && tail < 1.15) {
          this.accR += dt * 34 * lim;
          while (this.accR >= 1) { this.accR--; const u = tail + 0.06 + rnd() * 0.08, x = tx(u); this.spawn(x, ribY(R, i, x, W) + (rnd() - 0.5) * R.B[o] * 1.6, dir * (6 + rnd() * 14), -4 - rnd() * 10, 0.4 + rnd() * 0.7, rnd() < 0.6 ? 0 : 2, [1, 0.94, 0.82], 1.3, 0.7 + rnd() * 0.8); }
        }
        if (head > -0.05 && head < 1.05 && T.enter < 1) {
          this.accH += dt * 18 * lim;
          while (this.accH >= 1) { this.accH--; const x = tx(head - rnd() * 0.04); this.spawn(x, ribY(R, i, x, W), dir * (4 + rnd() * 8), -3 - rnd() * 6, 0.5 + rnd() * 0.5, 2, [1, 0.93, 0.8], 1.6, 0.5 + rnd() * 0.5); }
        }
      }
      let n = 0; const f = this.inst;
      for (const q of this.p) {
        if (q.age >= q.life) continue;
        q.age += dt; q.vx *= 1 - dt * 0.7; q.vy *= 1 - dt * 0.5; q.vy -= dt * 3;
        q.vx += Math.sin(q.age * 3 + q.seed * 20) * dt * 9;
        q.x += q.vx * dt; q.y += q.vy * dt; q.rot += q.vr * dt;
        const k = q.age / q.life, a = Math.min(1, q.age / 0.08) * Math.pow(1 - clamp01(k), 1.4);
        f.set([q.x, q.y, q.size * (1 - 0.4 * k), q.rot, q.r, q.g, q.b, a, q.kind, q.glow, q.seed, 1], n * FLOATS); n++;
      }
      // Ambient motes drifting on the hero currents (analytic, no state).
      const show = clamp01(T.slabIn * 1.2) * (1 - T.slabOut), amb = quality >= 2 ? 8 : AMB;
      if (show > 0.01 && R.count) {
        for (let j = 0; j < amb; j++) {
          const rib = j % NS.ribbonArt.P.coreCount, sp = 0.035 + 0.03 * hash1(j * 3.3) + 0.03 * s.energy, u = (hash1(j * 7.1) + s.t * sp) % 1;
          const dir = R.B[rib * 4 + 2], x = (dir > 0 ? u : 1 - u) * (W + 40) - 20;
          const off = (hash1(j * 5.7) - 0.5) * 34 + 6 * Math.sin(s.t * 0.6 + j);
          const tw = 0.4 + 0.6 * Math.pow(0.5 + 0.5 * Math.sin(s.t * (1.3 + hash1(j)) + j * 2.1), 3);
          const a = show * Math.sin(Math.PI * u) * tw * (0.55 + 0.45 * s.energy);
          f.set([x, ribY(R, rib, x, W) + off, 0.55 + 0.6 * hash1(j * 2.9), 0, 1, 0.93, 0.8, a, j % 5 === 0 ? 2 : 0, 1.1, hash1(j), 1], n * FLOATS); n++;
        }
      }
      this.count = n;
    }
  }
  NS.Particles = Particles; NS.PARTICLE_FLOATS = FLOATS; NS.PARTICLE_MAX = MAX + AMB;
})(window.RA);
