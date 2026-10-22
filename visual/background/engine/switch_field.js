/* BG16: the shallow bed, wake and shared roll follow the same transport.
 * The source settling shape remains a contour reference. Its independent
 * shoulder chase and full-strength selection wave are not superimposed on
 * the four-slot transport. BG16 separates quiet and periodic-wave gain.
 * Fixed parameter index: ../../玻璃图层_参数总表.md
 */
(function(NS){
 'use strict';
 const smooth=t=>{t=Math.max(0,Math.min(1,t));return t*t*t*(10+t*(-15+6*t));};
 const params=Object.freeze({restCenterOffset:.14,activeCrest:.56,crestWidth:6.0,
  movingCrestReduction:.15,ambientGain:.28,idleScale:1.5,packetScale:1.6,wakeAmplitude:.075,tailGain:.045});
 function state(a){const roll=NS.roll.sample(a);return{row:7-a.rail/.62,lane:1+a.trackX/5.2,drive:roll.weight,rest:a.flatten||0};}
 function centerWeight(lane,c){return 1-smooth(Math.abs(lane-c.lane));}
 function profile(row,lane,a,c=state(a)){
  const d=row-c.row,center=centerWeight(lane,c);
  const shared=NS.layoutProfile.settlingWave(d,26.56)*NS.layoutProfile.columnStrength(1,0);
  const crest=params.activeCrest*Math.exp(-.5*(d/params.crestWidth)**2)*(1-c.rest)*(1-params.movingCrestReduction*c.drive);
  return shared+center*(params.restCenterOffset+crest);
 }
 function pulse(distance,age){if(age<0||age>3.2)return 0;const phase=distance-age*8;return .8*smooth(age/.2)*Math.exp(-age*1.15)*Math.cos(phase*.58)*Math.exp(-.5*(phase/3.4)**2);}
 function selection(row,lane,t,a){let sum=0;for(const p of a.pulses||[])sum+=pulse(Math.hypot(row-p.row,(lane-p.lane)*2.2),t-p.time);return Math.max(-.6,Math.min(.6,sum));}
 function response(row,lane,t,a,c){
  const d=row-c.row,lateral=lane-c.lane;
  // A shallow travelling bed: phase is distance along the moving rail,
  // rather than an independently advancing crest or per-card tween.
  const wake=-Math.sin((d+1.1)*.62)*Math.exp(-.5*((d+1.1)/4.8)**2)*(.76+.24*Math.exp(-.5*lateral*lateral));
  const tail=selection(row,lane,t,a)*params.tailGain*(1-.90*c.drive);
  return (params.wakeAmplitude*wake*c.drive+tail)*(1-c.rest);
 }
 function field(row,lane,t,a,o={},c=state(a)){
  const d=row-c.row,q=Math.max(0,Math.min(1,(d+7)/14));
  // All resting columns share the same gentle spatial contour and drift.
  // At a fixed row, the middle column only adds the small center offset.
  const m=NS.motion.sample(d,0,t,o,q);
  const ambient=(m.idle*(o.idleScale??params.idleScale)+m.pulse*(o.packetScale??params.packetScale))*
   params.ambientGain*(a.idleGain??1)*(1-.85*c.drive);
  return profile(row,lane,a,c)+ambient+response(row,lane,t,a,c)*(o.interactionGain??1);
 }
 function sample(row,lane,t,a,o,screenQ,quality,c=state(a)){const base=field(row,lane,t,a,o,c),slope=field(row+.5,lane,t,a,o,c)-field(row-.5,lane,t,a,o,c);return{base,selection:response(row,lane,t,a,c),pitch:slope*.024*(1-quality),slope};}
 NS.switchField={params,state,centerWeight,profile,pulse,selection,response,field,sample,smooth};
})(window.BG16);
