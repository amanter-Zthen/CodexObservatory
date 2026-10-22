/* G02-BG02 glass art controls. All slabs observe one external travelling light. */
(function(NS){
 'use strict';
 const defaults=Object.freeze({
  focus:[870,475],focusScale:[365,265],
  frontFrost:.92,backFrost:.86,lightStrength:1,depthOfField:1,occlusion:.90,thicknessContrast:1,
  period:11.8,duration:7.6,start:.35,
  sweepStart:-420,sweepEnd:1970,
  ground:[216/255,204/255,184/255],key:[239/255,226/255,205/255],mass:[185/255,162/255,130/255],
  exposure:.88,saturation:1.18,whiteBalance:[1.025,1,.955]
 });
 const ease=t=>{t=Math.max(0,Math.min(1,t));return t*t*t*(t*(t*6-15)+10);};
 function illumination(t){
  const age=t<defaults.start?-1:(t-defaults.start)%defaults.period;
  const progress=age>=0&&age<defaults.duration?age/defaults.duration:0;
  const envelope=age>=0&&age<defaults.duration?ease(progress/.09)*(1-ease((progress-.88)/.12)):0;
  const travel=progress+.06*Math.sin(progress*2*Math.PI)/(2*Math.PI);
  return{center:defaults.sweepStart+(defaults.sweepEnd-defaults.sweepStart)*travel,envelope,progress};
 }
 function focalWeight(p,focus=defaults.focus,scale=defaults.focusScale){
  const x=(p[0]-focus[0])/scale[0],y=(p[1]-focus[1])/scale[1];
  return .07+.93*Math.exp(-.5*(x*x+y*y));
 }
 NS.art={defaults,illumination,focalWeight};
})(window.BG16=window.BG16||{});
