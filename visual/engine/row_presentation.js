// One bounded presentation owner per model row. Token cadence sets intent;
// a complete expand/collapse clip drives the name, rate, guide and orb together.
(function(NS){
 'use strict';
 const P=NS.presentation,{clamp01,smoother,lerp}=NS.core,TAU=Math.PI*2;
 // Slightly earlier travel leaves a longer, quiet settling tail. The same
 // monotonic curve drives the glyph position and both moving end blocks.
 const travel=u=>{u=clamp01(u);return smoother(u+P.rowSettleBias*u*(1-u));};
 // 0.4.8 floatui/layout.go Tween: quintic arrival with a bounded incoming tangent.
 // Sample on presented frames; frequent targets keep the current value and velocity.
 class RateTween{
  constructor(){this.x=0;this.from=0;this.target=0;this.start=0;this.duration=0;this.tangent=0;}
  value(now){
   if(this.duration<=0)return this.target;
   const p=clamp01((now-this.start)/this.duration),h=p-6*p**3+8*p**4-3*p**5;
   return this.from+(this.target-this.from)*smoother(p)+this.tangent*this.duration*h;
  }
  velocity(now){
   if(this.duration<=0||now<=this.start||now>=this.start+this.duration)return 0;
   const p=(now-this.start)/this.duration;
   return (this.target-this.from)*30*p*p*(p-1)*(p-1)/this.duration+this.tangent*(1-18*p*p+32*p**3-15*p**4);
  }
  set(target,now){
   if(target===this.target)return this;
   const value=this.value(now),delta=target-value,duration=P.rateTweenDuration;
   let v=this.velocity(now);v=v*delta<=0?0:Math.sign(v)*Math.min(Math.abs(v),1.5*Math.abs(delta)/duration);
   this.from=this.x=value;this.target=target;this.start=now;this.duration=duration;this.tangent=v;return this;
  }
  step(now){return this.x=this.value(now);}
 }
 class RowMotion{
  constructor(seed=0){
   this.wanted=false;this.open=0;this.phase='closed';this.elapsed=0;
   this.quiet=P.outputPauseHold;this.active=0;this.angle=seed*TAU;this.completed=0;
  }
  setOutput(shown){this.wanted=!!shown;if(this.wanted)this.quiet=0;}
  closeImmediate(){this.open=0;this.phase='closed';this.elapsed=0;this.active=0;this.quiet=P.outputPauseHold;}
  step(dt,rate,allowOpen=true,forceClose=false){
   dt=Math.max(0,Math.min(.1,dt));
   this.quiet=this.wanted?0:Math.min(P.outputPauseHold,this.quiet+dt);
   this.active=this.wanted?Math.min(P.outputStartDelay,this.active+dt):0;
   const wanted=this.wanted?(this.phase!=='closed'||this.active>=P.outputStartDelay):this.phase!=='closed'&&this.quiet<P.outputPauseHold;
   const target=!forceClose&&allowOpen&&wanted;
   if(this.phase==='closed'&&target){this.phase='opening';this.elapsed=0;}
   else if(this.phase==='open'&&!target){this.phase='closing';this.elapsed=0;}
   if(this.phase==='opening'||this.phase==='closing'){
    const opening=this.phase==='opening',duration=opening?P.rowExpandDuration:P.rowCollapseDuration;
    this.elapsed+=dt;const u=clamp01(this.elapsed/duration);
    this.open=opening?travel(u):1-travel(u);
    if(u===1){this.phase=opening?'open':'closed';this.completed++;}
   }
   this.turns=P.orbIdleTurns+(P.orbOutputTurns+P.orbRateTurns*clamp01(rate/60)-P.orbIdleTurns)*this.open;
   this.angle=(this.angle+TAU*this.turns*dt)%TAU;
   return this.open;
  }
 }
 function fit(at,text,font,limit){
  // Display work is bounded independently of the original model evidence.
  text=String(text??'—').slice(0,192);
  const points=Array.from(text);if(points.length>64)text=points.slice(0,63).join('')+'…';
  const tracking=.15/font,width=at.measure('name',text,1,tracking);
  const size=Math.min(font,Math.max(9,limit/Math.max(.001,width)));
  const track=.15*size/font;
  let display=text;
  if(at.measure('name',display,size,track)>limit){
   const chars=[...display];let lo=0,hi=chars.length;
   while(lo<hi){const mid=Math.ceil((lo+hi)/2);if(at.measure('name',chars.slice(0,mid).join('')+'…',size,track)<=limit)lo=mid;else hi=mid-1;}
   display=chars.slice(0,lo).join('')+'…';
  }
  return{text:display,size,track,width:at.measure('name',display,size,track)};
 }
 function rowLayout(at,row,panelWidth){
  const open=clamp01(row.rateOp),left=P.infoLeft,right=panelWidth-P.infoRight,center=(left+right)/2;
  const rs=row.font*.8,unitWidth=at.measure('rate','/s',rs*.78,.2);
  // Fixed capacity, so a change in the number of digits never moves the name.
  const rateWidth=at.measure('rate','999.9',rs,.3)+unitWidth+1;
  const finalLeft=P.sessionX,finalRight=panelWidth-P.rowRightInset;
  const limit=finalRight-finalLeft-P.modelRateGap-rateWidth;
  const name=fit(at,row.model,row.font,limit);
  const previous=fit(at,row.prev||row.model,row.font,limit);
  const width=lerp(previous.width,name.width,clamp01(row.mix));
  const collapsedX=P.collapsedSessionX;
  const modelX=lerp(collapsedX,finalLeft,open),rulerOrigin=collapsedX+width/2;
  return{modelX,orbX:modelX-P.orbOffset,center,open,name,previous,
   nameEnd:modelX+width,rateSize:rs,rateWidth,unitWidth,
   rateRight:finalRight,rulerOrigin,
   rateVisibility:smoother((open-.25)/.75),
   rulerStart:lerp(rulerOrigin,finalLeft,open),rulerEnd:lerp(rulerOrigin,finalRight,open),
   baseline:row.y+row.font*.36,guideY:row.y+row.font*.36+4.8,
   expandedRight:finalRight,groupLeft:modelX-P.orbExtent};
 }
 NS.RowMotion=RowMotion;NS.rowLayout=rowLayout;NS.rowTravel=travel;NS.RateTween=RateTween;
})(window.RA);
