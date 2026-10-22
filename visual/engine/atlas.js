// Glyph SDF atlas. The browser supplies the coverage mask here; the native host
// will supply the same mask from GDI+. The distance transform is identical.
// Per-glyph (not per-label) entries so rate digits never rebuild textures and
// every letter can animate on its own.
(function (NS) {
  'use strict';
  const REF = 56, PAD = 10, R = 9, AW = 2048, AH = 2048;
  const FONTS = {
    name: '700 56px "Segoe UI","Microsoft YaHei UI",sans-serif',
    rate: '700 56px "Segoe UI",sans-serif',
    quota: '700 56px "Segoe UI",sans-serif',
    hud: '700 56px "Segoe UI",sans-serif',
    hudl: '400 56px "Segoe UI",sans-serif',
    cjk: '400 56px "Segoe UI","Microsoft YaHei UI",sans-serif',
  };
  const INF = 1e20;

  function edt1d(f, d, v, z, n) {
    v[0] = 0; z[0] = -INF; z[1] = INF;
    for (let q = 1, k = 0; q < n; q++) {
      let s = (f[q] + q * q - (f[v[k]] + v[k] * v[k])) / (2 * q - 2 * v[k]);
      while (s <= z[k]) { k--; s = (f[q] + q * q - (f[v[k]] + v[k] * v[k])) / (2 * q - 2 * v[k]); }
      k++; v[k] = q; z[k] = s; z[k + 1] = INF;
    }
    for (let q = 0, k = 0; q < n; q++) { while (z[k + 1] < q) k++; const r = q - v[k]; d[q] = r * r + f[v[k]]; }
  }
  function edt(g, w, h, f, d, v, z) {
    for (let x = 0; x < w; x++) { for (let y = 0; y < h; y++) f[y] = g[y * w + x]; edt1d(f, d, v, z, h); for (let y = 0; y < h; y++) g[y * w + x] = d[y]; }
    for (let y = 0; y < h; y++) { for (let x = 0; x < w; x++) f[x] = g[y * w + x]; edt1d(f, d, v, z, w); for (let x = 0; x < w; x++) g[y * w + x] = d[x]; }
  }

  class Atlas {
    constructor() {
      this.cv = document.createElement('canvas'); this.cv.width = 256; this.cv.height = 128;
      this.ctx = this.cv.getContext('2d', { willReadFrequently: true });
      this.map = new Map(); this.dirty = []; this.sx = 0; this.sy = 0; this.rowH = 0;
      this.widths=new Map();this.requested=new Map();this.generation=0;this.built=0;
      const n = 256; this.f = new Float64Array(n); this.d = new Float64Array(n); this.v = new Int32Array(n); this.z = new Float64Array(n + 1);
    }
    font(key, ch) { return /[　-鿿＀-￯]/.test(ch) ? FONTS.cjk : FONTS[key] || FONTS.name; }
    advance(key,ch){
      const id=key+'\u0000'+ch;if(this.widths.has(id))return this.widths.get(id);
      this.ctx.font=this.font(key,ch);const width=this.ctx.measureText(ch).width;
      if(this.widths.size>=4096)this.widths.clear();this.widths.set(id,width);return width;
    }
    want(key,ch){this.requested.set(key+'\u0000'+ch,[key,ch]);}
    prepare(budgetMs=Infinity){
      const fits=reset=>{
        let x=reset?0:this.sx,y=reset?0:this.sy,rh=reset?0:this.rowH;
        for(const[id,[key,ch]]of this.requested){
          if(!reset&&this.map.has(id))continue;
          const w=Math.ceil(this.advance(key,ch))+2*PAD+4,h=Math.ceil(REF*1.32)+2*PAD;
          if(x+w>AW){x=0;y+=rh;rh=0;}if(y+h>AH)return false;x+=w;rh=Math.max(rh,h);
        }return true;
      };
      if(!fits(false)){
        if(!fits(true))throw Error('Visible glyph budget exceeded');
        this.map.clear();this.dirty.length=0;this.sx=this.sy=this.rowH=0;this.generation++;
      }
      const end=performance.now()+budgetMs;
      for(const[id,[key,ch]]of this.requested){
        if(!this.map.has(id)){if(performance.now()>=end)return false;this.get(key,ch);}
      }return true;
    }
    get(key, ch) {
      const id = key + '\u0000' + ch; let g = this.map.get(id);
      if (!g) { g = this.build(key, ch); this.map.set(id, g); }
      return g;
    }
    build(key, ch) {
      const c = this.ctx; c.font = this.font(key, ch);
      const adv = c.measureText(ch).width, w = Math.ceil(adv) + 2 * PAD + 4, h = Math.ceil(REF * 1.32) + 2 * PAD, base = PAD + Math.round(REF * 1.0);
      if (this.sx + w > AW) { this.sx = 0; this.sy += this.rowH; this.rowH = 0; }
      if (this.sy + h > AH) throw Error('Prepare the glyph atlas before building a frame');
      this.built++;
      const gx = this.sx, gy = this.sy; this.sx += w; this.rowH = Math.max(this.rowH, h);
      c.clearRect(0, 0, w, h); c.fillStyle = '#fff'; c.textBaseline = 'alphabetic'; c.fillText(ch, PAD + 2, base);
      const img = c.getImageData(0, 0, w, h).data, n = w * h;
      const outer = new Float64Array(n), inner = new Float64Array(n);
      for (let i = 0; i < n; i++) {
        const a = img[i * 4 + 3] / 255;
        if (a <= 0) { outer[i] = INF; inner[i] = 0; } else if (a >= 1) { outer[i] = 0; inner[i] = INF; }
        else { const e = 0.5 - a; outer[i] = e > 0 ? e * e : 0; inner[i] = e < 0 ? e * e : 0; }
      }
      edt(outer, w, h, this.f, this.d, this.v, this.z); edt(inner, w, h, this.f, this.d, this.v, this.z);
      const data = new Uint8Array(n);
      for (let i = 0; i < n; i++) { const sd = Math.sqrt(inner[i]) - Math.sqrt(outer[i]); data[i] = Math.max(0, Math.min(255, Math.round(255 * (0.5 + sd / (2 * R))))); }
      this.dirty.push({ x: gx, y: gy, w, h, data });
      return { x: gx, y: gy, w, h, adv, base, ox: PAD + 2 };
    }
    // Width in DIP for layout (right alignment, centring).
    measure(key, text, size, track) {
      let a = 0; for (const ch of text) a += this.advance(key, ch);
      return a * size / REF + Math.max(0, [...text].length - 1) * (track || 0);
    }
  }
  NS.Atlas = Atlas; NS.ATLAS = { REF, PAD, R, AW, AH };
})(window.RA);
