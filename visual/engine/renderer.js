// G01: scene -> glyph aura -> particles -> bloom -> composite -> readable ink.
(function (NS) {
  'use strict';
  const G = NS.gl, SH = NS.SH, LEVELS = 5;
  const MARGIN = NS.presentation.renderMargin;

  class Renderer {
    constructor(canvas) {
      const gl = (this.gl = canvas.getContext('webgl2', { alpha: true, premultipliedAlpha: true, antialias: false, depth: false, stencil: false }));
      if (!gl) throw new Error('WebGL2 不可用');
      this.canvas = canvas; this.fmt = G.formats(gl); this.drift = 0;
      this.glass = new NS.GlassBackground(gl,new URLSearchParams(location.search).has('glass-copy'));
      this.pendingGPU=null;this.skipped=0;this.submitted=0;
      this.scene = G.program(gl, G.VS_FULL, SH.sceneLib + SH.sceneLayers + SH.sceneHud + SH.sceneRib, 'scene');
      this.textP = G.program(gl, SH.textVS, SH.textFS, 'text');
      this.partP = G.program(gl, SH.partVS, SH.partFS, 'particles');
      this.down = G.program(gl, G.VS_FULL, SH.downFS, 'down');
      this.up = G.program(gl, G.VS_FULL, SH.upFS, 'up');
      this.comp = G.program(gl, G.VS_FULL, SH.compFS, 'composite');
      this.quad = G.quad(gl); this.glyphs = G.instanced(gl, NS.GLYPH_FLOATS/4); this.parts = G.instanced(gl, 3);
      gl.pixelStorei(gl.UNPACK_ALIGNMENT, 1);
      this.atlasTex = gl.createTexture(); gl.bindTexture(gl.TEXTURE_2D, this.atlasTex);
      gl.texImage2D(gl.TEXTURE_2D, 0, gl.R8, NS.ATLAS.AW, NS.ATLAS.AH, 0, gl.RED, gl.UNSIGNED_BYTE, null);
      for (const [k, v] of [[gl.TEXTURE_MIN_FILTER, gl.LINEAR], [gl.TEXTURE_MAG_FILTER, gl.LINEAR], [gl.TEXTURE_WRAP_S, gl.CLAMP_TO_EDGE], [gl.TEXTURE_WRAP_T, gl.CLAMP_TO_EDGE]]) gl.texParameteri(gl.TEXTURE_2D, k, v);
    }
    resize(w, h) {
      if (this.w === w && this.h === h) return;
      const gl = this.gl; this.w = w; this.h = h;
      if (this.main) { this.main.free(); this.bloom.forEach((b) => b.free()); }
      this.main = G.target(gl, w, h, this.fmt, 2);
      this.bloom = [];
      for (let i = 0, bw = w, bh = h; i < LEVELS; i++) { bw = Math.max(1, bw >> 1); bh = Math.max(1, bh >> 1); this.bloom.push(G.target(gl, bw, bh, this.fmt, 1)); }
      if(!this.main.ok||this.bloom.some(b=>!b.ok))throw Error('Framebuffer allocation failed');
    }
    ready() {
      const g=this.gl;if(g.isContextLost())throw Error('Graphics context lost');
      if(!this.pendingGPU)return true;
      const state=g.clientWaitSync(this.pendingGPU,0,0);
      if(state===g.TIMEOUT_EXPIRED){
        this.skipped++;
        if(performance.now()-this.pendingSince>3000)throw Error('Graphics queue has stopped responding');
        return false;
      }
      if(state===g.WAIT_FAILED)throw Error('Graphics completion check failed');
      g.deleteSync(this.pendingGPU);this.pendingGPU=null;return true;
    }
    uploadAtlas(atlas) {
      if (!atlas.dirty.length) return;
      const gl = this.gl; gl.bindTexture(gl.TEXTURE_2D, this.atlasTex);
      for (const d of atlas.dirty) gl.texSubImage2D(gl.TEXTURE_2D, 0, d.x, d.y, d.w, d.h, gl.RED, gl.UNSIGNED_BYTE, d.data);
      atlas.dirty.length = 0;
    }
    submitFrame(){
      const gl=this.gl;
      if(this.pendingGPU)gl.deleteSync(this.pendingGPU);
      this.pendingGPU=gl.fenceSync(gl.SYNC_GPU_COMMANDS_COMPLETE,0);this.pendingSince=performance.now();gl.flush();this.submitted++;
    }
    render(s, labels, parts, o) {
      const gl = this.gl, scale = o.scale;
      this.resize(this.canvas.width, this.canvas.height);
      if (s.stage.hidden) { gl.bindFramebuffer(gl.FRAMEBUFFER, null); gl.viewport(0, 0, this.w, this.h); gl.clearColor(0, 0, 0, 0); gl.clear(gl.COLOR_BUFFER_BIT);this.submitFrame(); return; }
      this.drift += (s.motionDt??s.dt) * (0.25 + 0.9 * s.energy);
      const T = s.T, R = s.ribbons, q = s.quota;
      const L = NS.layers.enabled, soloGlow=NS.layers.soloKey==='glow';
      const show=k=>soloGlow||L[k], profile=o.backdrop||'auto', glassEnabled=profile!=='legacy';
      if(show('background')&&glassEnabled)this.glass.update(s.t,profile,s.glassDynamics);
      const crop=this.glass.crop(profile,s.W,s.H,s.quota.dock);this.lastCrop=crop;this.lastProfile=profile;
      gl.bindFramebuffer(gl.FRAMEBUFFER, this.main.fb); gl.viewport(0, 0, this.w, this.h);
      gl.disable(gl.BLEND);
      let P = this.scene, u = P.u; gl.useProgram(P.p);
      gl.uniform2f(u.uRes, this.w, this.h); gl.uniform1f(u.uScale, scale); gl.uniform2f(u.uMargin, MARGIN, MARGIN); gl.uniform2f(u.uPanel, s.W, s.H);
      gl.uniform1f(u.uTime, s.t); gl.uniform1f(u.uBreathe, s.breathe); gl.uniform1f(u.uEnergy, s.energy); gl.uniform1f(u.uQuality, o.quality);
      gl.uniform1f(u.uDrift, this.drift); gl.uniform1f(u.uRowsA, labels.rowsA);
      gl.uniform4f(u.uS1, T.glowIn, T.slabIn, T.frameIn, T.ringIn); gl.uniform4f(u.uS2, T.headIn, T.quotaIn, T.detailIn, T.glowPulse);
      gl.uniform4f(u.uS3, T.textOut, T.frameOut, T.ringOut, T.slabOut); gl.uniform4f(u.uS4, T.glowOut, T.snake, T.enter, T.exit);
      gl.uniform4f(u.uQuota, q.cx, q.cy, q.level, q.known ? (q.fresh ? 3 : 1) : 0); gl.uniform1f(u.uRingR, labels.ringR); gl.uniform2f(u.uQuotaArc, q.arcEnd, q.arcStart);
      gl.uniform1fv(u.uRibD, R.D); gl.uniform4fv(u.uRibA, R.A); gl.uniform4fv(u.uRibB, R.B); gl.uniform4fv(u.uRibC, R.C); gl.uniform1i(u.uRibN, R.count || 0);
      gl.uniform1f(u.uRibDetail,R.detailAmount||0);
      const identity=NS.hudMotion.bars(T);
      gl.uniform3f(u.uIdentity,identity.headerRise,identity.headerReach,identity.footerReach);
      gl.uniform4fv(u.uOrbA, labels.orbA); gl.uniform4fv(u.uOrbB, labels.orbB);gl.uniform4fv(u.uRowA,labels.rowA); gl.uniform1i(u.uOrbN, labels.orbN);
      gl.uniform3fv(u.uStatus, s.status.rgb);
      gl.uniform4f(u.uLayers,+show('background'),+show('curves'),+show('particles'),+show('frame'));gl.uniform1f(u.uInfo,+show('info'));
      gl.uniform1f(u.uGlassEnabled,+glassEnabled);gl.uniform2f(u.uGlassCanvas,1672,941);
      gl.uniform2f(u.uGlassFocus,NS.presentation.focusClearBand,o.glassFocus===false?0:NS.presentation.focusMaxRadius);
      gl.uniform4f(u.uGlassCrop,crop.x,crop.y,crop.width,crop.height);
      gl.activeTexture(gl.TEXTURE3);gl.bindTexture(gl.TEXTURE_2D,this.glass.texture);gl.uniform1i(u.uGlassTex,3);gl.activeTexture(gl.TEXTURE0);
      gl.bindVertexArray(this.quad); gl.drawArrays(gl.TRIANGLES, 0, 6);
      gl.enable(gl.BLEND); gl.blendFunc(gl.ONE, gl.ONE_MINUS_SRC_ALPHA);
      if (labels.n && show('info')) {
        P = this.textP; u = P.u; gl.useProgram(P.p);
        gl.uniform1f(u.uInkPass, 0);
        gl.uniform2f(u.uRes, this.w, this.h); gl.uniform2f(u.uMargin, MARGIN, MARGIN); gl.uniform1f(u.uScale, scale);
        gl.uniform2f(u.uAtlas, NS.ATLAS.AW, NS.ATLAS.AH); gl.uniform2f(u.uPanel, s.W, s.H); gl.uniform1f(u.uTime, s.t); gl.uniform1f(u.uSeaLv, labels.seaLv);
        gl.activeTexture(gl.TEXTURE0); gl.bindTexture(gl.TEXTURE_2D, this.atlasTex); gl.uniform1i(u.uTex, 0);
        gl.bindBuffer(gl.ARRAY_BUFFER, this.glyphs.buf); gl.bufferData(gl.ARRAY_BUFFER, labels.buf.subarray(0, labels.n * NS.GLYPH_FLOATS), gl.STREAM_DRAW);
        gl.bindVertexArray(this.glyphs.vao); gl.drawArraysInstanced(gl.TRIANGLES, 0, 6, labels.n);
      }
      if (parts.count && show('particles')) {
        P = this.partP; u = P.u; gl.useProgram(P.p);
        gl.uniform2f(u.uRes, this.w, this.h); gl.uniform2f(u.uMargin, MARGIN, MARGIN); gl.uniform1f(u.uScale, scale);
        gl.bindBuffer(gl.ARRAY_BUFFER, this.parts.buf); gl.bufferData(gl.ARRAY_BUFFER, parts.inst.subarray(0, parts.count * NS.PARTICLE_FLOATS), gl.STREAM_DRAW);
        gl.bindVertexArray(this.parts.vao); gl.drawArraysInstanced(gl.TRIANGLES, 0, 6, parts.count);
      }
      gl.disable(gl.BLEND); gl.bindVertexArray(this.quad);
      const levels = o.quality >= 2 ? 3 : LEVELS;
      let src = this.main.tex[1], sw = this.w, sh = this.h;
      gl.useProgram(this.down.p);
      for (let i = 0; i < levels; i++) {
        const b = this.bloom[i]; gl.bindFramebuffer(gl.FRAMEBUFFER, b.fb); gl.viewport(0, 0, b.w, b.h);
        gl.activeTexture(gl.TEXTURE0); gl.bindTexture(gl.TEXTURE_2D, src); gl.uniform1i(this.down.u.uSrc, 0); gl.uniform2f(this.down.u.uTexel, 1 / sw, 1 / sh);
        gl.drawArrays(gl.TRIANGLES, 0, 6); src = b.tex[0]; sw = b.w; sh = b.h;
      }
      gl.useProgram(this.up.p); gl.enable(gl.BLEND); gl.blendFunc(gl.ONE, gl.ONE);
      for (let i = levels - 2; i >= 0; i--) {
        const b = this.bloom[i], s2 = this.bloom[i + 1]; gl.bindFramebuffer(gl.FRAMEBUFFER, b.fb); gl.viewport(0, 0, b.w, b.h);
        gl.bindTexture(gl.TEXTURE_2D, s2.tex[0]); gl.uniform1i(this.up.u.uSrc, 0); gl.uniform2f(this.up.u.uTexel, 1 / s2.w, 1 / s2.h); gl.uniform1f(this.up.u.uW, 1);
        gl.drawArrays(gl.TRIANGLES, 0, 6);
      }
      gl.disable(gl.BLEND); gl.bindFramebuffer(gl.FRAMEBUFFER, null); gl.viewport(0, 0, this.w, this.h);
      P = this.comp; u = P.u; gl.useProgram(P.p);
      gl.activeTexture(gl.TEXTURE0); gl.bindTexture(gl.TEXTURE_2D, this.main.tex[0]); gl.uniform1i(u.uBase, 0);
      gl.activeTexture(gl.TEXTURE1); gl.bindTexture(gl.TEXTURE_2D, this.main.tex[1]); gl.uniform1i(u.uEmit, 1);
      gl.activeTexture(gl.TEXTURE2); gl.bindTexture(gl.TEXTURE_2D, this.bloom[0].tex[0]); gl.uniform1i(u.uBloom, 2);
      const glow=o.glow&&L.glow;
      gl.uniform1f(u.uBloomK, glow ? 0.42*NS.presentation.foregroundGlowGain : 0);
      gl.uniform1f(u.uEmitK, glow ? 0.30*NS.presentation.foregroundGlowGain : 0.12);
      gl.uniform1f(u.uGlowSaturation,glow?NS.presentation.foregroundGlowSaturation:1);
      gl.uniform1f(u.uGlowCoverage,glow?NS.presentation.foregroundGlowCoverage:0);
      gl.uniform2f(u.uPanelSize,s.W,s.H);gl.uniform2f(u.uOuterMargin,MARGIN,MARGIN);
      gl.uniform1f(u.uOpeningLight,s.T.exit===0?s.T.glowPulse:0);
      gl.uniform1f(u.uTime, s.t); gl.uniform1f(u.uScale, scale);gl.uniform1f(u.uSoloGlow,+soloGlow);
      gl.drawArrays(gl.TRIANGLES, 0, 6);
      // Keep the original glyph buffer. This extra draw does no atlas rebuild.
      if (labels.n && show('info') && !soloGlow) {
        P = this.textP; u = P.u; gl.useProgram(P.p);
        gl.uniform1f(u.uInkPass, 1);
        gl.activeTexture(gl.TEXTURE0); gl.bindTexture(gl.TEXTURE_2D, this.atlasTex);
        gl.enable(gl.BLEND); gl.blendFunc(gl.ONE, gl.ONE_MINUS_SRC_ALPHA);
        gl.bindVertexArray(this.glyphs.vao);
        gl.drawArraysInstanced(gl.TRIANGLES, 0, 6, labels.n);
        gl.disable(gl.BLEND);
      }
      gl.activeTexture(gl.TEXTURE0);
      this.submitFrame();
    }
  }
  NS.Renderer = Renderer; NS.MARGIN = MARGIN;
})(window.RA);
