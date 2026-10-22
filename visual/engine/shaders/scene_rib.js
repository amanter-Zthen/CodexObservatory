// Scene shader part 4/4: glass-silk ribbons, status orbs, main().
(function (NS) {
  'use strict';
  NS.SH.sceneRib = `
${NS.ribbonArt.paletteGLSL}
void ribbons(vec2 p, inout vec4 c, inout vec4 e){
  float aa = 1./uScale, W = uPanel.x;
  // Ribbons may leave the glass; their light fades with distance, never cut.
  float spill = exp(-max(0., sdRound(p - uPanel*.5, uPanel*.5, 14.))/8.);
  if (spill < .01) return;
  for (int i = 0; i < 12; i++){
    if (i >= uRibN) break;
    vec4 A = uRibA[i], B = uRibB[i], C = uRibC[i];
    if (C.z <= .001) continue;
    float tw, tw2; float y = ribY(i, p.x, tw);
    float y2 = ribY(i, p.x + .75, tw2), dy = (y2 - y)/.75;
    float normalScale = sqrt(1. + dy*dy);
    if (abs(p.y - y) > (B.x + 13.)*normalScale) continue;
    float hw = B.x*(.12 + .88*abs(tw)), dist = (p.y - y)/normalScale;
    float uu = (B.z > 0. ? p.x : W - p.x)/W;
    float vis = smoothstep(C.y, C.y + .10, uu)*(1. - smoothstep(C.x - .14, C.x, uu))*C.z*spill*(1. - .42*B.y);
    if (vis <= .002) continue;
    float blur = .55*aa + B.y*1.7, ed = abs(dist) - hw;
    float cov = 1. - smoothstep(-blur, blur, ed);
    float v = clamp(dist/max(hw, .3), -1., 1.), thin = 1. - abs(tw);
    float fres = pow(abs(v), 3.), fib = .5 + .5*sin(v*11. + p.x*.05 + A.w*.4);
    // Material mid-tones and a small highlight shoulder are separate.
    vec3 surrounding=vec3(.85,.80,.70);
    if(uGlassEnabled>.5&&uLayers.x>.5){
      vec2 source=uGlassCrop.xy+(p/uPanel)*uGlassCrop.zw;
      surrounding=textureLod(uGlassTex,vec2(source.x/uGlassCanvas.x,1.-source.y/uGlassCanvas.y),3.).rgb;
    }
    float var=fbm(vec2(p.x*.013-A.w*.12,B.w+tw*.23));
    float g = pow(max(0., sin(p.x*.017*(1. + .4*h11(B.w)) - A.w*1.7 + B.w)), 9.)*(.35 + .65*C.w);
    float hierarchy=i==0?1.20:i==1?.92:.76;
    vec3 face=mix(ribbonTone(.30+.25*var+.12*thin),surrounding,.22);
    over(c,face,(.06+.19*fres+.08*thin*thin+.025*fib)*cov*vis*hierarchy);
    // Warm return + cream-lit edge: local contrast carries the sense of light.
    float lip = exp(-sq((ed + .35)/(.45 + blur)));
    over(c,ribbonTone(.03+.12*var),lip*step(0.,dist)*.38*vis*hierarchy);
    over(c,ribbonTone(.88+.10*g+.02*thin),lip*step(dist,0.)*(.55+.18*g)*vis*hierarchy);
    e.rgb+=ribbonTone(.54)*lip*step(dist,0.)*(.32+.32*g)*vis*hierarchy;
    // Flowing light has a coloured body. Isolated energy peaks approach warm
    // white; direct RGB addition no longer clips the whole sheet to white.
    float spec = (g*exp(-sq((v + .55)/.32)) + .75*pow(thin, 7.))*cov*vis;
    float peak=ribbonShoulder((g+pow(thin,7.))*.95);
    vec3 core=ribbonTone(.82+.18*peak);
    over(c,core,ribbonShoulder(spec*1.50)*hierarchy);
    e.rgb += ribbonTone(.55+.28*peak)*spec*(.45+.80*C.w)*${NS.ribbonArt.P.emissionGain.toFixed(4)}*hierarchy;
    // Entering head carries light; the leaving tail sheds glitter.
    float head = exp(-sq((uu - C.x)/.035))*step(C.x, 1.25)*cov;
    e.rgb += ribbonTone(.86)*head*.8*C.z*spill;
    if (i < 2 && uQuality < 1.5 && abs(dist) < hw + 9.){
      for (int j = -1; j <= 1; j++){
        float ci = floor(p.x/14.) + float(j), hh = h21(vec2(ci, B.w));
        float sx = (ci + .2 + .6*hh)*14., sy = y + dy*(sx - p.x) - hw*(.6 + .4*h11(ci + B.w));
        float fl = pow(max(0., sin(uTime*(1.1 + 2.2*hh) + hh*40.)), 16.)*(.35 + .65*C.w + uS4.y);
        vec2 d = p - vec2(sx, sy);
        float st = exp(-dot(d, d)/.5) + .45*exp(-abs(d.y)*2.4 - abs(d.x)*.32) + .35*exp(-abs(d.x)*2.4 - abs(d.y)*.45);
        float detail=.75+.12*uRibDetail;
        over(c,ribbonTone(.98),ribbonShoulder(st*fl*.65)*vis*detail);
        e.rgb += ribbonTone(.80)*st*fl*.72*vis*detail;
      }
    }
    if (i < 2){
      float ty = y + (hw + 6. + 2.*sin(p.x*.02 + uTime*.5))*(i == 0 ? 1. : -1.);
      float cx = mod(p.x + (i == 0 ? -1. : 1.)*uTime*9., 4.2) - 2.1, wv = pow(.5 + .5*sin(p.x*.028 - uTime*1.3 + float(i)*2.), 3.);
      float dd = length(vec2(cx, p.y - ty)), dot_ = 1. - smoothstep(.25 + .45*wv, .55 + .45*wv + aa, dd);
      float detail=.75+.12*uRibDetail;
      over(c,ribbonTone(.12),dot_*wv*.30*vis*detail);
      e.rgb += ribbonTone(.70)*dot_*wv*.10*vis*detail;
    }
  }
}
void orbs(vec2 p, inout vec4 c, inout vec4 e){
  float aa = 1./uScale;
  for (int i = 0; i < 16; i++){
    if (i >= uOrbN) break;
    vec4 A = uOrbA[i], B = uOrbB[i], R=uRowA[i];
    if (A.w <= .002) continue;
    vec2 o = A.xy; vec2 q = p - o; float d = length(q), r = 2.05 + .6*B.w/17.;
    if (d > 16.) continue;
    float a = A.w, flash = a*(1. - a)*4.;
    vec3 lo, glow; float gk;
    if (A.z < .5){
      lo = vec3(${NS.statusPalette.green.core.join(',')});
      glow = vec3(${NS.statusPalette.green.glow.join(',')});
      // A steady green light with a bounded breath above its visible floor.
      float breath = sin(uTime*TAU/${NS.presentation.lampBreathPeriod.toFixed(4)} + B.z*6.);
      gk = ${NS.presentation.lampLightBase.toFixed(4)}
         + ${NS.presentation.lampBreathAmplitude.toFixed(4)}*breath
         + ${NS.presentation.lampOutputBoost.toFixed(4)}*R.z;
    }
    else if (A.z < 1.5){ float pu = .5 + .5*sin(uTime*3.3 + B.z*6.); lo = vec3(${NS.statusPalette.red.core.join(',')}); glow = vec3(${NS.statusPalette.red.glow.join(',')}); gk = .42 + .22*pu; }
    else { float br = .5 - .5*cos(uTime*TAU/2.8 + B.z*6.); lo = vec3(${NS.statusPalette.yellow.core.join(',')}); glow = vec3(${NS.statusPalette.yellow.glow.join(',')}); gk = .24 + .22*br; }
    float core = 1. - smoothstep(r - aa, r + aa, d);
    over(c, lo, core*a);
    // Uniform light across the flat disc; a soft falloff starts at its edge.
    float halo = exp(-sq(max(0.,d-r)/(r*1.75)));
    e.rgb += glow*halo*(gk + flash)*a;
    // Integrated phase accelerates with output, without a time*speed jump.
    float ang=atan(q.y,q.x),spin=line(abs(d-r-3.),.65,aa)*smoothstep(.25,.85,sin((ang-R.w)*3.));
    float spinAlpha=(A.z<.5?.20:.06)+.68*R.z;
    over(c,lo,spin*spinAlpha*a);e.rgb+=glow*spin*(.035+.40*R.z)*a;
    if (A.z > .5 && A.z < 1.5){
      float rr = fract(uTime*.7 + B.z), ring = line(abs(d - r - 1.5 - 9.*rr), .8, aa)*(1. - rr)*(1. - rr);
      e.rgb += glow*ring*.55*a; over(c, lo, ring*.25*a);
    }
  }
}
// Restore the original broad entrance light and its warm radiance. A brief
// gathering motion and a feathered footprint leave room before the host edge.
float transitionEase(float p){
  p=clamp(p,0.,1.);
  return p*p*p*(p*(p*6.-15.)+10.);
}
float transitionLight(vec2 p){
  if(uS4.w>0. || uS2.w<=0.)return 0.;
  float seconds=uS4.z*${NS.STAGE.ENTER_DUR.toFixed(4)};
  float gather=transitionEase(seconds/${NS.presentation.transitionGlowGather.toFixed(4)});
  float release=1.-smoothstep(.82,1.,uS4.z/.62);
  vec2 radius=vec2(120.+120.*uS1.x,40.+40.*uS1.x)*mix(.22,1.,gather);
  vec2 g=(p-uPanel*.5)/radius;
  // Keep the original broad source; the enlarged composite owns its outer tail.
  return exp(-dot(g,g))*uS2.w*.8*gather*release*max(uLayers.x,uLayers.w);
}
void main(){
  vec2 p = vec2(gl_FragCoord.x, uRes.y - gl_FragCoord.y)/uScale - uMargin;
  oBase = vec4(0.); oEmit = vec4(0.);
  if (p.y > uPanel.y + uMargin.y - 2.) return;
  vec4 c = vec4(0.), e = vec4(0.);
  float aa = 1./uScale, d = sdRound(p - uPanel*.5, uPanel*.5, 14.);
  float inside = 1. - smoothstep(-aa*.5, aa*.5, d), burn, sv = slabVis(p, burn), pa = inside*sv;
  float shown = uS1.y*(1. - uS3.w);
  float sh = exp(-max(0., sdRound(p - uPanel*.5 - vec2(0., 5.), uPanel*.5, 14.))/9.)*(1. - inside);
  over(c, vec3(.30, .25, .18), sh*.14*shown*uLayers.w);
  if (pa > 0.){
    if(uLayers.x > .5){ body(p, d, pa, c); if(uGlassEnabled < .5) sea(p, pa, c, e); }
    if(uLayers.z > .5) bokeh(p, pa, c, e);
    if(max(uLayers.w,uInfo) > .5) hud(p, pa, c, e);
  }
  if(uLayers.y > .5) ribbons(p, c, e);
  if(uLayers.w > .5) identityLines(p,c,e);
  if(uInfo > .5) orbs(p, c, e);
  if (uLayers.w > .5 && pa > 0. && d > -9.){
    vec2 ex = vec2(.5, 0.), ey = vec2(0., .5);
    vec2 n = normalize(vec2(sdRound(p + ex - uPanel*.5, uPanel*.5, 14.) - sdRound(p - ex - uPanel*.5, uPanel*.5, 14.),
                            sdRound(p + ey - uPanel*.5, uPanel*.5, 14.) - sdRound(p - ey - uPanel*.5, uPanel*.5, 14.)) + 1e-5);
    rim(p, d, n, pa, c, e);
  }
  float glow = transitionLight(p);
  addLight(c, vec3(1., .95, .86)*glow*.35); e.rgb += vec3(1., .88, .68)*glow*.9;
  e.rgb += vec3(1., .80, .52)*burn*inside*1.3*max(uLayers.x,uLayers.w);
  addLight(c, vec3(1., .93, .80)*burn*inside*.35*max(uLayers.x,uLayers.w));
  oBase = c; oEmit = vec4(e.rgb, 0.);
}
`;
})(window.RA);
