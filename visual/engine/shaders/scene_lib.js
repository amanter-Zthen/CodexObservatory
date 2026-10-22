// Scene shader part 1/4: interface + shared functions.
// Coordinates: p = panel-local DIP, y down (same as Scene/layout in Go).
(function (NS) {
  'use strict';
  NS.SH = NS.SH || {};
  NS.SH.sceneLib = `#version 300 es
precision highp float;
layout(location=0) out vec4 oBase;
layout(location=1) out vec4 oEmit;
uniform vec2 uRes; uniform float uScale; uniform vec2 uMargin; uniform vec2 uPanel;
uniform float uTime, uBreathe, uEnergy, uQuality, uDrift, uRowsA, uSynth;
uniform vec4 uS1; // glowIn slabIn frameIn ringIn
uniform vec4 uS2; // headIn quotaIn detailIn glowPulse
uniform vec4 uS3; // textOut frameOut ringOut slabOut
uniform vec4 uS4; // glowOut snake enter exit
uniform vec4 uQuota; // cx cy level flags(1=known,2=fresh)
uniform float uRingR; uniform vec2 uQuotaArc; uniform float uRibD[12];
uniform vec4 uRibA[12]; uniform vec4 uRibB[12]; uniform vec4 uRibC[12]; uniform int uRibN;
uniform vec4 uOrbA[16]; uniform vec4 uOrbB[16]; uniform vec4 uRowA[16]; uniform int uOrbN;
uniform vec3 uStatus;
uniform sampler2D uGlassTex;
uniform vec2 uGlassCanvas;
uniform vec4 uGlassCrop, uLayers; // background / curves / particles / frame
uniform float uGlassEnabled, uInfo;
uniform vec2 uGlassFocus; // central clear band / maximum radius in panel DIP
uniform float uRibDetail;
uniform vec3 uIdentity; // header vertical scale, header rightward reach, footer leftward reach

float sq(float x){ return x*x; }
const float PI = 3.14159265, TAU = 6.2831853;
float h11(float n){ return fract(sin(n*127.1+311.7)*43758.5453); }
float h21(vec2 p){ p = fract(p*vec2(123.34,456.21)); p += dot(p, p+45.32); return fract(p.x*p.y); }
float vnoise(vec2 p){ vec2 i = floor(p), f = fract(p); f = f*f*(3.-2.*f);
  return mix(mix(h21(i), h21(i+vec2(1,0)), f.x), mix(h21(i+vec2(0,1)), h21(i+vec2(1,1)), f.x), f.y); }
float fbm(vec2 p){ float a = .5, s = 0.; for (int i = 0; i < 4; i++){ s += a*vnoise(p); p = p*2.03+vec2(1.7,9.2); a *= .5; } return s; }
float sdRound(vec2 p, vec2 b, float r){ vec2 q = abs(p)-b+r; return length(max(q,0.))+min(max(q.x,q.y),0.)-r; }
float line(float d, float w, float aa){ return 1.-smoothstep(w*.5-aa*.5, w*.5+aa*.5, d); }
void over(inout vec4 dst, vec3 c, float a){ a = clamp(a, 0., 1.); dst = vec4(c*a, a) + dst*(1.-a); }
// Additive light that also works over transparent desktop pixels.
void addLight(inout vec4 dst, vec3 c){ dst.rgb += c; dst.a = max(dst.a, min(1., max(c.r, max(c.g, c.b)))); }

// Quota sea: three stacked surfaces, each a sum of travelling waves with
// different wavelengths/speeds, so the surface breathes instead of sliding.
${NS.Flow.seaGLSL}
// Ribbon centre line + twist. Wave sum with a slow along-length envelope:
// carriers move at different rates, so the silhouette morphs every frame.
${NS.Flow.ribbonGLSL}
`;
})(window.RA);
