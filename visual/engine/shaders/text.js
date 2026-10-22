// Text pass. One instanced quad per glyph, SDF atlas, premultiplied MRT output.
// Styles: 0 ink+halo, 1 green, 2 red, 3 yellow (status inks), 4 quota two-tone, 5 micro HUD.
(function (NS) {
  'use strict';
  NS.SH.textVS = `#version 300 es
layout(location=0) in vec2 aC;
layout(location=1) in vec4 iRect; layout(location=2) in vec4 iUV; layout(location=3) in vec4 iCol;
layout(location=4) in vec4 iFx;   layout(location=5) in vec4 iFx2;
layout(location=6) in vec4 iOutput; // outputAmount envelope, continuous flow phase
uniform vec2 uRes, uMargin, uAtlas; uniform float uScale;
out vec2 vUV, vCell, vP; flat out vec4 vCol, vFx, vFx2; flat out vec2 vOutput;
void main(){
  vec2 p = iRect.xy + aC*iRect.zw; vP = p;
  vCell = aC*iUV.zw; vUV = (iUV.xy + vCell)/uAtlas;
  vCol = iCol; vFx = iFx; vFx2 = iFx2;
  vOutput = iOutput.xy;
  vec2 d = (p + uMargin)*uScale;
  gl_Position = vec4(d.x/uRes.x*2. - 1., 1. - d.y/uRes.y*2., 0., 1.);
}`;
  NS.SH.textFS = `#version 300 es
precision highp float;
layout(location=0) out vec4 oBase; layout(location=1) out vec4 oEmit;
uniform sampler2D uTex; uniform vec2 uAtlas, uPanel; uniform float uScale, uTime, uSeaLv;
in vec2 vUV, vCell, vP; flat in vec4 vCol, vFx, vFx2; flat in vec2 vOutput;
const float R = 9., REF = 56., BASE = 66., PI = 3.14159265;
float h21(vec2 p){ p = fract(p*vec2(123.34,456.21)); p += dot(p, p+45.32); return fract(p.x*p.y); }
float vnoise(vec2 p){ vec2 i = floor(p), f = fract(p); f = f*f*(3.-2.*f);
  return mix(mix(h21(i), h21(i+vec2(1,0)), f.x), mix(h21(i+vec2(0,1)), h21(i+vec2(1,1)), f.x), f.y); }
${NS.Flow.seaGLSL}
float sq(float x){ return x*x; }
uniform float uInkPass;
float sd(vec2 uv){ return (texture(uTex, uv).r - .5)*2.*R; }
void over(inout vec4 dst, vec3 c, float a){ a = clamp(a, 0., 1.); dst = vec4(c*a, a) + dst*(1.-a); }
void main(){
  float style = vFx.x, rev = vFx.y, dis = vFx.z, seed = vFx.w, size = vFx2.y;
  float k = size/REF*uScale;
  // Reveal: noise-ordered, out of focus first, then sharp. Never a uniform fade.
  float n = .55*vnoise(vP*.55 + seed*17.) + .45*h21(floor(vP*uScale/1.3) + seed);
  float front = rev*1.35 - .2;
  float vis = smoothstep(n - .12, n + .12, front);
  float ke = k*mix(.16, 1., smoothstep(.1, 1., rev));
  // Dissolve: cells of mixed size break away; their light is the edge glow.
  float cs = mix(1.1, 2.3, step(.6, h21(floor(vP/3.) + seed)));
  float hc = h21(floor(vP/cs) + seed*13.)*.8 + .2*clamp(vCell.x/180., 0., 1.);
  float gone = step(hc, dis*1.15);
  vis *= 1. - gone;
  float burn = exp(-sq((dis*1.15 - hc - .04)/.05))*step(.001, dis) + exp(-sq((front - n)/.07))*step(rev, .995)*step(.01, rev);
  float d = sd(vUV), cov = clamp(d*ke + .5, 0., 1.);
  vec2 off = vec2(0., .8*REF/size)/uAtlas;
  float lip = max(0., clamp(sd(vUV - off)*ke + .5, 0., 1.) - cov);
  float sh = clamp(sd(vUV - off*1.7)*ke*.33 + .5, 0., 1.);
  float halo = sq(clamp((d + 5.)/5., 0., 1.));
  vec3 ink = vCol.rgb, glowC = mix(vCol.rgb, vec3(1.), .45);
  if(style>.5&&style<1.5)glowC=vec3(${NS.statusPalette.green.glow.join(',')});
  else if(style>1.5&&style<2.5)glowC=vec3(${NS.statusPalette.red.glow.join(',')});
  else if(style>2.5&&style<3.5)glowC=vec3(${NS.statusPalette.yellow.glow.join(',')});
  float gy = clamp((vCell.y - (BASE - .74*REF))/(.74*REF), 0., 1.);
  vec4 c = vec4(0.); vec3 em = vec3(0.);
  if (style > 3.5 && style < 4.5 && uSeaLv > .001){
    float under=smoothstep(-.5,.5,vP.y-seaY(vP.x,0.,uSeaLv));
    ink=mix(ink,vec3(.32,.37,.34),under*.68);
  }
  float outputAmount=clamp(vOutput.x,0.,1.);
  float flowX=vP.x+.35*vP.y-vFx2.x;
  float band = exp(-sq(flowX/mix(7.5,10.5,outputAmount)))*vFx2.w;
  float shimmer=1.+outputAmount*${NS.presentation.outputTextShimmer.toFixed(4)}*(.65*sin(vOutput.y*PI*6.+seed*4.3)+.35*sin(vOutput.y*PI*10.+seed*7.1));
  float luma=dot(glowC,vec3(.2126,.7152,.0722));
  vec3 rich=clamp(mix(vec3(luma),glowC,${NS.presentation.outputTextChroma.toFixed(4)})*${NS.presentation.outputTextBrightness.toFixed(4)},0.,1.);
  glowC=mix(glowC,rich,outputAmount*.75);
  float aura=exp(-max(0.,-d)/(R*.45*mix(1.,${NS.presentation.outputTextHaloSpread.toFixed(4)},outputAmount)));
  aura*=mix(1.,smoothstep(-R,-R+1.8,d),outputAmount);
  ink = mix(ink, mix(ink, vec3(1., .99, .96), .5), band*.22);
  over(c, ink, cov*vis);
  em += glowC*aura*vFx2.z*shimmer*vis*(style > 4.5 ? 0. : 1.);
  float shoulder=exp(-sq((flowX+22.)/24.))*.55+exp(-sq((flowX-38.)/15.))*.25;
  em += mix(glowC,vec3(1.,.97,.86),.20)*aura*shoulder*outputAmount*.42*shimmer*vis;
  em += vec3(1., .93, .78)*band*cov*.35*vis;
  em += vec3(1., .82, .55)*burn*clamp(d*k*.5 + 1., 0., 1.)*.9;
  float A = vCol.a;
  oBase = uInkPass > .5 ? c*A : vec4(0.);
  oEmit = uInkPass > .5 ? vec4(0.) : vec4(em*A, 0.);
}`;
})(window.RA);
