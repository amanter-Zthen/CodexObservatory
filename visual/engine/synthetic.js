// Synthetic snapshots only. Field names follow internal/floatui/project.go JSON.
// Nothing here is real routing evidence or account quota.
(function (NS) {
  'use strict';
  const MODELS = ['gpt-6.1-sol', 'gpt-6-sol', 'gpt-6.1-sol', 'gpt-6.1-sol-mini'];
  const MIXES = {
    mixed: ['green', 'yellow', 'red', 'green', 'green', 'yellow'],
    green: ['green'], yellow: ['yellow'], red: ['red', 'green'],
  };
  function synth(t, o) {
    const rows = [], colors = MIXES[o.mix] || MIXES.mixed;
    for (let i = 0; i < o.count; i++) {
      const color = colors[i % colors.length];
      // Each session has its own output rhythm: bursts, pauses, tool waits.
      const cyc = (t * (0.09 + 0.013 * i) + i * 0.37) % 1;
      let outputting = o.output === 'on' ? true : o.output === 'off' ? false : cyc >= 0.46 && cyc < 0.88 && color !== 'yellow';
      const rate = 14 + 9 * Math.sin(t * 0.27 + i * 1.7) + 6 * Math.sin(t * 0.71 + i);
      rows.push({
        thread_id: 'synthetic-' + i, turn_id: 'turn-' + i, model: MODELS[i % MODELS.length], requested_model: MODELS[i % MODELS.length],
        route: color === 'green' ? 'match' : color === 'red' ? 'mismatch' : 'pending', color,
        activity: outputting ? 'running' : color === 'yellow' ? 'tool_wait' : 'running',
        speed: '—/s', speed_valid: false, speed_fresh: false, turn_anomaly: color === 'red', health_valid: true,
        outputting, estimated_visible_tokens_per_second: outputting ? Math.max(1, rate) : 0, output_activity: outputting ? rate * 2.4 : 0,
        activity_scope: 'turn',
      });
    }
    const quota = o.quota === 'unknown' ? { known: false, fresh: false }
      : { known: true, fresh: o.quota !== 'stale', remaining_percent: o.quota === 'stale' ? 64 : +o.quota, bucket: 'synthetic-week' };
    return { mode: 'synthetic', revision: Math.floor(t * 5), ready: true, pending_completed: o.pending ? 1 : 0, rows, quota };
  }
  NS.synth = synth;
})(window.RA);
