/* Single owner of appearance, archive transport and local motion time.
 * Current clip/transport completes, then the latest target is consumed.
 * Automatic snapshots and preview controls share this bounded intent mailbox.
 */
(function(NS){
 'use strict';
 const P=Object.freeze({clockRate:10,readyRate:.995,positionEpsilon:.015,velocityEpsilon:.045,liftEpsilon:.006});
 const clamp=x=>Math.max(0,Math.min(1,x));let serial=0;
 class Dynamics{
  constructor(){
   this.id=++serial;this.generation=0;this.appearance=new NS.appearance.Controller();
   this.archive=new NS.archive.Timeline();this.desiredState='green';this.desiredShown=true;this.rate={value:1,velocity:0,target:1};
   this.replay=false;this.lightTime=0;this.commands=0;this.applied=0;
  }
  requestState(state){
   if(state!=='idle'&&!NS.archive.states[state])throw Error('Unknown glass state');
   if(this.desiredState===state)return;
   this.desiredState=state;this.generation++;this.commands++;
  }
  show(){this.replay=false;this.desiredShown=true;this.generation++;}
  hide(){this.replay=false;this.desiredShown=false;this.generation++;}
  replayAppearance(){this.replay=true;this.desiredShown=true;this.generation++;}
  immediate(shown){
   this.replay=false;this.desiredShown=shown;this.appearance.immediate(shown);
   this.rate.value=shown?1:0;this.rate.velocity=0;this.generation++;
  }
  settled(){
   const a=this.archive;
   return Math.abs(a.x.target-a.x.value)<P.positionEpsilon&&Math.abs(a.z.target-a.z.value)<P.positionEpsilon&&
    Math.abs(a.x.velocity)<P.velocityEpsilon&&Math.abs(a.z.velocity)<P.velocityEpsilon&&
    Math.abs(a.flatten.target-a.flatten.value)<P.liftEpsilon&&
    [...a.records.values()].every(r=>Math.abs(r.lift.target-r.lift.value)<P.liftEpsilon&&Math.abs(r.lift.velocity)<P.velocityEpsilon);
  }
  update(dt,allowArchive=true){
   dt=Math.max(0,Math.min(.1,dt));
   // Only settled endpoints admit an operation. An in-flight entry, exit or
   // archive roll owns its complete duration, regardless of new target data.
   if(this.appearance.mode==='hidden'&&this.desiredShown){
    this.replay=false;this.appearance.request('enter');this.generation++;
   }else if(this.appearance.mode==='visible'&&this.settled()){
    if(!this.desiredShown||this.replay){
     this.replay=false;this.appearance.request('exit');this.generation++;
    }else if(allowArchive&&this.rate.value>P.readyRate&&this.archive.state!==this.desiredState){
     this.archive.request(this.desiredState,this.archive.time);this.applied++;this.generation++;
    }
   }
   this.appearance.update(dt);
   const visible=this.appearance.mode==='visible';
   this.rate.target=visible?1:0;NS.archive.damp(this.rate,this.rate.target,P.clockRate,dt);
   const speed=clamp(this.rate.value);
   if(this.appearance.mode!=='hidden')this.lightTime+=dt;
   // Rendering and camera changes never seek or reset this owned timeline.
   this.archive.seek(this.archive.time+dt*speed);
   return this;
  }
  view(){
   const a=this.archive.view(),speed=clamp(this.rate.value);
   a.railVelocity*=speed;a.idleGain*=.10+.90*speed;
   return{archive:a,geometryTime:this.archive.time,lightTime:this.lightTime,appearance:this.appearance,interactionGain:speed,
    state:a.state,desiredState:this.desiredState,pending:a.state===this.desiredState?null:this.desiredState,desiredShown:this.desiredShown,
    phase:this.appearance.mode!=='visible'?'glass-'+this.appearance.mode:!this.settled()?'archive-transition':'steady',
    rate:speed,replay:this.replay,commands:this.commands,applied:this.applied};
  }
 }
 NS.dynamics={Dynamics,params:P};
})(window.BG16);
