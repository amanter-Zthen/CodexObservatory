// Scene shader part 3/4: HUD hairlines, instrument rings, brackets, gold tabs.
// Header line y=22, rows 30..H-20, footer text at H-7 (labels.js uses the same).
(function (NS) {
  'use strict';
  NS.SH.sceneHud = `
// Lines break into irregular, growing dashes instead of fading uniformly.
float dashOut(float s, float k){
  if (k <= 0.) return 1.;
  float seg = floor(s/6.3);
  return step(k*1.08, h11(seg*3.7 + 1.3)*.92 + .08) * step(k*.55, fract(s/6.3 + h11(seg)*.4));
}
void goldTab(vec2 p, vec2 o, float s, float a, inout vec4 c, inout vec4 e){
  vec2 q = p - o; float d = max(abs(q.x), abs(q.y)) - s*.5;
  float m = 1. - smoothstep(-.5/uScale, .5/uScale, d);
  if (m <= 0.) return;
  vec3 g = mix(vec3(.78, .63, .42), vec3(.99, .93, .80), clamp(.5 - (q.x + q.y)/(s*1.4), 0., 1.));
  over(c, g, m*a*.92);
  e.rgb += vec3(1., .82, .55)*m*a*(.05 + .05*sin(uTime*1.3));
}
void ring(vec2 p, vec2 o, float R, float a, float inP, float outP, float startP, bool fine, inout vec4 c, inout vec4 e){
  vec2 q = p - o; float rr = length(q);
  if (abs(rr - R) > 8.) return;
  float aa = 1./uScale, u = fract(atan(q.x, -q.y)/TAU + 1.), arc = u*TAU*R;
  if (inP <= startP) return;
  float vis = step(startP, u)*step(u, inP)*dashOut(arc, outP)*a;
  if (vis <= 0.) return;
  vec3 ink = vec3(.36, .35, .32);
  over(c, ink, line(abs(rr - R), .7, aa)*.26*vis);
  if (!fine) return;
  float tk = u*60., ti = floor(tk + .5), da = abs(tk - ti)*TAU*R/60.;
  float len = mod(ti, 5.) < .5 ? 3.6 : 1.5;
  over(c, ink, line(da, .6, aa)*step(R + 2., rr)*step(rr, R + 2. + len)*.30*vis);
  float v = fract(u - uTime*.035);
  if (v < .34){
    float sp = .0105, j = v/sp, local = (fract(j) - .5)*sp*TAU*(R - 5.);
    float dd = length(vec2(local, rr - (R - 5.)));
    over(c, vec3(.55, .50, .42), (1. - smoothstep(.35, .75, dd))*sin(PI*v/.34)*.55*vis);
  }
  float g = uTime*.42;
  vec2 gp = o + R*vec2(sin(g), -cos(g));
  float gl = exp(-dot(p - gp, p - gp)/2.2);
  addLight(c, vec3(1., .94, .82)*gl*.55*vis); e.rgb += vec3(1., .86, .62)*gl*1.1*vis;
}
// Row-local guides share their endpoints and expansion with the glyph layout.
void rowGuides(vec2 p, float a, inout vec4 c, inout vec4 e){
  if(a <= .001) return;
  float aa=1./uScale;
  for(int i=0;i<16;i++){
    if(i>=uOrbN) break;
    vec4 A=uOrbA[i],B=uOrbB[i],R=uRowA[i];
    // Retraction changes geometry. The remaining line keeps its ink density.
    float hy=A.y+B.w*.36+4.8,vis=A.w*a;
    if(R.z<=.000001||vis<=.001||abs(p.y-hy)>4.5||p.x<R.x-1.5||p.x>R.y+1.5) continue;
    float s=p.x-R.x,span=max(1.,R.y-R.x),v=vis*dashOut(s,B.y);
    float onLine=step(R.x,p.x)*step(p.x,R.y);
    over(c,vec3(.32,.31,.29),line(abs(p.y-hy),.55,aa)*.19*v*onLine);
    float tick=floor(p.x/8.+.5),dx=abs(p.x-tick*8.),len=mod(tick,4.)<.5?2.4:1.2;
    over(c,vec3(.40,.37,.31),line(dx,.5,aa)*step(hy,p.y)*step(p.y,hy+len)*.22*v*onLine);
    float pulse=exp(-sq((fract(s/span-uTime*.65)-.5)/.10))*B.x;
    e.rgb+=vec3(1.,.88,.66)*line(abs(p.y-hy),1.2,aa)*pulse*.28*v*onLine;
    float capSize=1.1*smoothstep(0.,.035,R.z);
    float capX=min(abs(p.x-R.x),abs(p.x-R.y));
    float cap=1.-smoothstep(capSize-.25,capSize+.25,max(capX,abs(p.y-hy)));
    over(c,vec3(.32,.31,.29),cap*.45*v);
  }
}
// Identity furniture follows its own geometric clips, alongside the flying
// glyphs, while the glass curtain dissolves underneath.
void identityLines(vec2 p,inout vec4 c,inout vec4 e){
  float aa=1./uScale,W=uPanel.x,H=uPanel.y;
  vec3 ink=vec3(.30,.30,.28);
  if(uIdentity.x>.0001&&p.x>=14.&&p.x<=30.){
    float height=1.5*uIdentity.x,bottom=22.75;
    float rect=max(abs(p.x-22.)-8.,abs(p.y-(bottom-height*.5))-height*.5);
    over(c,ink,(1.-smoothstep(-aa*.5,aa*.5,rect))*.70);
  }
  if(uIdentity.y>.0001){
    float tip=30.+(W-14.5-30.)*uIdentity.y;
    float vis=step(30.,p.x)*step(p.x,tip);
    over(c,ink,line(abs(p.y-22.),.65,aa)*.26*vis);
    goldTab(p,vec2(tip,22.),3.*smoothstep(0.,.08,uIdentity.y),1.,c,e);
  }
  if(uIdentity.z>.0001){
    float right=W-19.,left=right-10.*uIdentity.z;
    float rect=max(abs(p.x-(left+right)*.5)-(right-left)*.5,abs(p.y-(H-9.5))-.60);
    over(c,ink,(1.-smoothstep(-aa*.5,aa*.5,rect))*.75);
  }
}
void hud(vec2 p, float a, inout vec4 c, inout vec4 e){
  if (a <= 0.) return;
  a *= uLayers.w;
  float aa = 1./uScale, W = uPanel.x, H = uPanel.y;
  vec3 ink = vec3(.30, .30, .28);
  float fo = uS3.y;
  rowGuides(p,a,c,e);
  // One quota instrument. The large decorative ring behind model rows is removed.
  ring(p, uQuota.xy, uRingR, a*uS2.y, uQuotaArc.x, uS3.z, uQuotaArc.y, true, c, e);
}
`;
})(window.RA);
