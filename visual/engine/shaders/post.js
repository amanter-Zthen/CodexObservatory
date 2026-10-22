// Particles (shards / glitter / glints), bloom chain and final composite.
(function (NS) {
  'use strict';
  NS.SH.partVS = `#version 300 es
layout(location=0) in vec2 aC;
layout(location=1) in vec4 iA; // x y size rot
layout(location=2) in vec4 iB; // r g b alpha
layout(location=3) in vec4 iK; // kind glow seed aspect
uniform vec2 uRes, uMargin; uniform float uScale;
out vec2 vQ; flat out vec4 vB, vK;
void main(){
  vec2 q = (aC - .5)*2.; vQ = q;
  float s = sin(iA.w), c = cos(iA.w);
  vec2 off = vec2(q.x*iK.w, q.y)*iA.z*(iK.x > 1.5 ? 4. : 1.);
  vec2 p = iA.xy + vec2(c*off.x - s*off.y, s*off.x + c*off.y);
  vB = iB; vK = iK;
  vec2 d = (p + uMargin)*uScale;
  gl_Position = vec4(d.x/uRes.x*2. - 1., 1. - d.y/uRes.y*2., 0., 1.);
}`;
  NS.SH.partFS = `#version 300 es
precision highp float;
layout(location=0) out vec4 oBase; layout(location=1) out vec4 oEmit;
in vec2 vQ; flat in vec4 vB, vK;
void main(){
  float a = vB.a; vec3 col = vB.rgb, em = vec3(0.); float cov = 0.;
  if (vK.x < .5){ // soft glitter dot
    float r2 = dot(vQ, vQ); cov = exp(-r2*3.2); em = col*cov*vK.y;
  } else if (vK.x < 1.5){ // faceted shard: two-tone square with a lit edge
    float m = max(abs(vQ.x), abs(vQ.y)); cov = 1. - smoothstep(.78, 1., m);
    float lit = step(vQ.x + vQ.y, 0.);
    col = mix(col*.82, mix(col, vec3(1.), .55), lit);
    em = vec3(1., .86, .6)*cov*vK.y*(.4 + .6*lit);
  } else { // four-point glint
    vec2 q = vQ*4.; float r2 = dot(q, q);
    cov = exp(-r2*4.) + .55*exp(-abs(q.y)*7. - abs(q.x)*1.1) + .55*exp(-abs(q.x)*7. - abs(q.y)*1.1);
    em = col*cov*vK.y; cov *= .5;
  }
  float alpha = clamp(cov*a, 0., 1.);
  oBase = vec4(col*alpha, alpha); oEmit = vec4(em*a, 0.);
}`;
  NS.SH.downFS = `#version 300 es
precision highp float; in vec2 vUV; out vec4 o; uniform sampler2D uSrc; uniform vec2 uTexel;
void main(){ vec2 d = uTexel;
  vec3 c = texture(uSrc, vUV).rgb*4. + texture(uSrc, vUV + vec2(-d.x,-d.y)).rgb + texture(uSrc, vUV + vec2(d.x,-d.y)).rgb
         + texture(uSrc, vUV + vec2(-d.x,d.y)).rgb + texture(uSrc, vUV + vec2(d.x,d.y)).rgb;
  o = vec4(c/8., 1.); }`;
  NS.SH.upFS = `#version 300 es
precision highp float; in vec2 vUV; out vec4 o; uniform sampler2D uSrc; uniform vec2 uTexel; uniform float uW;
void main(){ vec2 d = uTexel; vec3 c = vec3(0.);
  c += texture(uSrc, vUV + vec2(-d.x, 0.)).rgb*2. + texture(uSrc, vUV + vec2(d.x, 0.)).rgb*2.
     + texture(uSrc, vUV + vec2(0., -d.y)).rgb*2. + texture(uSrc, vUV + vec2(0., d.y)).rgb*2.
     + texture(uSrc, vUV + vec2(-d.x, -d.y)).rgb + texture(uSrc, vUV + vec2(d.x, -d.y)).rgb
     + texture(uSrc, vUV + vec2(-d.x, d.y)).rgb + texture(uSrc, vUV + vec2(d.x, d.y)).rgb + texture(uSrc, vUV).rgb*4.;
  o = vec4(c/16.*uW, 1.); }`;
  // Bounded, monotonic radiance over a preserved premultiplied base.
  // Readable ink is composited after this pass. Only emitters create bloom.
  NS.SH.compFS = `#version 300 es
precision highp float; in vec2 vUV; out vec4 o;
uniform sampler2D uBase, uEmit, uBloom;
uniform float uBloomK, uEmitK, uTime, uScale, uSoloGlow;
uniform float uGlowSaturation, uGlowCoverage;
uniform vec2 uPanelSize, uOuterMargin;
uniform float uOpeningLight;
void main(){
  vec4 b = texture(uBase, vUV);
  float baseA = clamp(b.a, 0., 1.);
  vec3 base = clamp(b.rgb, vec3(0.), vec3(baseA));
  vec3 L = max(vec3(0.), texture(uBloom, vUV).rgb*uBloomK
                       + texture(uEmit, vUV).rgb*uEmitK
                       + max(b.rgb - vec3(baseA), vec3(0.)));
  float luminance = dot(L,vec3(.2126,.7152,.0722));
  L = max(vec3(0.),mix(vec3(luminance),L,uGlowSaturation));
  if(uSoloGlow > .5){ baseA=0.; base=vec3(0.); }
  float peak = max(L.r, max(L.g, L.b));
  float lightA = 1. - exp(-peak*.85);
  vec3 tint = peak > .00001 ? L/peak : vec3(0.);
  float a = baseA + (1. - baseA)*lightA;
  vec3 c = base + (vec3(1.)-base)*tint*lightA;
  c = mix(c,tint*a,uGlowCoverage*lightA);
  vec2 res = vec2(textureSize(uBase, 0));
  float edge = min(min(gl_FragCoord.x, gl_FragCoord.y), min(res.x-gl_FragCoord.x, res.y-gl_FragCoord.y));
  float support = smoothstep(0., min(uOuterMargin.x,uOuterMargin.y)*.30*uScale, edge);
  if(uOpeningLight>0.){
    vec2 point=vec2(gl_FragCoord.x,res.y-gl_FragCoord.y)/uScale-uOuterMargin;
    // A long C2-continuous tail begins at the panel and settles before the window.
    // Preserve the original source shape instead of stacking two ellipse masks.
    vec2 q=abs(point-uPanelSize*.5)-uPanelSize*.5+vec2(14.);
    float outside=length(max(q,0.))+min(max(q.x,q.y),0.)-14.;
    float tail=clamp(max(outside,0.)/${NS.presentation.transitionGlowEdgeFeather.toFixed(4)},0.,1.);
    float settle=tail*tail*tail*(tail*(tail*6.-15.)+10.);
    support*=1.-settle;
  }
  float n = fract(sin(dot(gl_FragCoord.xy, vec2(12.9898,78.233)))*43758.5453);
  c = clamp(c + (n-.5)*a/255., vec3(0.), vec3(a));
  o = vec4(c,a)*support;
}`;
})(window.RA);
