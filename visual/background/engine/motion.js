/* BG16. Pure, seekable fields: quiet drift + a delayed damped travelling wave.
 * Source concepts: RhineLabUI idleWave / selection settling, MIT.
 * The repeating background schedule and below tuning are original to this study.
 */
(function(NS){
 'use strict';
 const TAU=2*Math.PI;
 const P=Object.freeze({idleAmplitude:.078,secondaryAmplitude:.022,minorAmplitude:.006,
  idlePeriod:8.7,secondaryPeriod:13.1,minorPeriod:5.9,
  wavePeriod:9.4,waveStart:1.6,travelTime:3.6,
  impulse:.315,damping:.88,omega:2.72,rise:.28,tailStart:4.8,tailEnd:6.2});
 const smooth=x=>{x=Math.max(0,Math.min(1,x));return x*x*x*(x*(x*6-15)+10);};
 function quiet(row,lane,t,screenQ){
  const phase=screenQ*6.4,jitter=.10*Math.sin(row*1.37+lane*2.41);
  return (P.idleAmplitude*Math.sin(TAU*t/P.idlePeriod+phase+jitter)+
    P.secondaryAmplitude*Math.sin(TAU*t/P.secondaryPeriod+screenQ*3.7+lane*.10)+
    P.minorAmplitude*Math.sin(TAU*t/P.minorPeriod+screenQ*12.9+jitter))*smooth(t/1.4);
 }
 function impulse(age){
  if(age<=0||age>=P.tailEnd)return 0;
  return P.impulse*Math.exp(-P.damping*age)*Math.sin(P.omega*age)*
   smooth(age/P.rise)*(1-smooth((age-P.tailStart)/(P.tailEnd-P.tailStart)));
 }
 function arrival(screenQ){return P.waveStart+(1-screenQ)*P.travelTime;}
 function packet(row,lane,t,screenQ){
  const delay=arrival(screenQ)-P.waveStart;
  const recent=Math.floor((t-P.waveStart-delay)/P.wavePeriod);
  if(recent<0)return 0;
  return impulse(t-(P.waveStart+recent*P.wavePeriod+delay));
 }
 function sample(row,lane,t,options={},screenQ=.5){
  const mode=options.motionMode||'all',gain=options.amplitude??1;
  const idle=(mode==='all'||mode==='idle')?quiet(row,lane,t,screenQ):0;
  const pulse=(mode==='all'||mode==='pulse')?packet(row,lane,t,screenQ):0;
  return {idle:idle*gain,pulse:pulse*gain,lift:(idle+pulse)*gain,crest:Math.max(0,pulse)/.19};
 }
 NS.motion={params:P,smooth,quiet,impulse,packet,sample,arrival};
})(window.BG16=window.BG16||{});
