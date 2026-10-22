// Panel lifecycle. Keeps the host contract of the Go Animator:
//   beginReveal / beginExit / showImmediate / hideImmediate
//   flags entering / exiting / hidden / transitioning
// Two monotonic progress clocks (enter, exit) instead of one tween, so an exit
// interrupted by a reveal rewinds the SAME dissolve instead of teleporting.
(function (NS) {
  'use strict';
  const { clamp01, win, smoother, easeOutExpo } = NS.core;

  const ENTER_DUR = 2.5;   // boot from empty space
  const EXIT_DUR = 2.0;    // snake-out + shatter
  const REWIND = 1.7;      // exit rewinds faster than it plays

  class Stage {
    constructor() { this.pe = 1; this.px = 0; this.dir = 1; this.hidden = false; }
    beginReveal() {
      if (this.dir > 0 && !this.hidden) return;
      if (this.hidden) { this.pe = 0; this.px = 0; }
      this.hidden = false; this.dir = 1;
    }
    beginExit() {
      if (this.hidden || this.dir < 0) return;
      this.dir = -1;
    }
    showImmediate() { this.pe = 1; this.px = 0; this.dir = 1; this.hidden = false; }
    hideImmediate() { this.pe = 0; this.px = 1; this.dir = -1; this.hidden = true; }
    update(dt) {
      if (this.hidden) return;
      if (this.dir > 0) {
        if (this.px > 0) this.px = Math.max(0, this.px - dt * REWIND / EXIT_DUR);
        else this.pe = Math.min(1, this.pe + dt / ENTER_DUR);
      } else {
        this.px = Math.min(1, this.px + dt / EXIT_DUR);
        if (this.px >= 1) this.hidden = true;
      }
    }
    get entering() { return !this.hidden && this.dir > 0 && (this.pe < 1 || this.px > 0); }
    get exiting() { return !this.hidden && this.dir < 0; }
    get transitioning() { return this.entering || this.exiting; }

    // Every element gets its own window, curve and direction of travel.
    // `in` = assembled amount, `out` = dissolved amount. Renderer uses both.
    timeline() {
      const e = this.pe, x = this.px;
      const T = {
        glowIn: win(e, 0.0, 0.28, easeOutExpo), glowPulse: Math.sin(Math.PI * clamp01(e / 0.62)) * (e < 0.62 ? 1 : 0),
        slabIn: win(e, 0.16, 0.56), frameIn: win(e, 0.30, 0.72), ringIn: win(e, 0.36, 0.84),
        headIn: win(e, 0.44, 0.74), quotaIn: win(e, 0.50, 0.84), detailIn: win(e, 0.70, 1.0),
        rowsIn: clamp01((e - 0.56) / 0.40),
        textOut: win(x, 0.02, 0.42), quotaOut: win(x, 0.10, 0.48), frameOut: win(x, 0.14, 0.58),
        ringOut: win(x, 0.12, 0.56), slabOut: win(x, 0.0, 0.92, (v) => v), glowOut: win(x, 0.62, 1.0),
        rowsOut: clamp01(x / 0.42),
        snake: Math.sin(Math.PI * clamp01((x - 0.08) / 0.8)),
        energy: Math.max(Math.sin(Math.PI * clamp01(e)) * (e < 1 ? 1 : 0), Math.sin(Math.PI * clamp01(x))),
        enter: e, exit: x, hidden: this.hidden,
      };
      return T;
    }
    // Ribbon i head/tail in travel units. Heads weave in from alternating sides
    // (direction lives on the ribbon); on exit the tail chases the head along
    // the same animated path so a finite segment swims out and is gone.
    ribbonWindow(i, T) {
      const st = 0.035 * (i % 6);
      const inR = smoother(clamp01((T.enter - 0.03 - st) / 0.58));
      const outR = smoother(clamp01((T.exit - 0.06 - st * 0.8) / 0.74));
      return { head: -0.35 + 1.75 * inR, tail: -0.40 + 1.80 * outR };
    }
  }
  NS.Stage = Stage;
  NS.STAGE = { ENTER_DUR, EXIT_DUR };
})(window.RA);
