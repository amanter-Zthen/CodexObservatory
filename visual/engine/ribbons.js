// Two persistent currents + two paired detail tiers at 2 and 4 sessions.
// Original packed renderer interface and tuned wave kernel retained.
(function(NS){
 'use strict';
 const {clamp01,smoother,strHash,Spring}=NS.core,P=NS.ribbonArt.P;
 const PACKED=12;
 class Ribbons{
  constructor(){
   this.A=new Float32Array(PACKED*4);this.B=new Float32Array(PACKED*4);this.C=new Float32Array(PACKED*4);
   this.D=new Float32Array(PACKED);this.field=new Float32Array(16);this.phase=new Float64Array(PACKED);
   for(let i=0;i<PACKED;i++)this.phase[i]=i*1.37;
   this.H=112;this.slots=[];this.epoch=0;this.liveCount=0;this.targetDetails=0;
   // Fixed slots retain phase and velocity during rapid threshold reversals.
   this.details=P.detailThresholds.map((threshold,i)=>({
    threshold,amount:new Spring(0,.85,1),phase:3.1+i*2.7,flow:new Spring(.35,1.1,1),
    seed:.83+i*.77,dir:i%2===0?-1:1
   }));
   this.detailDensity=new Spring(0,.75,1);
  }
  sync(rows,now){
   const epoch=++this.epoch;let live=0,energy=0,flow=0;
   for(const r of rows){
    if(!r.alive)continue;live++;
    const rate=!r.outputting?.35:r.rate<12?.6:r.rate<35?.9:1.25;
    flow+=rate;energy+=r.energy||0;
    let s=this.slots.find(q=>q.id===r.id);
    if(!s&&this.slots.length<P.maxFlowSources){
     const seed=strHash(r.id);
     s={id:r.id,seen:epoch,left:-1,y:new Spring(r.y,.9,1),seed,phase:seed*6.28,flow:new Spring(.35,1.1,1)};
     this.slots.push(s);
    }
    if(s){s.seen=epoch;s.left=-1;s.y.set(r.y);s.flow.set(rate);}
   }
   for(const s of this.slots)if(s.seen!==epoch&&s.left<0)s.left=now;
   this.liveCount=live;this.targetDetails=P.detailThresholds.filter(n=>live>=n).length;
   this.detailDensity.set(this.targetDetails/this.details.length);
   for(const s of this.details){s.amount.set(live>=s.threshold?1:0);s.flow.set(live?flow/live:.35);s.energy=live?energy/live:0;}
  }
  update(dt,now,H,stage,T,quality,breathe,totalEnergy){
   this.H=H;
   for(let i=this.slots.length-1;i>=0;i--){
    const s=this.slots[i];
    if(s.left>=0&&now-s.left>1.6){this.slots.splice(i,1);continue;}
    s.currentY=s.y.step(dt);s.phase+=dt*s.flow.step(dt);
   }
   for(let j=0;j<16;j++){
    const y=H*j/15;let sum=0,weight=0;
    for(const s of this.slots){
     const op=s.left<0?1:1-smoother((now-s.left)/1.45),d=(y-s.currentY)/Math.max(35,H*.28),w=op/(1+d*d);
     sum+=Math.sin(s.phase)*w;weight+=w;
    }
    this.field[j]=weight>0?sum/weight:0;
   }
   this.detailAmount=this.detailDensity.step(dt);
   // First TWO original G02 currents. Widths stay independent of row/font count.
   const heroes=[
    [.60,Math.min(48,.15*H),5.40,P.mainHalfWidth,.12,1,1],
    [.42,Math.min(48,.12*H),5.83,P.secondHalfWidth,.45,-1,P.secondOpacity]
   ];
   for(let i=0;i<P.coreCount;i++){
    const h=heroes[i],w=stage.ribbonWindow(i,T);
    this.phase[i]+=dt*.76;
    const breath=1+.18*Math.sin(breathe*(1+.21*i)+i*2.1);
    this.put(i,H*h[0],h[1]*breath,h[2],this.phase[i],h[3],h[4],h[5],i*.31,w.head,w.tail,h[6],.35+.65*totalEnergy);
   }
   let n=P.coreCount;
   for(let j=0;j<this.details.length;j++){
    const s=this.details[j],i=P.coreCount+j,a=clamp01(s.amount.step(dt)),w=stage.ribbonWindow(i,T);
    s.phase+=dt*s.flow.step(dt);
    const presence=smoother(a),head=-.35+1.75*presence;
    this.put(i,H*P.detailTracks[j],6+.038*H,5.4+.43*(i%3),s.phase,
     P.detailHalfWidths[j],.55+j*.14,s.dir,s.seed,
     Math.min(w.head,head),w.tail,P.detailOpacities[j]*presence,s.energy||0);
    if(presence>.001)n=i+1;
   }
   for(let i=n;i<PACKED;i++)this.C[i*4+2]=0;
   this.count=n;
  }
  put(i,y,amp,k,ph,hw,depth,dir,seed,head,tail,op,energy){
   const pos=clamp01(y/this.H)*15,j=Math.min(14,Math.floor(pos)),f=pos-j,o=i*4;
   this.D[i]=.24*(this.field[j]*(1-f)+this.field[j+1]*f);
   this.A.set([y,amp,k,ph],o);this.B.set([hw,depth,dir,seed],o);
   this.C.set([head,tail,op*(tail<head?1:0),clamp01(energy)],o);
  }
 }
 NS.Ribbons=Ribbons;NS.RIBBON_MAX=P.maxCurves;
})(window.RA);
