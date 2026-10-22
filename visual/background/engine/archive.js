/* Reference mechanism: LBEILC/RhineLabUI (MIT), scene.ts select/outgoing,
 * motion.ts damp, archive-loop.ts. See references/ARCHIVE_REFERENCE.txt.
 * BG16 restores one selection event, shared-field response and finite pulses.
 * Bounded incoming-slot retargeting adapts the source to three live states.
 */
(function(NS){
 'use strict';
 const states=Object.freeze({
  green:{word:'RUNNING',caption:'运行良好',index:0,ink:'#506c58'},
  red:{word:'ANOMALY',caption:'出现问题',index:1,ink:'#976058'},
  yellow:{word:'WAITING',caption:'等待响应',index:2,ink:'#92743b'}
 });
 const inkColors=Object.values(states).map(({ink})=>[1,3,5].map(i=>parseInt(ink.slice(i,i+2),16)/255));
 const params=Object.freeze({rowSlots:48,columnSlots:6,laneStep:-1,rearSteps:4,lift:.4,trackRate:3.7,raiseRate:4.2,lowerRate:4.5,
  shoulderRate:5,focusRate:4,recordLimit:8,inputLimit:32,rebaseEvery:16,pulseLimit:6,pulseLife:3.2});
 const key=(lane,row)=>lane+':'+row,spring=v=>({value:v,velocity:0,target:v});
 function damp(s,target,rate,dt){const delta=s.value-target,impulse=s.velocity+rate*delta,decay=Math.exp(-rate*dt);s.value=target+(delta+impulse*dt)*decay;s.velocity=(s.velocity-rate*impulse*dt)*decay;}
 class Timeline{
  constructor(){this.events=[];this.history=[];this.reset();}
  reset(){this.time=0;this.cursor=0;this.state='green';this.active=true;this.cell={lane:1,row:7};this.started=-10;this.pending=null;this.originLane=0;this.originRow=0;this.rebases=0;this.pulses=[];this.lastInteraction=-10;this.idleGain=1;
   this.x=spring(0);this.z=spring(0);this.shoulder=spring(7);this.focus=spring(1);
   this.bankLane=0;this.bankRow=0;this.flatten=spring(0);
   this.records=new Map([[key(1,7),{lane:1,row:7,state:'green',previous:'green',inkMix:1,lift:spring(params.lift)}]]);
  }
  request(state,time=this.time){if(state!=='idle'&&!states[state])throw Error('Unknown synthetic archive state');
   // Manual mode retains a bounded diagnostic ring, not a replay of every click.
   this.seek(Math.max(this.time,time));this.events=[];this.cursor=0;this.begin(state);
   this.history.push({time:this.time,state});if(this.history.length>params.inputLimit)this.history.shift();return this.view();
  }
  demo(){this.events=[{time:4,state:'yellow'},{time:8,state:'red'},{time:12,state:'green'},{time:16,state:'idle'},{time:21,state:'green'}];this.history=[];this.reset();return this.view();}
  begin(state){
   if(state==='idle'){this.active=false;this.state='idle';this.pending=null;this.lastInteraction=this.time;this.flatten.target=1;for(const r of this.records.values())r.lift.target=0;return;}
   if(this.active&&this.state===state){this.pending=null;return;}
   // Repeated changes while a card is arriving retarget that same nearby card.
   // This bounds the distance ahead of the viewport without a fixed time lock.
   // A left incoming column and four slots of rearward array travel.
   const next=this.active?{lane:Math.round(1+this.x.value/5.2)+params.laneStep,row:Math.round(7-this.z.value/.62)+params.rearSteps}:{...this.cell};
   const id=key(next.lane,next.row),changed=next.lane!==this.cell.lane||next.row!==this.cell.row;
   if(!this.records.has(id)&&this.records.size>=params.recordLimit){this.pending=state;return;}
   if(this.active&&changed){for(const r of this.records.values())r.lift.target=0;this.cell=next;
    this.x.target=(next.lane-1)*5.2;this.focus.target=next.lane;
    this.z.target=-(next.row-7)*.62;this.shoulder.target=next.row;
    this.pulses.push({lane:next.lane,row:next.row,time:this.time});if(this.pulses.length>params.pulseLimit)this.pulses.shift();
   }
   this.active=true;this.state=state;this.started=this.time;this.pending=null;
   this.flatten.target=0;
   this.lastInteraction=this.time;
   const record=this.records.get(id)||{...this.cell,state,previous:state,inkMix:1,lift:spring(0)};
   if(record.state!==state){record.previous=record.state;record.inkMix=0;}record.state=state;record.lift.target=params.lift;this.records.set(id,record);
   if(this.records.size>params.recordLimit)throw Error('Archive record pool invariant violated');
  }
  step(dt){
   damp(this.x,this.x.target,params.trackRate,dt);damp(this.z,this.z.target,params.trackRate,dt);
   damp(this.shoulder,this.shoulder.target,params.shoulderRate,dt);damp(this.focus,this.focus.target,params.focusRate,dt);
   damp(this.flatten,this.flatten.target,5,dt);
   const idle=this.time>=this.lastInteraction+2.5;this.idleGain=(idle?1:0)+(this.idleGain-(idle?1:0))*Math.exp(-dt*(idle?.8:4));
   for(const [id,r]of this.records){damp(r.lift,r.lift.target,r.lift.target>0?params.raiseRate:params.lowerRate,dt);r.inkMix=1-(1-r.inkMix)*Math.exp(-dt*9);
    if(r.lift.target===0&&r.lift.value<.00001)this.records.delete(id);
   }this.time+=dt;this.pulses=this.pulses.filter(p=>this.time-p.time<=params.pulseLife);this.rebase();
  }
  rebase(){
   const n=params.rebaseEvery,dl=Math.trunc(this.x.value/(5.2*n))*n,dr=Math.trunc(-this.z.value/(.62*n))*n;
   if(!dl&&!dr)return;this.cell.lane-=dl;this.cell.row-=dr;
   this.x.value-=dl*5.2;this.x.target-=dl*5.2;this.focus.value-=dl;this.focus.target-=dl;
   this.z.value+=dr*.62;this.z.target+=dr*.62;this.shoulder.value-=dr;this.shoulder.target-=dr;
   for(const p of this.pulses){p.lane-=dl;p.row-=dr;}
   const rows=Array.from(this.records.values());this.records.clear();for(const r of rows){r.lane-=dl;r.row-=dr;this.records.set(key(r.lane,r.row),r);}
   this.originLane=(this.originLane+dl+1024)%1024;this.originRow=(this.originRow+dr+1024)%1024;this.rebases++;
   this.bankLane=((this.bankLane+dl)%params.columnSlots+params.columnSlots)%params.columnSlots;this.bankRow=((this.bankRow+dr)%params.rowSlots+params.rowSlots)%params.rowSlots;
  }
  seek(time){const end=Math.max(0,time);if(end<this.time)this.reset();let guard=0;
   while(guard++<1000){
    if(this.cursor<this.events.length&&this.events[this.cursor].time<=this.time+1e-9){this.begin(this.events[this.cursor++].state);continue;}
    if(this.pending&&this.records.size<params.recordLimit){const target=this.pending;this.pending=null;this.begin(target);continue;}
    if(this.time>=end-1e-9)break;
    const idleAt=this.lastInteraction+2.5,next=Math.min(end,this.events[this.cursor]?.time??Infinity,idleAt>this.time+1e-9?idleAt:Infinity);
    this.step(Math.max(0,next-this.time));
   }if(guard>=1000)throw Error('Archive timeline failed to advance');return this.view();
  }
  view(){return{time:this.time,state:this.state,active:this.active,pending:this.pending,cell:{...this.cell},
   trackX:this.x.value,rail:this.z.value,railVelocity:this.z.velocity,shoulder:this.shoulder.value,laneFocus:this.focus.value,originLane:this.originLane,originRow:this.originRow,rebases:this.rebases,
   idleGain:this.idleGain,pulses:this.pulses.map(p=>({...p})),lastInteraction:this.lastInteraction,
   bankLane:this.bankLane,bankRow:this.bankRow,flatten:this.flatten.value>.9999?1:this.flatten.value<.0001?0:this.flatten.value,
   records:Array.from(this.records.values(),r=>({lane:r.lane,row:r.row,state:r.state,previous:r.previous,inkMix:r.inkMix,lift:r.lift.value,target:r.lift.target}))};}
 }
 class Atlas{
  constructor(gl){this.width=512;this.height=1152;const c=document.createElement('canvas');c.width=this.width;c.height=this.height;const ctx=c.getContext('2d');
   function tracked(text,x,y,tracking){for(const ch of text){ctx.fillText(ch,x,y);x+=ctx.measureText(ch).width+tracking;}}
   for(const data of Object.values(states)){const top=data.index*384;ctx.save();ctx.translate(0,top);ctx.fillStyle=data.ink;
    ctx.font='500 30px "Segoe UI",Arial,sans-serif';tracked(data.word,70,43,2.0);
    ctx.globalAlpha=.66;ctx.font='400 11px "Segoe UI",Arial,sans-serif';tracked('SYSTEM  /  '+String(data.index+1).padStart(2,'0'),71,67,1.35);
    ctx.globalAlpha=.38;ctx.fillRect(71,79,110,.85);ctx.restore();
   }
   this.canvas=c;this.texture=gl.createTexture();gl.bindTexture(gl.TEXTURE_2D,this.texture);
   gl.pixelStorei(gl.UNPACK_PREMULTIPLY_ALPHA_WEBGL,false);gl.pixelStorei(gl.UNPACK_FLIP_Y_WEBGL,false);
   gl.texImage2D(gl.TEXTURE_2D,0,gl.RGBA,gl.RGBA,gl.UNSIGNED_BYTE,c);
   gl.texParameteri(gl.TEXTURE_2D,gl.TEXTURE_MIN_FILTER,gl.LINEAR);gl.texParameteri(gl.TEXTURE_2D,gl.TEXTURE_MAG_FILTER,gl.LINEAR);
   gl.texParameteri(gl.TEXTURE_2D,gl.TEXTURE_WRAP_S,gl.CLAMP_TO_EDGE);gl.texParameteri(gl.TEXTURE_2D,gl.TEXTURE_WRAP_T,gl.CLAMP_TO_EDGE);
  }
 }
 NS.archive={Timeline,Atlas,states,inkColors,params,damp,key};
})(window.BG16=window.BG16||{});
