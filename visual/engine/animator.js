// Snapshot -> Animator.set -> frame() -> Scene. Same data path as the Go host.
// Consumes the existing Snapshot JSON shape (rows/quota/pending_completed/mode).
(function (NS) {
  'use strict';
  const { clamp01, smoother, strHash, Spring, Envelope } = NS.core;
  const W = 388, MAX_VISIBLE = 16, MAX_H = 500, P = NS.presentation;
  const localDissolve=(p,t)=>p.gone>=0
    ? (p.exitFrom||0)+(1-(p.exitFrom||0))*smoother((t-p.gone)/.9)
    : (p.returnFrom||0)*(1-smoother((t-p.returnAt)/.9));

  function layout(n, maxH) {
    const vis = Math.min(n, MAX_VISIBLE);
    const font = Math.max(12.6, 17.0 - 1.2 * Math.log1p(Math.max(0, n - 1)));
    const rowH = Math.max(23, 33 - 3.4 * Math.log1p(Math.max(0, n - 1)));
    const H = Math.min(maxH || MAX_H, Math.max(112, 58 + rowH * vis));
    const fit = Math.max(1, Math.floor((H - 52) / rowH));
    return { H, font, rowH, vis: n ? Math.min(vis, fit) : 0 };
  }

  class Animator {
    constructor() {
      this.stage = new NS.Stage(); this.ribbons = new NS.Ribbons();
      this.glassDynamics = new window.BG16.dynamics.Dynamics();
      this.glassAppearance = this.glassDynamics.appearance;
      this.panelWanted = true;this.glassWanted=true;this.panelReplay=false;this.glassReplay=false;this.lifecycle='visible';
      this.poses = new Map(); this.t = 0; this.breathe = 0;this.motionTime=0;this.reducedMotion=false;this.offset=0;
      this.H = new Spring(112, 1.05, 1); this.quotaMotion = new NS.QuotaMotion(); this.qy = new Spring(56, 0.9, 1);
      this.qlevel = new Spring(0, 0.45, 1); this.qval = new Spring(0, 0.8, 1);
      this.sr = new Spring(0.22, 1, 1); this.sg = new Spring(0.42, 1, 1); this.sb = new Spring(0.33, 1, 1);
      this.snapshot = { rows: [], quota: { known: false } }; this.layout = layout(0);
    }
    set(s, offset, maxH) {
      this.snapshot = s; const rows = s.rows || [];
      const L = (this.layout = layout(rows.length, maxH)); offset = Math.max(0, Math.min(offset || 0, rows.length - L.vis));
      this.offset=offset;
      const previousHeight=this.H.target;
      const live = new Set(), top = 30, mid = (top + L.H - 20) * 0.5;
      for (let i = 0; i < L.vis; i++) {
        const row = rows[offset + i], id = row.thread_id, y = mid + (i - (L.vis - 1) / 2) * L.rowH;
        live.add(id);
        let p = this.poses.get(id);
        if (!p) {
          p = { id, seed: strHash(id), admitted:false, born: this.t, gone: -1, returnAt:0, returnFrom:0, y: new Spring(y + 7, 1.5, 0.92), font: new Spring(L.font * 0.94, 1.2, 1),
            rate: new NS.RateTween(), rowMotion:new NS.RowMotion(strHash(id)), mix: new Spring(1, 2.2, 1), env: new Envelope(11, 2.6),outputLight:new Envelope(8,4),lightPhase:strHash(id),
            model:row.model,pendingModel:null,retiring:false,nameReady:0,prev:'',rateText:'',rateAt:-1 };
          this.poses.set(id, p);
        }
        p.pendingModel=p.model===row.model?null:row.model;
        if(p.gone>=0){p.returnFrom=localDissolve(p,this.t);p.returnAt=this.t;p.gone=-1;}
        p.row = row; p.index = i; p.retiring=false;
        p.y.set(y); p.font.set(L.font);
        let rate = row.estimated_visible_tokens_per_second || 0;
        if (rate <= 0 && row.outputting) rate = (row.output_activity || 0) * 0.4;
        let show = !!row.outputting && rate > 0;
        // A completed response can contain the first accurate speed sample.
        // Accept each fresh response sample once; a finite presentation latch
        // lets the complete name enter before opening, without renewing on replay.
        const completionKey=[row.turn_id,row.response_id,row.ended_at,row.tokens_per_second].join('|');
        const measured=!!row.speed_fresh&&!!row.speed_valid&&Number.isFinite(row.tokens_per_second)&&row.tokens_per_second>0;
        if(!row.outputting&&measured&&completionKey!==p.completionKey){
          p.completionKey=completionKey;p.completion={key:completionKey,rate:row.tokens_per_second,until:this.t+6,shown:0};
          // The display continues its estimate tween through the finite tail.
          // Raw measured evidence remains in the snapshot, not a visual snap.
        }
        if(row.outputting||row.health_valid===false||s.ready===false||
          (p.completion&&p.completion.key!==completionKey))p.completion=null;
        if(!show&&p.completion&&this.t<p.completion.until&&p.completion.shown<1.1){
          rate=p.completion.rate;show=true;
        }
        show=show&&Number.isFinite(rate);
        if (show) p.rate.set(Math.min(999, rate),this.t);
        p.rowMotion.setOutput(show); p.show = show; p.rawRate = Number.isFinite(rate)?rate:0;
      }
      // Only admitted rows own exit clips. Unseen intermediate snapshots are
      // replaced by the latest page; at most 16 painted + 16 waiting rows exist.
      for (const [id,p] of this.poses) if (!live.has(id)){
        if(!p.admitted)this.poses.delete(id);
        else{p.retiring=true;p.completion=null;}
      }
      this.H.set([...this.poses.values()].some(p=>p.retiring)?Math.max(previousHeight,L.H):L.H);
      const q = s.quota || {};
      this.qy.set(mid);
      this.qlevel.set(q.known ? clamp01(q.remaining_percent / 100) : 0); if (q.known) this.qval.set(q.remaining_percent);
    }
    status(T) {
      const rows = this.snapshot.rows || [];
      if (this.stage.exiting) return ['DISAPPEARING', [0.29, 0.43, 0.35], true];
      if (this.stage.entering && T.enter < 0.97) return ['INITIALIZING', [0.20, 0.45, 0.34], true];
      if (rows.some((r) => r.color === 'red')) return ['ROUTE ANOMALY', [0.66, 0.22, 0.24], false];
      if (rows.some((r) => r.color === 'yellow')) return ['VERIFYING ROUTE', [0.58, 0.43, 0.15], false];
      if (!rows.length) return ['STANDBY', [0.42, 0.42, 0.38], false];
      return ['NORMAL RUNNING', [0.16, 0.43, 0.31], false];
    }
    frame(dt, quality) {
      dt = Math.max(0, Math.min(0.1, dt)); this.t += dt; const t = this.t;
      const still=this.reducedMotion||quality===3,motionDt=still?0:dt;
      this.motionTime+=motionDt;
      if(still){
        this.panelReplay=this.glassReplay=false;this.lifecycle=this.panelWanted?'visible':'hidden';
        this.panelWanted?this.stage.showImmediate():this.stage.hideImmediate();
        if(this.glassAppearance.mode!==(this.panelWanted&&this.glassWanted?'visible':'hidden'))
          this.glassDynamics.immediate(this.panelWanted&&this.glassWanted);
      }else this.updateLifecycle(dt);
      const T = this.stage.timeline();
      this.breathe += motionDt * (2 * Math.PI / 6.4);
      const H = this.H.step(dt), rows = [], ribRows = [];
      let eMax = 0, eSum = 0;
      for(const[id,p]of this.poses)if((p.gone>=0&&localDissolve(p,t)>=.999999)||(still&&p.retiring))this.poses.delete(id);
      let slots=[...this.poses.values()].filter(p=>p.admitted).length;
      for(const p of [...this.poses.values()].filter(p=>!p.admitted&&!p.retiring).sort((a,b)=>a.index-b.index)){
        if(slots>=MAX_VISIBLE)break;p.admitted=true;p.born=t+.07*(p.index%6);slots++;
      }
      for (const [id, p] of this.poses) {
        if(!p.admitted)continue;
        if(p.completion&&(t>=p.completion.until||p.completion.shown>=1.1)){
          p.completion=null;if(!p.row.outputting){p.show=false;p.rowMotion.setOutput(false);}
        }
        const r = p.row, out = r.outputting ? 0.32 + 0.68 * clamp01(p.rawRate / 42) : r.activity === 'running' ? 0.1 : 0;
        const e = p.env.step(p.retiring ? 0 : out, dt); eMax = Math.max(eMax, e); eSum += e;
        const y = p.y.step(dt), font = p.font.step(dt), rate = p.rate.step(t);
        const outputLight=p.outputLight.step(r.outputting&&!p.retiring?1:0,dt);
        p.lightPhase=(p.lightPhase+motionDt*(P.outputTextFlowTurns+.20*clamp01(rate/60)))%1;
        if (t - p.rateAt > P.rateRefresh) {
          const digit=Math.round(rate*10);if(digit!==p.rateDigit){p.rateDigit=digit;p.rateText=(digit/10).toFixed(1);}p.rateAt=t;
        }
        const i = p.index || 0;
        // Normalized staggering lets every row reach a complete name, including
        // rows 7..16; all enter/exit windows still end at the parent's endpoint.
        const stagger=.018*Math.min(i,15);
        const reveal = still?1:Math.min(smoother((t - p.born) / 0.95), clamp01((T.rowsIn-stagger)/(1-stagger)));
        if(still)p.mix.snap(1);
        const mix=p.mix.step(dt);
        const forceClose=this.lifecycle!=='visible'||!this.panelWanted||p.retiring||!!p.pendingModel;
        const ready=!forceClose&&reveal>=.999999&&mix>=.999&&localDissolve(p,t)<.000001;
        p.nameReady=ready?p.nameReady+dt:0;
        let rateOp;
        if(still){
          p.rowMotion.open=p.show&&!p.retiring?1:0;p.rowMotion.phase=p.rowMotion.open?'open':'closed';
          rateOp=p.rowMotion.open;
        }else rateOp=p.rowMotion.step(dt,rate,p.nameReady>=P.rowNameHold,forceClose);
        if(p.completion&&rateOp>=.98)p.completion.shown+=dt;
        if(p.rowMotion.phase==='closed'){
          if(p.retiring&&p.gone<0){p.exitFrom=localDissolve(p,t);p.gone=t;}
          if(p.pendingModel&&!p.retiring&&mix>=.999){
            p.prev=p.model;p.model=p.pendingModel;p.pendingModel=null;p.mix.snap(0).set(1);p.nameReady=0;
          }
        }
        if(still&&p.pendingModel){p.prev='';p.model=p.pendingModel;p.pendingModel=null;p.mix.snap(1);}
        const dissolve = still?0:Math.max(localDissolve(p,t), clamp01((T.rowsOut-stagger)/(1-stagger)));
        rows.push({ id, model: p.model, prev: p.prev, mix:p.mix.x, color: r.color, activity: r.activity, y, font,
          reveal, dissolve, rate, rateText: p.rateText, rateOp, orbAngle:p.rowMotion.angle, energy: e, seed: p.seed,outputLight,lightPhase:p.lightPhase });
        ribRows.push({ id, y, font, energy: e, rate: p.rawRate, outputting: !!r.outputting, alive: p.gone < 0 });
      }
      if(![...this.poses.values()].some(p=>p.retiring))this.H.set(this.layout.H);
      const energy = clamp01(eMax * 0.65 + (rows.length ? eSum / rows.length : 0) * 0.35);
      this.ribbons.sync(ribRows, t); this.ribbons.update(motionDt, t, H, this.stage, T, quality, this.breathe, energy);
      const [stText, stRgb, dots] = this.status(T);
      const rgb = [this.sr.set(stRgb[0]).step(dt), this.sg.set(stRgb[1]).step(dt), this.sb.set(stRgb[2]).step(dt)];
      const q = this.snapshot.quota || {}, qv = this.qval.step(dt), n = (this.snapshot.rows || []).length;
      const qm = this.quotaMotion.step(n > 0, rows.every(r => r.dissolve >= .999),
        T.quotaIn >= .98 && !this.stage.exiting, this.stage.hidden, dt);
      return { W, H, t:this.motionTime, dt, motionDt, T, breathe: this.breathe, energy, rows, ribbons: this.ribbons,glassAppearance:this.glassAppearance,glassDynamics:this.glassDynamics,
        stage: { entering: this.stage.entering, exiting: this.stage.exiting, hidden: this.stage.hidden },
        quota: { known: !!q.known, fresh: q.fresh !== false, text: q.known ? Math.round(qv) + '%' : '—', level: this.qlevel.step(dt),
          cx: W*.5+(P.quotaDockX-W*.5)*qm.dock, cy: this.qy.step(dt), size: P.quotaIdleSize+(P.quotaDockSize-P.quotaIdleSize)*qm.dock,
          dock: qm.dock, arcEnd: qm.arcEnd, arcStart: qm.arcStart, motionPhase: qm.phase },
        pending: this.snapshot.pending_completed || 0, synthetic: this.snapshot.mode === 'synthetic',
        status: { text: stText + (dots ? ' ' + '. . .'.slice(0, 1 + 2 * (Math.floor(t * 2.5) % 3)) : ''), rgb },
        total: n, visible: this.layout.vis, offset:this.offset };
    }
    updateLifecycle(dt) {
      const g=this.glassDynamics;
      // Overlap only the pre-lift with the final slice of row retraction.
      // Falling glass and foreground dissolve still wait for closed rows.
      const exitLead=Math.min(P.glassExitLead,window.BG16.appearance.params.anticipation);
      let rowsClosed=true,preliftReady=true;
      for(const p of this.poses.values()){
        const m=p.rowMotion;
        if(m.phase!=='closed'){
          rowsClosed=false;
          if(m.phase!=='closing'||P.rowCollapseDuration-m.elapsed>exitLead+1e-7)preliftReady=false;
        }
      }
      if(this.lifecycle==='hidden'&&this.panelWanted){
        this.panelReplay=false;this.stage.beginReveal();this.lifecycle='enter';
      }else if(this.lifecycle==='visible'){
        if(!this.panelWanted||this.panelReplay){
          this.panelReplay=false;this.lifecycle='rows-exit';
        }else{
          if(g.desiredShown!==this.glassWanted)this.glassWanted?g.show():g.hide();
          if(this.glassReplay){this.glassReplay=false;g.replayAppearance();}
        }
      }
      if(this.lifecycle==='rows-exit'&&preliftReady){
        g.hide();this.lifecycle='glass-exit';
      }
      // Cue glass entry at the configured foreground progress.
      if(this.lifecycle==='enter'&&this.stage.pe>=P.glassEnterAt&&!g.desiredShown)g.show();
      g.update(dt,this.lifecycle==='visible');
      // The first falling slab cues the curtain. Both complete independently
      // before a queued reveal may start, after model rows have closed.
      if(this.lifecycle==='glass-exit'&&rowsClosed&&
        (this.glassAppearance.mode==='hidden'||
         (this.glassAppearance.mode==='exit'&&this.glassAppearance.elapsed>=window.BG16.appearance.params.anticipation))){
        this.stage.beginExit();this.lifecycle='panel-exit';
      }
      this.stage.update(dt);
      if(this.lifecycle==='enter'&&!this.stage.transitioning&&this.glassAppearance.mode==='visible'){
        this.lifecycle='visible';
      }else if(this.lifecycle==='panel-exit'&&this.stage.hidden&&this.glassAppearance.mode==='hidden'){
        this.lifecycle='hidden';
      }
    }
    // These are target setters. Automatic hosts can call them repeatedly;
    // the finite lifecycle, rather than the caller, owns all clip starts.
    beginReveal() { this.panelWanted=true;this.glassWanted=true;this.panelReplay=false;this.glassReplay=false; }
    beginExit() { this.panelWanted=false;this.panelReplay=false;this.glassReplay=false; }
    beginReplay() { this.panelWanted=true;this.glassWanted=true;this.panelReplay=this.lifecycle!=='hidden';this.glassReplay=false; }
    setReducedMotion(value) { this.reducedMotion=!!value; }
    showGlass() { this.beginReveal(); }
    hideGlass() { this.glassWanted=false;this.glassReplay=false; }
    replayGlass() { this.beginReveal();this.glassReplay=this.lifecycle==='visible'; }
    showImmediate() { this.panelWanted=this.glassWanted=true;this.panelReplay=this.glassReplay=false;this.lifecycle='visible';this.stage.showImmediate();this.glassDynamics.immediate(true); }
    hideImmediate() { this.panelWanted=this.glassWanted=false;this.panelReplay=this.glassReplay=false;this.lifecycle='hidden';this.stage.hideImmediate();this.glassDynamics.immediate(false);for(const p of this.poses.values()){p.rowMotion.closeImmediate();p.nameReady=0;} }
  }
  NS.Animator = Animator; NS.PANEL_W = W; NS.layout = layout;
})(window.RA);
