// Scene shader part 2/4: glass slab, quota sea, bokeh.
(function (NS) {
  'use strict';
  NS.SH.sceneLayers = `
// Boot: radial, noise-feathered reveal from the centre.
// Exit: mixed-size cells break from top-right toward bottom-left.
float slabVis(vec2 p, out float burn){
  vec2 cc = uPanel*.5;
  float r = length((p-cc)/vec2(uPanel.x*.56, uPanel.y*.62 + 46.));
  float inF = uS1.y*1.35 - (r*.85 + fbm(p*.035 + 3.1)*.38);
  float vin = smoothstep(0., .10, inF);
  float bIn = exp(-sq((inF - .04)/.05))*step(.001, uS1.y)*step(uS1.y, .999);
  float cs = mix(3.5, 8.5, step(.55, vnoise(p*.018 + 7.)));
  vec2 cell = floor(p/cs);
  float sweep = .60*(1. - p.x/uPanel.x) + .22*(p.y/uPanel.y) + .30*h21(cell) + .12*fbm(p*.05);
  float f = uS3.w*1.32 - sweep - .002;
  float vout = 1. - step(0., f);
  float bOut = exp(-sq((f + .035)/.035))*step(.001, uS3.w)*step(uS3.w, .999);
  burn = max(bIn, bOut);
  return vin*vout;
}
// An art-directed focus plane over the complete BG16 image. The existing
// geometric depth-of-field remains in BG16; here blur grows above/below the
// central subject. Adjacent colour and light spread across silhouettes before
// the rounded window crop. Geometry and coverage remain intact.
vec3 glassFocus(vec2 uv, vec2 p){
  float edge=smoothstep(uGlassFocus.x,1.02,abs(p.y/uPanel.y-.5)*2.);
  float radius=edge*uGlassFocus.y*uGlassCrop.z/uPanel.x;
  if(radius<.05)return textureLod(uGlassTex,uv,0.).rgb;
  float lod=log2(max(1.,radius*.72));
  vec2 tap=vec2(radius*.36)/uGlassCanvas;
  vec3 rgb=pow(textureLod(uGlassTex,uv,lod).rgb,vec3(2.2))*.40;
  rgb+=pow(textureLod(uGlassTex,uv+tap*vec2(-1.,-1.),lod).rgb,vec3(2.2))*.15;
  rgb+=pow(textureLod(uGlassTex,uv+tap*vec2( 1.,-1.),lod).rgb,vec3(2.2))*.15;
  rgb+=pow(textureLod(uGlassTex,uv+tap*vec2(-1., 1.),lod).rgb,vec3(2.2))*.15;
  rgb+=pow(textureLod(uGlassTex,uv+tap*vec2( 1., 1.),lod).rgb,vec3(2.2))*.15;
  return pow(max(rgb,vec3(0.)),vec3(1./2.2));
}
void body(vec2 p, float d, float a, inout vec4 c){
  if(uGlassEnabled > .5){
    vec2 source = uGlassCrop.xy + (p/uPanel)*uGlassCrop.zw;
    vec2 uv = vec2(source.x/uGlassCanvas.x, 1.-source.y/uGlassCanvas.y);
    over(c, glassFocus(uv,p), a*.958);
    return;
  }
  vec2 u = p/uPanel;
  vec3 col = mix(vec3(1., .997, .988), vec3(.972, .957, .925), smoothstep(0., 1., u.y));
  vec2 q1 = (u - vec2(.02, 1.08))*vec2(1.25, 1.8), q2 = (u - vec2(1.02, .96))*vec2(1.55, 1.9);
  col = mix(col, vec3(.962, .902, .826), .34*exp(-dot(q1, q1)));
  col = mix(col, vec3(.905, .937, .918), .22*exp(-dot(q2, q2)));
  // Wide transmitted light band, bottom-left to top-right, drifting very slowly.
  float band = exp(-sq((p.x*.55 + (uPanel.y - p.y) - 170. - 16.*sin(uTime*.19))/56.));
  col = mix(col, vec3(1., .985, .955), .55*band);
  col *= 1. - .03*smoothstep(-18., -2., d);
  col += (h21(floor(p*uScale) + fract(uTime)*7.) - .5)*.005;
  over(c, col, a*.958);
}
void rim(vec2 p, float d, vec2 n, float a, inout vec4 c, inout vec4 e){
  if (a <= 0. || d > 1.5 || d < -9.) return;
  float aa = 1./uScale;
  float lit = dot(n, normalize(vec2(-.5, -.86)))*.5 + .5;
  over(c, mix(vec3(.60, .55, .47), vec3(.82, .79, .73), lit), line(abs(d + .45), .85, aa)*.62*a);
  over(c, vec3(1., .996, .978), line(abs(d + 1.65), .8, aa)*(.30 + .62*lit)*a);
  float band = smoothstep(-8., -2.6, d)*(1. - smoothstep(-2.6, -2., d));
  vec3 rc = mix(vec3(.93, .83, .69), vec3(.79, .88, .84), smoothstep(.25, .95, p.x/uPanel.x));
  over(c, rc, band*.24*(1. - .75*lit)*a);
  float ang = atan(p.y - uPanel.y*.5, p.x - uPanel.x*.5);
  float sp = pow(max(0., cos(ang - uTime*.21 - 2.3)), 36.) + .6*pow(max(0., cos(ang + uTime*.13 + .9)), 60.);
  float spec = line(abs(d + 1.3), 1.5, aa)*sp*a;
  addLight(c, vec3(1., .93, .80)*spec*.5); e.rgb += vec3(1., .88, .70)*spec*.9;
}
void sea(vec2 p, float a, inout vec4 c, inout vec4 e){
  if (uQuota.w < .5 || a <= 0.) return;
  float st = uQuota.w > 2.5 ? 1. : .42;
  float lv = uQuota.z*uS2.y*(1. - uS3.y);
  if (lv <= .001) return;
  for (int k = 0; k < 3; k++){
    float fk = float(k), y = seaY(p.x, fk, lv);
    if (p.y < y - 3.) continue;
    float below = smoothstep(y - .6, y + .6, p.y);
    float depth = clamp((p.y - y)/max(24., uPanel.y - y), 0., 1.);
    vec3 col = k == 0 ? vec3(.855, .915, .89) : k == 1 ? vec3(.935, .892, .826) : vec3(.95, .95, .935);
    col = mix(col, vec3(.86, .86, .84), 1. - st);
    over(c, col, below*(.05 + .055*depth)*(1. - .22*fk)*st*a);
    over(c, vec3(1., .992, .968), exp(-sq((p.y - y)/(.85 + .3*fk)))*(.50 - .12*fk)*st*a);
    if (k == 0) over(c, vec3(.52, .58, .53), exp(-sq((p.y - y - 1.9)/1.15))*.13*st*a);
  }
  float dd = p.y - seaY(p.x, 0., lv);
  if (dd > 0. && dd < 70.){
    float cz = sin(p.x*.11 + uTime*.9 + 2.*sin(p.y*.07 - uTime*.5))*sin(p.x*.05 - p.y*.13 - uTime*.6);
    float caus = pow(max(0., cz), 6.)*exp(-dd/24.);
    addLight(c, vec3(1., .975, .91)*caus*.11*st*a); e.rgb += vec3(1., .92, .78)*caus*.05*st*a;
  }
}
void bokeh(vec2 p, float a, inout vec4 c, inout vec4 e){
  if (a <= 0.) return;
  int N = uQuality < .5 ? 16 : 8;
  for (int i = 0; i < 16; i++){
    if (i >= N) break;
    float fi = float(i), r = 3. + 13.*sq(h11(fi*3.1));
    vec2 q = vec2(mod(h11(fi*1.7)*(uPanel.x + 60.) + uDrift*(5. + 11.*h11(fi*9.)), uPanel.x + 60.) - 30.,
                  h11(fi*5.3)*uPanel.y + 7.*sin(uTime*.23 + fi*1.9));
    float dd = length(p - q);
    if (dd > r + 1.5) continue;
    float disc = 1. - smoothstep(r*.85 - 1.2, r, dd);
    float rv = exp(-sq((dd - r*.86)/(r*.1 + .5)));
    float tw = .55 + .45*sin(uTime*(.5 + .5*h11(fi*4.)) + fi*2.7);
    over(c, vec3(1., .978, .935), (disc*.065 + rv*.06)*tw*a);
    e.rgb += vec3(1., .92, .80)*rv*.06*tw*a;
  }
}
`;
})(window.RA);
