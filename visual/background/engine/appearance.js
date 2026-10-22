/* Complete, atomic glass rise/collapse clips. Depth comes from the fixed camera.
 * Dynamics owns the latest desired state and starts clips at settled endpoints.
 * Each clip finishes before another starts; storage and duration are bounded.
 */
(function(NS){
 'use strict';
 const P=Object.freeze({
  nearDepth:7,farDepth:-9,travel:.70,drop:6.2,
  rise:1.22,wakeDelay:1.07,wakeAmplitude:.24,wakeDamping:1.45,wakeOmega:5.1,
  wakeTailStart:1.55,wakeTailEnd:2.15,
  anticipation:.12,anticipationLift:.24,fall:.82,
  pitchPerSpeed:.0025,maxPitch:.026
 });
 const clamp=x=>Math.max(0,Math.min(1,x));
 const smooth=x=>{x=clamp(x);return x*x*x*(10+x*(-15+6*x));};
 const lerp=(a,b,u)=>a+(b-a)*u;
 const rank=depth=>clamp((P.nearDepth-depth)/(P.nearDepth-P.farDepth));
 const segment=(a,b,t,duration)=>lerp(a,b,smooth(t/duration));
 function wake(age){
  if(age<=0||age>=P.wakeTailEnd)return 0;
  return P.wakeAmplitude*Math.exp(-P.wakeDamping*age)*Math.sin(P.wakeOmega*age)*
   smooth(age/.14)*(1-smooth((age-P.wakeTailStart)/(P.wakeTailEnd-P.wakeTailStart)));
 }
 function value(s,q,elapsed=s.elapsed){
  if(s.mode==='visible')return 0;if(s.mode==='hidden')return -P.drop;
  const age=Math.max(0,elapsed)-clamp(q)*P.travel;
  if(s.mode==='enter')return segment(-P.drop,0,age,P.rise)+wake(age-P.wakeDelay);
  if(age<P.anticipation)return segment(0,P.anticipationLift,age,P.anticipation);
  return segment(P.anticipationLift,-P.drop,age-P.anticipation,P.fall);
 }
 function velocity(s,q){
  if(s.mode==='visible'||s.mode==='hidden')return 0;
  if(s.elapsed<.0001)return 0;
  return (value(s,q,s.elapsed+.0001)-value(s,q,s.elapsed-.0001))/.0002;
 }
 function sample(s,depth,out={}){
  const q=rank(depth),offset=s?value(s,q):0,v=s?velocity(s,q):0;
  out.rank=q;out.delay=q*P.travel;out.offset=offset;out.velocity=v;
  out.visibility=smooth((1+offset/P.drop-.08)/.74);
  out.pitch=Math.max(-P.maxPitch,Math.min(P.maxPitch,v*P.pitchPerSpeed));
  out.ambient=.3+.7*smooth(1+offset/P.drop);
  return out;
 }
 let serial=0;
 class Controller{
  constructor(){
   this.id=++serial;this.generation=0;this.elapsed=0;this.mode='visible';this.completed=0;
  }
  immediate(shown){this.mode=shown?'visible':'hidden';this.elapsed=0;this.generation++;}
  request(mode){
   if(!['enter','exit'].includes(mode))throw Error('Unknown glass appearance');
   if(this.mode==='enter'||this.mode==='exit')return false;
   if((mode==='enter'&&this.mode==='visible')||(mode==='exit'&&this.mode==='hidden'))return false;
   this.elapsed=0;this.mode=mode;this.generation++;return true;
  }
  update(dt){
   if(this.mode==='visible'||this.mode==='hidden')return;
   this.elapsed+=Math.max(0,dt);
   const duration=P.travel+(this.mode==='enter'?P.wakeDelay+P.wakeTailEnd:P.anticipation+P.fall);
   if(this.elapsed>=duration){this.mode=this.mode==='enter'?'visible':'hidden';this.elapsed=duration;this.completed++;}
  }
 }
 NS.appearance={params:P,Controller,rank,value,velocity,sample,wake,smooth};
})(window.BG16);
