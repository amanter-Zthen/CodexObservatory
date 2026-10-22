// Scene -> glyph instances (24 floats each) + orb uniforms + dissolve emitters.
// Layout constants match scene_hud: header line y=22, footer text at H-7.5.
(function (NS) {
  'use strict';
  const { clamp01, smoother, easeOutExpo, hash1 } = NS.core;
  const MAXG = 2304, F = 24, REF = 56;
  const INK = Object.fromEntries(Object.entries(NS.statusPalette).map(([key,value])=>[key,value.ink]));
  const STYLE = { green: 1, red: 2, yellow: 3 };

  class Labels {
    constructor(atlas) {
      this.atlas = atlas; this.buf = new Float32Array(MAXG * F); this.n = 0; this.emitters = [];
      this.orbA = new Float32Array(64); this.orbB = new Float32Array(64);this.rowA=new Float32Array(64);this.rowLayouts=[];this.hudGlyphs=[];
    }
    text(key, str, x, by, size, col, a, style, rev, dis, seed, track, fx2, motion) {
      if (a <= 0.003 || !str) return 0;
      const k = size / REF, at = this.atlas, w = at.measure(key, str, size, track || 0);
      if(this.collecting){for(const ch of str)if(ch!==' ')at.want(key,ch);return w;}
      let pen = x, i = 0; const chars = [...str], len = chars.length;
      for (const ch of chars) {
        const g = ch===' '?{adv:at.advance(key,ch)}:at.get(key, ch);
        if (ch !== ' ' && this.n < MAXG) {
          const m=motion?NS.hudMotion.glyph(motion.side,motion.index+i,motion.count,motion.T):null;
          const xr = len > 1 ? i / (len - 1) : 0, o = this.n * F, b = this.buf;
          const rv = clamp01(rev * 1.6 - xr * 0.6), dv = clamp01(dis * 1.5 - (1 - xr) * 0.3 - hash1(seed * 31 + i) * 0.2);
          b[o] = pen - g.ox * k+(m?m.x:0); b[o + 1] = by - g.base * k; b[o + 2] = g.w * k; b[o + 3] = g.h * k;
          b[o + 4] = g.x; b[o + 5] = g.y; b[o + 6] = g.w; b[o + 7] = g.h;
          b[o + 8] = col[0]; b[o + 9] = col[1]; b[o + 10] = col[2]; b[o + 11] = a*(m?m.alpha:1);
          b[o + 12] = style; b[o + 13] = rv; b[o + 14] = dv; b[o + 15] = seed + i * 0.137;
          b[o + 16] = fx2 ? fx2[0] : -999; b[o + 17] = size; b[o + 18] = fx2 ? fx2[1] : 0; b[o + 19] = fx2 ? fx2[2] : 0;
          b[o + 20] = fx2?.[3] || 0; b[o + 21] = fx2?.[4] || 0; b[o + 22] = 0; b[o + 23] = 0;
          this.n++;
          if(m)this.hudGlyphs.push({side:motion.side,char:ch,index:motion.index+i,targetX:pen,x:pen+m.x,alpha:a*m.alpha,entering:m.entering,leaving:m.leaving});
        }
        pen += g.adv * k + (track || 0); i++;
      }
      if (dis > 0.02 && dis < 0.97 && this.emitters.length < 48) this.emitters.push({ id:key+':'+seed, x, y: by - size * 0.75, w, h: size * 0.8, rate: 36 + w * 0.5, col });
      return w;
    }
    build(s,budgetMs=Infinity){
      this.collecting=true;this.atlas.requested.clear();this.buildFrame(s);this.collecting=false;
      this.ready=this.atlas.prepare(budgetMs);
      if(this.ready)this.buildFrame(s);
      return this.ready?this.n:0;
    }
    buildFrame(s) {
      this.n = 0; this.emitters.length = 0;this.rowLayouts.length=0;this.hudGlyphs.length=0;
      const T = s.T, W = s.W, H = s.H, t = s.t, at = this.atlas, grey = [0.30, 0.30, 0.28], soft = [0.45, 0.44, 0.41];
      // Header identity only. Routing semantics remain on the model rows.
      const heading='CODEX OBSERVATORY',headerCount=heading.length+3;
      let x = this.text('hud',heading,18,16.2,8.2,grey,1,5,1,0,1.1,1.05,null,{side:'header',index:0,count:headerCount,T});
      this.text('hud','OS',18+x+4,16.2,8.2,INK.green,1,5,1,0,2.3,1.05,null,{side:'header',index:heading.length+1,count:headerCount,T});
      // Footer: keep the right signature and overflow count.
      const fr = T.detailIn, fd = T.textOut, fy = H - 7.5;
      const pw = at.measure('hud', 'CODEX', 6, 0.8), qw = at.measure('hudl', 'POWERED BY ', 5.4, 0.8);
      this.text('hudl','POWERED BY ',W-33-pw-qw,fy,5.4,soft,.9,5,1,0,7.3,.8,null,{side:'footer',index:0,count:15,T});
      this.text('hud','CODEX',W-33-pw,fy,6,grey,1,5,1,0,8.9,.8,null,{side:'footer',index:10,count:15,T});
      if (s.total > s.visible) { const start=(s.offset||0)+1,v = start+'–'+(start+s.visible-1)+' / '+s.total; this.text('hudl', v, W * 0.5 - at.measure('hudl', v, 5.6, 0.6) / 2, fy, 5.6, soft, 0.9, 5, fr, fd, 9.4, 0.6); }
      // Quota instrument.
      const q = s.quota, qr = T.quotaIn, qd = T.quotaOut;
      const ringR = Math.max(24, Math.min(46, q.cy - 26, H - 17 - q.cy))*NS.presentation.quotaRingScale;
      if (q.known) {
        const num = String(Math.round(parseFloat(q.text) * easeOutExpo(qr))) + '%';
        const maxWidth = 105+(2*ringR-8-105)*q.dock;
        const ns = Math.min(q.size, maxWidth/at.measure('quota',num,1,0));
        const width=at.measure('quota',num,ns,0), by=q.cy+ns*.35;
        const ink=q.fresh?[.29,.29,.26]:[.44,.44,.41];
        this.text('quota',num,q.cx-width/2,by,ns,ink,1,q.fresh?4:0,qr,qd,11.3,0);
      } else {
        const cw = at.measure('cjk', '额度未获取', 9, 0.4), dw = at.measure('quota', '—', q.size * 0.8, 0);
        this.text('quota', '—', q.cx - dw / 2, q.cy + q.size * 0.2, q.size * 0.8, soft, 0.8, 0, qr, qd, 11.3, 0);
        this.text('cjk', '额度未获取', q.cx - cw / 2, q.cy + q.size * 0.2 + 13, 9, soft, 0.9, 0, T.detailIn, qd, 13.7, 0.4);
      }
      // Sessions.
      let orbN = 0, rowsA = 0;
      for (const r of s.rows) {
        const col = INK[r.color] || INK.yellow, style = STYLE[r.color] || 3, by = r.y + r.font * 0.36, a = clamp01(1 - r.dissolve * 0.2);
        const pulse = 0.5 + 0.5 * Math.sin(t * 3.3 + r.seed * 6), br = 0.5 - 0.5 * Math.cos(t * 2.244 + r.seed * 6);
        const glow = r.color === 'green' ? 0.06 + 0.22 * r.energy : r.color === 'red' ? 0.42 + 0.3 * pulse : 0.05 + 0.36 * br;
        const layout=NS.rowLayout(at,r,W),name=layout.name,previous=layout.previous;
        this.rowLayouts.push(layout);
        const active=(r.outputLight||0)*smoother(r.reveal)*(1-r.dissolve);
        const phase=r.lightPhase||0,crest=.90+.10*Math.sin(phase*Math.PI*4);
        const sweep=layout.modelX-34+phase*(layout.rateRight-layout.modelX+68)+.35*by;
        const idleX=r.color==='green'?layout.modelX-50+((t*(.16+.2*r.energy)+r.seed)%1)*(name.width+140)+.35*by:sweep;
        const idleBand=r.color==='green'?.35+.65*r.energy:0;
        const litGlow=glow+NS.presentation.outputTextGlow*active*crest;
        const sheen=[idleX+(sweep-idleX)*active,litGlow,idleBand+(NS.presentation.outputTextBandStrength*crest-idleBand)*active,active,phase];
        const rateSheen=[sweep,glow*.7+NS.presentation.outputTextGlow*active*crest,NS.presentation.outputTextBandStrength*active*crest,active,phase];
        if (r.prev && r.mix < 1) this.text('name',previous.text,layout.modelX,by-3*r.mix,previous.size,col,a*(1-r.mix),style,r.reveal,Math.max(r.dissolve,r.mix),r.seed*5+3,previous.track,sheen);
        this.text('name',name.text,layout.modelX,by+3*(1-r.mix),name.size,col,a*Math.min(1,r.mix*1.5),style,r.reveal,r.dissolve,r.seed*5,name.track,sheen);
        if (layout.rateVisibility > 0.003 && r.rateText && r.rateText !== '0.0') {
          const rs=layout.rateSize,sW=layout.unitWidth,dW=at.measure('rate',r.rateText,rs,.3),xr=layout.rateRight;
          const rv=Math.min(r.reveal,layout.rateVisibility),ra=layout.rateVisibility;
          this.text('rate', r.rateText, xr - sW - dW - 1, by, rs, col, ra, style, rv, r.dissolve, r.seed * 5 + 1, 0.3, rateSheen);
          this.text('rate', '/s', xr - sW, by, rs * 0.78, col, ra * 0.62, style, rv, r.dissolve, r.seed * 5 + 2, 0.2, rateSheen);
        }
        if (orbN < 16) {
          const o = orbN * 4, oa = Math.min(1, r.reveal * 1.4) * (1 - r.dissolve);
          this.orbA.set([layout.orbX, r.y, r.color === 'green' ? 0 : r.color === 'red' ? 1 : 2, oa], o);
          this.rowA.set([layout.rulerStart,layout.rulerEnd,layout.open,r.orbAngle],o);
          this.orbB.set([r.energy, r.dissolve, r.seed, r.font], o); orbN++; rowsA = Math.max(rowsA, oa);
        }
      }
      this.orbN = orbN; this.rowsA = rowsA; this.ringR = ringR;
      this.seaLv = q.known ? q.level * T.quotaIn * (1 - T.frameOut) : 0;
      return this.n;
    }
  }
  NS.Labels = Labels; NS.GLYPH_FLOATS = F; NS.GLYPH_MAX = MAXG;
})(window.RA);
