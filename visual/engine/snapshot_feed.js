// The native host owns credentials, source generation and one HTTP request.
// The view owns one replaceable snapshot slot, never a backlog of old frames.
(function(NS){
 'use strict';
 const finite=(n,f=0)=>typeof n==='number'&&Number.isFinite(n)?n:f;
 function normalize(s){
  if(!s||typeof s!=='object'||(s.rows!=null&&!Array.isArray(s.rows)))return null;
  const ids=new Set(),rows=[];
  for(const r of (s.rows||[]).slice(0,256)){
   if(!r||typeof r.thread_id!=='string'||!r.thread_id||r.thread_id.length>160||ids.has(r.thread_id))continue;
   ids.add(r.thread_id);
   rows.push({...r,model:typeof r.model==='string'?r.model.slice(0,1024):'—',
    color:['red','green','yellow'].includes(r.color)?r.color:'yellow',
    outputting:r.outputting===true,speed_valid:r.speed_valid===true,speed_fresh:r.speed_fresh===true,
    health_valid:r.health_valid===true,turn_anomaly:r.turn_anomaly===true,
    output_activity:Math.max(0,finite(r.output_activity)),
    estimated_visible_tokens_per_second:Math.max(0,finite(r.estimated_visible_tokens_per_second)),
    tokens_per_second:Math.max(0,finite(r.tokens_per_second))});
  }
  const q=s.quota||{},valid=typeof q.remaining_percent==='number'&&Number.isFinite(q.remaining_percent)&&q.remaining_percent>=0&&q.remaining_percent<=100;
  return{...s,rows,ready:s.ready===true,quota:{...q,known:q.known===true&&valid,fresh:q.fresh===true,remaining_percent:valid?q.remaining_percent:0}};
 }
 class SnapshotFeed{
  constructor(wallNow=()=>Date.now()){this.wallNow=wallNow;this.pending=null;this.current=null;this.generation=-1;this.sequence=-1;this.received=-Infinity;this.expired=false;this.sourceAt=-Infinity;}
  receive(message,now){
   if(!message||typeof message!=='object'||!Number.isSafeInteger(message.generation)||message.generation<0||
     !Number.isSafeInteger(message.sequence)||message.sequence<0||!Number.isFinite(now))return false;
   const g=message.generation,seq=message.sequence;
   if(g<this.generation||(g===this.generation&&seq<=this.sequence))return false;
   const s=normalize(message.snapshot);if(!s)return false;
   const at=typeof s.at==='string'?Date.parse(s.at):NaN;
   if(!Number.isFinite(at)||(g===this.generation&&at<this.sourceAt))return false;
   this.sourceAt=at;this.generation=g;this.sequence=seq;
   this.pending=s;this.received=now;this.expired=false;return true;
  }
  take(now){
   let changed=false;
   if(this.pending){this.current=this.pending;this.pending=null;changed=true;}
   if(this.current&&!this.expired&&(now-this.received>10||this.wallNow()-this.sourceAt>10000)){
    this.expired=true;this.current={...this.current,ready:false,
     quota:{...this.current.quota,fresh:false},
     rows:this.current.rows.map(r=>({...r,color:'yellow',health_valid:false,outputting:false,speed_fresh:false}))};
    return this.current;
   }return changed?this.current:null;
  }
 }
 NS.SnapshotFeed=SnapshotFeed;NS.normalizeSnapshot=normalize;
})(window.RA);
