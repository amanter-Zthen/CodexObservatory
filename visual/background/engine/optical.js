/* BG16: variable satin transmission over a coherent thickness-based substrate.
 * Geometry is projected 2.5D; refraction samples the previously completed layer.
 * This is an art reconstruction, not a volumetric ray tracer. Local reflection
 * cards, fog density, and guided edge light are explicitly art-directed fields.
 */
(function(NS){
 'use strict';
 const vertex=`#version 300 es
 layout(location=0) in vec2 aUV;uniform vec2 uOrigin,uDU,uDV,uResolution;out vec2 vUV;out vec2 vScreen;
 void main(){vUV=aUV;vScreen=uOrigin+aUV.x*uDU+aUV.y*uDV;gl_Position=vec4(vScreen/uResolution*vec2(2.,-2.)+vec2(-1.,1.),0.,1.);}`;
 const common=`precision highp float;
 in vec2 vUV;in vec2 vScreen;out vec4 color;
 uniform vec2 uResolution,uFocus,uFocusScale;uniform sampler2D uSource,uSceneDepth;
 uniform vec3 uGround,uKey,uMass,uWhiteBalance;uniform float uExposure,uSaturation;
 uniform float uSceneTime,uLightStrength,uBeamCenter,uBeamEnvelope,uFocusFalloff,uBackgroundResponse,uDepthOfField,uOcclusion,uFocusDepth,uIrregularity,uAmbient,uSweep,uSparkles,uPanelOpacity;
 float focusWeight(vec2 p){vec2 q=(p-uFocus)/uFocusScale;return mix(1.,.07+.93*exp(-.5*dot(q,q)),uFocusFalloff);}
 uniform float uDepthHalf;
 float sceneDepth(vec2 p){return texture(uSceneDepth,vec2(p.x/uResolution.x,1.-p.y/uResolution.y)).r*(2.*uDepthHalf)-uDepthHalf;}
 float sceneCoverage(vec2 p){return texture(uSceneDepth,vec2(p.x/uResolution.x,1.-p.y/uResolution.y)).g;}
 float sat(float x){return clamp(x,0.,1.);}float g(float x,float m,float s){return exp(-pow((x-m)/s,2.));}
 vec3 lin(vec3 x){return pow(max(x,vec3(0)),vec3(2.2));}vec3 enc(vec3 x){return pow(max(x,vec3(0)),vec3(1./2.2));}
 vec4 at(vec2 p){return texture(uSource,vec2(p.x/uResolution.x,1.-p.y/uResolution.y));}
 float hash(vec2 p){return fract(sin(dot(p,vec2(127.1,311.7)))*43758.5453);}
 float noise(vec2 p){vec2 i=floor(p),f=fract(p);f=f*f*(3.-2.*f);return mix(mix(hash(i),hash(i+vec2(1,0)),f.x),mix(hash(i+vec2(0,1)),hash(i+vec2(1,1)),f.x),f.y);}
 float fbm(vec2 p){return .67*noise(p)+.23*noise(p*2.13+7.3)+.10*noise(p*4.61-2.6);}
 vec2 incidentField(vec2 p){
  float distance=p.x+.14*(p.y-uFocus.y)-uBeamCenter;
  float train=g(distance,0.,132.)+.26*g(distance,225.,178.)+.12*g(distance,-210.,86.);
  float islands=smoothstep(.26,.73,fbm(p*vec2(.0054,.0086)+vec2(-uSceneTime*.052,uSceneTime*.027)));
  float variation=mix(1.,.12+.88*islands,uIrregularity);
  float streak=g(distance,-12.,36.)+.36*g(distance,62.,100.);
  return vec2(train,streak)*uBeamEnvelope*variation*focusWeight(p)*uLightStrength*uSweep;
 }
 float sharedLight(vec2 p){return incidentField(p).x;}
 float flowingLight(vec2 p){
  // Spatial advection: bright islands migrate, spread and merge without
  // changing the material roughness or using a single breathing multiplier.
  vec2 q=vec2(p.x*.0082-p.y*.0038,p.y*.0104+p.x*.0015);
  float a=fbm(q+vec2(-uSceneTime*.094,uSceneTime*.025));
  float b=noise(q*.61+vec2(uSceneTime*.032,-uSceneTime*.063)+vec2(4.8,13.2));
  float spread=smoothstep(.27,.72,a*.72+b*.28);
  return (.07+.93*spread)*focusWeight(p)*uLightStrength*uAmbient;
 }
 vec3 backgroundColor(vec2 p){
  vec3 a=uGround;
  float key=g(p.x,1250.,960.)*g(p.y,120.,720.);
  a=mix(a,uKey,key*.62);
  float mass=g(p.x,760.,510.)*g(p.y,770.,270.);
  a=mix(a,uMass,mass*.30);
  a+=g(p.y+.20*p.x,670.,210.)*.016+g(p.y-.17*p.x,365.,170.)*.010;
  // Trace near-neutral grey-green stays in the background; no green accent.
  float neutralShift=g(p.x,360.,660.)*g(p.y,230.,350.)*.065;
  a=mix(a,vec3(235.,236.,227.)/255.,neutralShift);
  float broadSpill=g(p.x+.14*(p.y-uFocus.y),uBeamCenter,390.)*uBeamEnvelope*uLightStrength*uSweep;
  vec3 response=vec3(.012,.010,.008)*broadSpill+vec3(.008,.007,.005)*sharedLight(p);
  response+=vec3(.005,.004,.003)*flowingLight(p)*g(p.y,640.,320.);
  return a+response*uBackgroundResponse;
 }`;
 const ground=`#version 300 es
 ${common}
 uniform int uGrid;
 void main(){vec3 a=backgroundColor(vScreen);a+=(hash(vScreen)-.5)/600.;
  if(uGrid==1){float m=min(abs(fract(vScreen.x/29.)-.5),abs(fract(vScreen.y/29.)-.5));a=mix(a,vec3(.45,.38,.28),1.-smoothstep(.012,.034,m));}
  color=vec4(a,0.);
 }`;
 const shadow=`#version 300 es
 ${common}
 uniform vec2 uStart,uEnd;uniform float uStrength;
 void main(){vec2 d=uEnd-uStart;float t=sat(dot(vScreen-uStart,d)/dot(d,d));float dist=length(vScreen-uStart-d*t);
  float a=exp(-pow(dist/17.,2.))*uStrength*smoothstep(0.,.13,t)*(1.-smoothstep(.85,1.,t));color=vec4(.49,.385,.265,a);}`;
 const face=`#version 300 es
 ${common}
 uniform vec2 uSpan,uBodyExtent,uCell;uniform vec3 uN,uTU,uTV,uLocalOrigin,uLocalDU,uLocalDV,uLightMask;
 uniform float uDepth,uEnergy,uIOR,uTime,uBevel,uAbsorption,uGlow,uRefraction,uMaterial,uId,uDynamicLight,uCrest,uCleanSurface,uFrontFrost,uBackFrost,uThicknessContrast;
 uniform int uFace,uMode,uHero;
 uniform sampler2D uArchiveAtlas;uniform float uArchiveOpacity,uArchiveMix;uniform int uArchiveState,uArchivePrevious;uniform vec3 uArchiveInk,uArchivePreviousInk;
 float sdf(vec2 p){
  float r=min(1.55,min(uSpan.x,uSpan.y)*.18);vec2 q=abs(p-uSpan*.5)-(uSpan*.5-vec2(r));return length(max(q,0.))+min(max(q.x,q.y),0.)-r;
 }
 float heightAt(vec2 p){float e=clamp(1.+sdf(p)/uBevel,0.,1.);return uDepth*pow(max(1.-pow(e,2.8),0.),1./2.8);}
 float ease(float x){x=sat(x);return x*x*x*(x*(x*6.-15.)+10.);}
 // One shared external sweep; there are no per-pane event clocks.
 vec3 dynamicEnergy(vec3 local,vec3 N,float rough,float frost){
  if(uDynamicLight<.001)return vec3(0);
  vec2 incoming=incidentField(vScreen);
  float W=uBodyExtent.x,H=uBodyExtent.y,illumination=incoming.x,ray=incoming.y,ambient=flowingLight(vScreen),focus=focusWeight(vScreen);
  float x=max(local.x,0.),y=max(local.y,0.);
  // The acute corner leads a long continuous top edge and a shorter return.
  // The incident light is shared in scene space, rather than timed per pane.
  float longGradient=.10+.90*pow(sat(x/W),.82);
  float longCore=g(H-y,0.,.022+rough*.014)*longGradient;
  float innerReturn=g(H-y,.040,.125)*longGradient*.13;
  float shortGradient=pow(max(0.,1.-(H-y)/(H*.39)),1.60);
  float shortEdge=g(W-x,0.,.025)*shortGradient*.32;
  float acute=g(W-x,0.,.088)*g(H-y,0.,.072)*1.90;
  float lowerEcho=(g(x,0.,.027)*pow(max(0.,1.-y/H),1.2)+g(x,0.,.09)*g(y,0.,.11)*.6)*.20;
  if(uFace==1){float facet=g(vUV.x,.10,.11)+.16*g(vUV.x,.86,.18)+.025;lowerEcho*=facet;longCore*=.18;innerReturn*=.35;acute*=.18;}
  if(uFace==2){float facet=g(vUV.y,.85,.13)+.16*g(vUV.y,.15,.16)+.035;longCore*=facet;innerReturn*=.35;acute*=.45+.55*facet;}
  float glancing=.33+.67*pow(sat(dot(N,normalize(vec3(-.46,-.24,.85)))),3.);
  float edge=(longCore*.72+innerReturn+shortEdge+acute+lowerEcho)*glancing*(illumination*.44+ray*1.65+ambient*.045);
  float reflection=.055+.065*pow(sat(dot(N,normalize(vec3(.64,-.10,.76)))),5.);
  float face=(illumination*.30+ray*.90)*reflection*(.65+.35*frost);
  float baseFoot=g(y,.17,.62)*(.42+.58*g(x,.60,1.55));
  float base=baseFoot*(ambient*.24+illumination*.13);
  base+=g(y,.10,.18)*g(x,.12,.24)*(.08*ambient+.13*ray);
  // Sparse material-bound points. Frost and micro normals remain stationary.
  vec2 grid=local.xy*vec2(3.3,5.3),cell=floor(grid);
  vec2 center=vec2(.2)+.6*vec2(hash(cell+uId),hash(cell+uId+73.));
  vec2 offset=(fract(grid)-center)*vec2(1.,1.25);
  float rare=step(.977,hash(cell+uId+17.));
  float tiny=exp(-dot(offset,offset)/.006),softer=exp(-dot(offset,offset)/.048)*.08;
  float glint=g(offset.x,0.,.25)*g(offset.y,0.,.018)*.23;
  float shimmer=.12+.88*smoothstep(.12,.94,.5+.5*sin(uSceneTime*.72+hash(cell+uId+4.)*6.2831853));
  float points=rare*(tiny+softer+glint)*shimmer*(.14*focus*uLightStrength+1.7*illumination+.35*ambient)*uIrregularity*uSparkles;
  return vec3(base,face,edge*3.7+points*2.4)*uLightMask*uDynamicLight;
 }
 vec3 environment(vec3 r,float rough,vec2 local){
  vec3 a=lin(vec3(.80,.775,.72));vec2 q=vScreen+r.xy*(390./max(abs(r.z),.30));
  float top=g(r.y,-.62,.17+rough*.30)*(.6+.4*g(r.x,.2,.75));
  float broad=g(q.y-.27*q.x,110.,45.+rough*150.)*g(q.x,1110.,600.);
  float shoulder=g(q.x,-95.,190.+rough*330.)*g(q.y,850.,200.+rough*250.);
  float stripe=g(q.y+.18*q.x,480.,9.+rough*60.)*g(q.x,650.,450.);
  float interruption=mix(.56+.44*fbm(local*.75+vec2(uId*.43,5.)),.88,uCleanSurface*float(uFace==0));
  return a+vec3(1.07,1.01,.88)*(top*.9+broad*1.40+shoulder*.78+stripe*.36*interruption);
 }
 void main(){
  vec2 p=vUV*uSpan;float d=sdf(p);if(d>.6)discard;
  float h=heightAt(p),hn=h/max(uDepth,.0001);
  float dx=(heightAt(p+vec2(.4,0))-heightAt(p-vec2(.4,0)))/.8;
  float dy=(heightAt(p+vec2(0,.4))-heightAt(p-vec2(0,.4)))/.8;
  vec3 local=uLocalOrigin+vUV.x*uLocalDU+vUV.y*uLocalDV;
  vec2 q=local.xy+vec2(uId*.237,uId*.093);
  float cloud=fbm(q*.77+vec2(3.4,8.6));
  float mid=fbm(q*2.34+vec2(15.1,1.6));
  float clearWindow=smoothstep(.53,.79,fbm(q*.91+vec2(26.2,-3.5)));
  float broadFace=(uFace==0||uFace==3)?1.:0.;
  float oldFrost=sat(.03+1.14*cloud+.16*mid-.78*clearWindow)*mix(.14,1.,broadFace);
  float cleanFront=sat(uFrontFrost+.035*(noise(q*.28)-.5)+.008*(mid-.5));
  float edgeFade=smoothstep(.3,5.0,-d);
  float frost=mix(oldFrost,cleanFront,uCleanSurface*broadFace)*uMaterial*edgeFade;
  float backFrost=uBackFrost*uCleanSurface*broadFace*uMaterial*edgeFade;
  float rough=mix(.04+.37*frost+.018*mid,.045+.46*frost+.004*(mid-.5),uCleanSurface*broadFace);
  vec3 N=normalize(uN-.095*dx*uTU-.095*dy*uTV);
  // Tiny variation in the broad face is locked to the slab, not animated noise.
  float micro=(noise(q*11.7)-.5)*mix(.006,.0015,uCleanSurface)*uMaterial*broadFace;
  N=normalize(N+micro*uTU+micro*.4*uTV);float nv=max(N.z,.07);
  if(uMode==1){color=vec4(vec3(hn),0.);return;}
  if(uMode==2){color=vec4(N*.5+.5,0.);return;}
  if(uMode==3){color=vec4(vec3(frost,backFrost,rough*2.),0.);return;}
  vec4 original=at(vScreen);vec3 ray=refract(vec3(0,0,-1),N,1./uIOR);
  float pathScale=uFace==1?4.8:uFace==2?2.1:1.;
  float travel=h*pathScale/max(abs(dot(ray,uN)),.25);
  vec2 offset=ray.xy*travel*.073*uRefraction;
  vec2 displaced=vScreen+offset;
  vec3 behind=lin(at(displaced).rgb);
  float radius=.35+frost*frost*7.5;
  if(uCleanSurface>.5&&broadFace>.5){
   // Two frosted broad surfaces compose their blur variances. This is a
   // single-layer optical approximation; side/end facets bypass this finish.
   float sigmaFront=frost*13.,sigmaBack=backFrost*17.;
   radius=sqrt(sigmaFront*sigmaFront+sigmaBack*sigmaBack)+.35;
   vec3 diffuse=behind*.16;
   for(int j=0;j<8;j++){float a=float(j)*.78539816;diffuse+=lin(at(displaced+vec2(cos(a),sin(a))*radius*.46).rgb)*.075;}
   for(int j=0;j<4;j++){float a=float(j)*1.5707963+.392699;diffuse+=lin(at(displaced+vec2(cos(a),sin(a))*radius).rgb)*.06;}
   float finishHaze=1.-(1.-frost*.85)*(1.-backFrost*.80);
   behind=mix(behind,diffuse,finishHaze);
  }else{
   vec3 diffuse=(lin(at(displaced+vec2(radius,0)).rgb)+lin(at(displaced-vec2(radius,0)).rgb)+lin(at(displaced+vec2(0,radius)).rgb)+lin(at(displaced-vec2(0,radius)).rgb))*.25;
   behind=mix(behind,diffuse,frost*.78);
  }
  vec3 sigma=-log(vec3(.855,.785,.665))/125.*uAbsorption;
  vec3 transmission=exp(-sigma*travel*mix(.94+.12*mid,.57,uCleanSurface*broadFace));
  float f0=pow((uIOR-1.)/(uIOR+1.),2.),f=f0+(1.-f0)*pow(1.-nv,5.);
  float reflected=1.-pow(1.-f,2.);
  vec3 env=environment(reflect(vec3(0,0,-1),N),rough,local.xy);
  float area=.50+.50*g(vScreen.x,720.,490.)*g(vScreen.y,500.,450.);
  env*=area;if(uFace==2)env*=.43+.53*g(vUV.x,.02,.08)+.20*g(vUV.x,.56,.22);
  vec3 result=behind*transmission*(1.-reflected)+env*reflected;
  // Spatially varying satin transmission. This models an artistic frosted finish,
  // not a uniform opaque tint. Clear areas retain the sampled neighbouring panes.
  vec3 milk=lin(vec3(.977,.963,.937));
  float milkAmount=mix(frost*(.12+.39*cloud)*(uHero==1?1.22:1.),.10+.16*frost+.10*backFrost,uCleanSurface*broadFace);
  result=mix(result,milk*transmission,milkAmount);
  float softShade=g(local.x,2.1,1.35)*g(local.y,1.25,.80)*broadFace*.10*uMaterial*mix(1.,.12,uCleanSurface);
  result*=1.-softShade;
  // Broad softly edged reflection and smaller polished streaks at different scales.
  float sheen=g(local.y-.24*local.x,1.25+.22*sin(uId*.71),.18+rough*.9);
  float reflectionMask=mix(.28+.72*smoothstep(.31,.73,mid),.80,uCleanSurface*broadFace);
  result+=vec3(.17,.156,.125)*sheen*reflectionMask*frost*mix(1.,.35,uCleanSurface*broadFace);
  // A broad reflected softbox is broken by clarity variations, with a darker
  // warm return underneath. Its extent is finite; the pane remains translucent.
  float faceCard=g(local.x-.43*local.y,1.6+sin(uId*.7)*.9,.55+rough*.95);
  float cardBreak=mix(.38+.62*smoothstep(.33,.72,cloud),.94,uCleanSurface*broadFace);
  result+=vec3(.12,.116,.103)*faceCard*cardBreak*broadFace*uMaterial*mix(1.,.30,uCleanSurface);
  result*=1.-g(local.x-.43*local.y,2.65+sin(uId*.7)*.9,.38)*.052*broadFace*uMaterial*mix(1.,.10,uCleanSurface);
  // Polished thickness faces carry a restrained double reflection and a warm
  // interior return, rather than a flat solid amber strip.
  if(uFace==1){
   float lengthEnvelope=.20+.80*g(vScreen.y,595.,215.);
   // A continuous backlit thickness return: warm absorption inside the facet,
   // with the narrow polished boundaries left free for highlights.
   float thicknessReturn=g(vUV.x,.53,.39)*(.78+.22*g(local.y,uBodyExtent.y*.46,uBodyExtent.y*.50));
   result*=exp(-vec3(.25,.29,.37)*thicknessReturn*uThicknessContrast);
   float polish=g(vUV.x,.075,.075)*.19+g(vUV.x,.86,.045)*.105;
   result+=vec3(.98,.94,.83)*polish*lengthEnvelope;
   result*=1.-g(vUV.x,.48,.25)*(.06+.04*cloud);
  }
  if(uFace==0){
   // The lower polished bevel has a shallow warm return, not a dark outline.
   float lowerReturn=g(local.y,.050,.105)*(1.-g(local.x,0.,.10));
   result*=exp(-vec3(.045,.055,.070)*lowerReturn*uThicknessContrast);
  }
  float striation=pow(.5+.5*sin(local.x*71.+noise(vec2(local.y*.6,uId))*1.1),28.);
  result+=vec3(.006,.0053,.0042)*striation*uMaterial*broadFace*(.3+frost)*mix(1.,.20,uCleanSurface);
  vec3 moving=dynamicEnergy(local,N,rough,frost)*uGlow;
  vec3 warmBody=vec3(.99,.915,.76),whiteCore=vec3(1.,.979,.929);
  result+=warmBody*moving.x*transmission+whiteCore*moving.y;
  result+=mix(vec3(.97,.80,.52),whiteCore,smoothstep(.025,.34,moving.z))*moving.z;
  float light=moving.x*.45+moving.y*.40+moving.z*.90;
  if(uHero==1&&uFace==0){
   float tabs=step(.043,vUV.x)*step(vUV.x,.098)+step(.917,vUV.x)*step(vUV.x,.966);
   if(tabs>.5&&vUV.y>.014&&vUV.y<.079){vec3 ink=mix(uArchivePreviousInk,uArchiveInk,uArchiveMix);result=mix(result,lin(ink),uArchiveOpacity);}
  }
  if(uFace==0&&uArchiveState>=0&&uArchiveOpacity>0.){
   vec4 ink=texture(uArchiveAtlas,vec2(vUV.x,(vUV.y+float(uArchiveState))/3.));
   if(uArchivePrevious>=0&&uArchivePrevious!=uArchiveState&&uArchiveMix<.999){vec4 previous=texture(uArchiveAtlas,vec2(vUV.x,(vUV.y+float(uArchivePrevious))/3.));ink=mix(previous,ink,uArchiveMix);}
   result=mix(result,lin(ink.rgb),ink.a*uArchiveOpacity);
  }
  float coverage=1.-smoothstep(-.65,.65,d);
  float farFade=0.;
  float coverageMix=coverage*(1.-farFade)*uPanelOpacity;
  color=vec4(mix(original.rgb,enc(result),coverageMix),mix(original.a,max(original.a*mix(.94,.56,frost),light*.78+max(max(env.r,env.g),env.b)*f*.02),coverageMix));
 }`;
 const extract=`#version 300 es
 ${common}
 void main(){
  vec3 e=vec3(0);
  for(int y=-1;y<=1;y++)for(int x=-1;x<=1;x++){
   vec4 v=at(vScreen+vec2(x,y)*1.4);e+=lin(v.rgb)*v.a;
  }
  color=vec4(e/9.,1.);
 }`;
 const blur=`#version 300 es
 ${common}
 uniform vec2 uAxis;
 void main(){vec3 c=vec3(0);float total=0.;
  for(int i=-5;i<=5;i++){float x=float(i),w=exp(-x*x/8.);c+=at(vScreen+uAxis*x*9.).rgb*w;total+=w;}
  color=vec4(c/total,1.);
 }`;
 const depthPass=`#version 300 es
 ${common}
 uniform vec2 uSpan;uniform vec3 uPlane;
 void main(){
  float r=min(1.55,min(uSpan.x,uSpan.y)*.18);
  vec2 p=vUV*uSpan,q=abs(p-uSpan*.5)-(uSpan*.5-vec2(r));
  float d=length(max(q,0.))+min(max(q.x,q.y),0.)-r;if(d>.6)discard;
  float z=uPlane.x+vUV.x*uPlane.y+vUV.y*uPlane.z;
  color=vec4((z+uDepthHalf)/(2.*uDepthHalf),uPanelOpacity,0,1);
 }`;
 const finish=`#version 300 es
 ${common}
 uniform float uBloom;uniform int uMode;uniform sampler2D uBloomSource;
 void main(){
  vec4 source=at(vScreen);float z=sceneDepth(vScreen);
  if(uMode!=0){color=vec4(source.rgb,1.);return;}
  bool object=z>-uDepthHalf+1.;
  float focal=focusWeight(vScreen);
  float coc=(.15+2.4*smoothstep(.9,6.,abs(z-uFocusDepth))+1.8*(1.-focal))*uDepthOfField;
  if(!object)coc=.32;
  vec3 resolved=source.rgb*.24;float total=.24;
  for(int i=0;i<8;i++){
   float a=float(i)*.78539816;vec2 delta=vec2(cos(a),sin(a))*coc;
   float nearDepth=sceneDepth(vScreen+delta),w=.095;
   if(object&&abs(nearDepth-z)>1.5)w*=.13;
   resolved+=at(vScreen+delta).rgb*w;total+=w;
  }
  resolved/=total;
  // Compare with a local depth plane so a tilted pane does not self-shadow.
  vec2 slope=clamp(vec2(dFdx(z),dFdy(z)*-1.),vec2(-.15),vec2(.15));
  float shade=0.;
  if(object){
   for(int ring=0;ring<3;ring++)for(int i=0;i<8;i++){
    float a=float(i)*.78539816+.16;vec2 delta=vec2(cos(a),sin(a))*(ring==0?5.:ring==1?13.:26.);
    float neighbor=sceneDepth(vScreen+delta);
    float residual=neighbor-(z+dot(slope,delta));
    float blocker=smoothstep(.10,.60,residual)*(1.-smoothstep(2.,5.,residual));
    shade+=blocker*sceneCoverage(vScreen+delta)*(ring==0?.48:ring==1?.32:.20);
   }
  }
  shade=shade/8.*uOcclusion*sceneCoverage(vScreen);
  vec3 lit=lin(resolved)*mix(vec3(1.),vec3(.62,.57,.46),shade);
  // Grade the substrate and material in linear light before adding emitted
  // energy. Mid-tones become warm champagne; small light cores keep headroom.
  float baseY=dot(lit,vec3(.2126,.7152,.0722));
  lit=max(vec3(0),mix(vec3(baseY),lit,uSaturation))*uExposure*uWhiteBalance;
  vec3 bloom=vec3(0);float weights=0.;
  if(uBloom>0.){
   for(int ring=0;ring<2;ring++)for(int i=0;i<12;i++){
    float a=float(i)*.52359878+float(ring)*.27,r=ring==0?7.:27.,w=ring==0?.65:.35;
    vec4 q=at(vScreen+vec2(cos(a),sin(a))*r);
    bloom+=lin(q.rgb)*q.a*w;weights+=w;
   }
   vec3 halo=texture(uBloomSource,vec2(vScreen.x/uResolution.x,1.-vScreen.y/uResolution.y)).rgb;
   lit+=(bloom/max(weights,.001)*.36+halo*.83)*vec3(1.,.935,.80)*uBloom;
  }
  lit*=mix(.955,1.,focal);
  float luminance=dot(lit,vec3(.2126,.7152,.0722));
  if(luminance>.84){float soft=.84+.16*(1.-exp(-(luminance-.84)/.30));lit*=soft/luminance;}
  color=vec4(enc(lit),1.);
 }`;
 class Optical{
  constructor(canvas,context=null){
   const gl=this.gl=context||canvas.getContext('webgl2',{alpha:false,premultipliedAlpha:false,antialias:false,preserveDrawingBuffer:true});this.canvas=canvas;if(!gl)throw Error('WebGL2 unavailable');
   this.hdr=!!gl.getExtension('EXT_color_buffer_float');gl.getExtension('EXT_float_blend');this.archiveAtlas=new NS.archive.Atlas(gl);
   this.programs={ground:this.program(ground),shadow:this.program(shadow),face:this.program(face),depth:this.program(depthPass),extract:this.program(extract),blur:this.program(blur),finish:this.program(finish)};this.uniforms=new Map();
   this.vao=gl.createVertexArray();gl.bindVertexArray(this.vao);this.buffer=gl.createBuffer();gl.bindBuffer(gl.ARRAY_BUFFER,this.buffer);gl.bufferData(gl.ARRAY_BUFFER,new Float32Array([0,0,1,0,0,1,0,1,1,0,1,1]),gl.STATIC_DRAW);gl.enableVertexAttribArray(0);gl.vertexAttribPointer(0,2,gl.FLOAT,false,0,0);
   this.targets=[this.target(1672,941),this.target(1672,941)];this.halos=[this.target(418,236),this.target(418,236)];this.sceneDepthTarget=this.target(1672,941,true);gl.disable(gl.BLEND);gl.disable(gl.DEPTH_TEST);this.draws=0;
  }
  program(src){const gl=this.gl;const shader=(kind,source)=>{const s=gl.createShader(kind);gl.shaderSource(s,source);gl.compileShader(s);if(!gl.getShaderParameter(s,gl.COMPILE_STATUS))throw Error(gl.getShaderInfoLog(s));return s;};const p=gl.createProgram();gl.attachShader(p,shader(gl.VERTEX_SHADER,vertex));gl.attachShader(p,shader(gl.FRAGMENT_SHADER,src));gl.linkProgram(p);if(!gl.getProgramParameter(p,gl.LINK_STATUS))throw Error(gl.getProgramInfoLog(p));return p;}
  target(w,h,depthOnly=false){const g=this.gl,t=g.createTexture(),f=g.createFramebuffer();g.bindTexture(g.TEXTURE_2D,t);g.texImage2D(g.TEXTURE_2D,0,this.hdr?(depthOnly?g.RG16F:g.RGBA16F):g.RGBA8,w,h,0,this.hdr&&depthOnly?g.RG:g.RGBA,this.hdr?g.HALF_FLOAT:g.UNSIGNED_BYTE,null);for(const p of[g.TEXTURE_MIN_FILTER,g.TEXTURE_MAG_FILTER])g.texParameteri(g.TEXTURE_2D,p,g.LINEAR);for(const p of[g.TEXTURE_WRAP_S,g.TEXTURE_WRAP_T])g.texParameteri(g.TEXTURE_2D,p,g.CLAMP_TO_EDGE);g.bindFramebuffer(g.FRAMEBUFFER,f);g.framebufferTexture2D(g.FRAMEBUFFER,g.COLOR_ATTACHMENT0,g.TEXTURE_2D,t,0);if(g.checkFramebufferStatus(g.FRAMEBUFFER)!==g.FRAMEBUFFER_COMPLETE)throw Error('Framebuffer incomplete');return{t,f};}
  use(p){
   this.p=p;this.gl.useProgram(p);this.set('uResolution',[1672,941]);
   const D=NS.art.defaults;
   this.set('uGround',D.ground);this.set('uKey',D.key);this.set('uMass',D.mass);
   this.set('uExposure',D.exposure);this.set('uSaturation',D.saturation);this.set('uWhiteBalance',D.whiteBalance);
   if(this.frame){const f=this.frame;this.set('uFocus',f.focus);this.set('uFocusScale',f.focusScale);
    this.set('uDepthHalf',f.depthHalf);
    this.set('uSceneTime',f.time);this.set('uLightStrength',f.light);this.set('uBeamCenter',f.beam.center);this.set('uBeamEnvelope',f.beam.envelope);
    this.set('uFocusFalloff',f.falloff);this.set('uBackgroundResponse',f.background);this.set('uDepthOfField',f.dof);this.set('uOcclusion',f.occlusion);this.set('uFocusDepth',f.focusDepth);this.set('uIrregularity',f.irregularity);this.set('uAmbient',f.ambient);this.set('uSweep',f.sweep);this.set('uSparkles',f.sparkles);
   }
  }
  set(name,value,int=false){const g=this.gl;let loc=this.uniforms.get(this.p);if(!loc){loc={};this.uniforms.set(this.p,loc);}if(!(name in loc))loc[name]=g.getUniformLocation(this.p,name);const u=loc[name];if(u===null)return;if(Array.isArray(value)){if(value.length===2)g.uniform2fv(u,value);else g.uniform3fv(u,value);}else if(int)g.uniform1i(u,value);else g.uniform1f(u,value);}
  draw(p){this.set('uOrigin',p[0]);this.set('uDU',[p[1][0]-p[0][0],p[1][1]-p[0][1]]);this.set('uDV',[p[3][0]-p[0][0],p[3][1]-p[0][1]]);this.gl.drawArrays(this.gl.TRIANGLES,0,6);this.draws++;}
  full(){this.draw([[0,0],[1672,0],[1672,941],[0,941]]);}
  render(s,o){
   const g=this.gl,D=NS.art.defaults;
   const eye=[NS.geometry.X[2],NS.geometry.Y[2],NS.geometry.Z[2]],dot=(a,b)=>a.reduce((v,x,i)=>v+x*b[i],0);
   const focus=s.hero||s.panels[0],focusPoint=o.focus??D.focus;
   let focusDepth=focus?dot(focus.worldCenter||[focus.x+focus.length*.5,focus.base+focus.height*.5,focus.z+focus.depth],eye):0;
   // Focus on the visible surface under the scene focus, following the same
   // painter order as the depth pass. The raised hero is only the fallback.
   for(const b of s.panels)for(const p of b.parts){
    const dx=focusPoint[0]-p.p[0][0],dy=focusPoint[1]-p.p[0][1];
    const ax=p.p[1][0]-p.p[0][0],ay=p.p[1][1]-p.p[0][1],bx=p.p[3][0]-p.p[0][0],by=p.p[3][1]-p.p[0][1],det=ax*by-ay*bx;
    const u=(dx*by-dy*bx)/det,v=(ax*dy-ay*dx)/det;
    if(u>=0&&u<=1&&v>=0&&v<=1)focusDepth=dot(p.worldOrigin||[b.x+p.localOrigin[0],b.base+p.localOrigin[1],b.z+p.localOrigin[2]],eye)+u*dot(p.worldDU||p.localDU,eye)+v*dot(p.worldDV||p.localDV,eye);
   }
   this.frame={time:o.lightTime??s.t,focus:focusPoint,focusScale:o.focusScale??D.focusScale,depthHalf:s.depthHalf??16,
    light:(o.dynamicLight??1)*(o.lightStrength??D.lightStrength),beam:NS.art.illumination(o.lightTime??s.t),
    falloff:o.focusFalloff??1,background:o.backgroundResponse??1,dof:o.depthOfField??D.depthOfField,
    occlusion:o.occlusion??D.occlusion,focusDepth,irregularity:o.irregularity??1,ambient:o.ambient??1,sweep:o.sweep??1,sparkles:o.sparkles??1};
   this.draws=0;g.disable(g.BLEND);g.disable(g.DEPTH_TEST);g.colorMask(true,true,true,true);g.activeTexture(g.TEXTURE0);g.bindVertexArray(this.vao);g.viewport(0,0,1672,941);
   g.bindFramebuffer(g.FRAMEBUFFER,this.sceneDepthTarget.f);g.clearBufferfv(g.COLOR,0,new Float32Array([0,0,0,1]));this.use(this.programs.depth);
   for(const b of s.panels)for(const part of b.parts){
    const local=part.localOrigin;this.set('uSpan',part.span);this.set('uPanelOpacity',b.opacity??1);
    this.set('uPlane',[dot(part.worldOrigin||[b.x+local[0],b.base+local[1],b.z+local[2]],eye),dot(part.worldDU||part.localDU,eye),dot(part.worldDV||part.localDV,eye)]);
    this.draw(part.p);
   }
   g.bindFramebuffer(g.FRAMEBUFFER,this.targets[0].f);this.use(this.programs.ground);this.set('uGrid',o.grid?1:0,true);this.full();let src=0;
   if(!(o.debug>0)){
    this.use(this.programs.shadow);g.enable(g.BLEND);g.blendFunc(g.SRC_ALPHA,g.ONE_MINUS_SRC_ALPHA);g.colorMask(true,true,true,false);
    for(const b of s.panels){const a=b.baseA,z=b.baseB;this.set('uStart',[a[0]+5,a[1]+8]);this.set('uEnd',[z[0]+5,z[1]+8]);this.set('uStrength',(b.kind==='hero'?.04:.035)*((o.cleanSurface??1)>.5?.45:1)*(b.opacity??1));const x0=Math.min(a[0],z[0])-48,x1=Math.max(a[0],z[0])+48,y0=Math.min(a[1],z[1])-38,y1=Math.max(a[1],z[1])+50;this.draw([[x0,y0],[x1,y0],[x1,y1],[x0,y1]]);}
    g.colorMask(true,true,true,true);g.disable(g.BLEND);
   }
   for(const b of s.panels){
    const dst=1-src;g.bindFramebuffer(g.READ_FRAMEBUFFER,this.targets[src].f);g.bindFramebuffer(g.DRAW_FRAMEBUFFER,this.targets[dst].f);g.blitFramebuffer(0,0,1672,941,0,0,1672,941,g.COLOR_BUFFER_BIT,g.NEAREST);g.bindFramebuffer(g.FRAMEBUFFER,this.targets[dst].f);
    this.use(this.programs.face);g.activeTexture(g.TEXTURE0);g.bindTexture(g.TEXTURE_2D,this.targets[src].t);this.set('uSource',0,true);
    g.activeTexture(g.TEXTURE3);g.bindTexture(g.TEXTURE_2D,this.archiveAtlas.texture);this.set('uArchiveAtlas',3,true);g.activeTexture(g.TEXTURE0);
    this.set('uArchiveState',b.archiveState??-1,true);this.set('uArchivePrevious',b.archivePrevious??-1,true);this.set('uArchiveMix',b.archiveMix??1);this.set('uArchiveOpacity',(b.archiveOpacity??0)*(o.archiveText??1));this.set('uPanelOpacity',b.opacity??1);
    this.set('uArchiveInk',NS.archive.inkColors[Math.max(0,b.archiveState)]);this.set('uArchivePreviousInk',NS.archive.inkColors[Math.max(0,b.archivePrevious)]);
    this.set('uIOR',1.48);this.set('uDepth',b.depth*NS.geometry.SCALE);this.set('uBodyExtent',[b.length,b.height]);this.set('uAbsorption',o.absorption??1);this.set('uEnergy',b.energy);this.set('uTime',o.lightTime??s.t);this.set('uCell',[b.row,b.lane]);this.set('uDynamicLight',o.dynamicLight??1);this.set('uLightMask',o.lightMask??[1,1,1]);this.set('uCrest',b.motion.crest);this.set('uGlow',o.glow??1);this.set('uRefraction',o.refraction??1);this.set('uMaterial',o.material??1);this.set('uCleanSurface',o.cleanSurface??1);this.set('uFrontFrost',o.frontFrost??D.frontFrost);this.set('uBackFrost',o.backFrost??D.backFrost);this.set('uThicknessContrast',o.thicknessContrast??D.thicknessContrast);this.set('uId',b.id);this.set('uMode',o.debug??0,true);this.set('uHero',b.archiveOpacity>0?1:0,true);
    for(const part of b.parts){this.set('uSpan',part.span);this.set('uN',part.normal);this.set('uTU',part.tu);this.set('uTV',part.tv);this.set('uLocalOrigin',part.localOrigin);this.set('uLocalDU',part.localDU);this.set('uLocalDV',part.localDV);this.set('uFace',part.type,true);this.set('uBevel',part.type===0||part.type===3?4.2:part.type===1?2.8:1.7);this.draw(part.p);}src=dst;
   }
   if(!(o.debug>0)){
    g.viewport(0,0,418,236);g.bindFramebuffer(g.FRAMEBUFFER,this.halos[0].f);this.use(this.programs.extract);g.bindTexture(g.TEXTURE_2D,this.targets[src].t);this.set('uSource',0,true);this.full();
    for(let axis=0;axis<2;axis++){g.bindFramebuffer(g.FRAMEBUFFER,this.halos[1-axis].f);this.use(this.programs.blur);g.bindTexture(g.TEXTURE_2D,this.halos[axis].t);this.set('uSource',0,true);this.set('uAxis',axis===0?[1,0]:[0,1]);this.full();}
   }
   g.bindFramebuffer(g.FRAMEBUFFER,o.framebuffer||null);g.viewport(0,0,this.canvas.width,this.canvas.height);this.use(this.programs.finish);g.activeTexture(g.TEXTURE0);g.bindTexture(g.TEXTURE_2D,this.targets[src].t);this.set('uSource',0,true);g.activeTexture(g.TEXTURE1);g.bindTexture(g.TEXTURE_2D,this.halos[0].t);this.set('uBloomSource',1,true);g.activeTexture(g.TEXTURE2);g.bindTexture(g.TEXTURE_2D,this.sceneDepthTarget.t);this.set('uSceneDepth',2,true);g.activeTexture(g.TEXTURE0);this.set('uBloom',o.bloom??1);this.set('uMode',o.debug??0,true);this.full();
   const err=g.getError();if(err!==g.NO_ERROR)throw Error('WebGL error '+err);return{drawCalls:this.draws,panels:s.panels.length,framebuffers:5,format:this.hdr?'RGBA16F':'RGBA8',workingTextureBytes:(1672*941*2+418*236*2)*(this.hdr?8:4)+1672*941*(this.hdr?4:4)+512*1152*4,archiveTextures:1,poolCapacity:NS.geometry.MAX_PANELS,debug:o.debug??0};
  }
 }
 NS.Optical=Optical;
})(window.BG16);
