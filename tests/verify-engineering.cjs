'use strict';
// Isolated engineering regressions. No live monitor, credentials or network feed.
// GPU completion waits below are measurement barriers in this test only.
const fs=require('fs'),path=require('path'),vm=require('vm'),assert=require('assert');
const {performance}=require('perf_hooks'),{pathToFileURL}=require('url');
const root=path.resolve(__dirname,'..'),visual=path.join(root,'visual');
const out=path.join(root,'verification/engineering/invariants.json');
const report={at:new Date().toISOString(),scope:'Isolated Node contracts and actual Chrome/WebGL/Canvas rendering',productionMutation:'NONE',checks:[],errors:[],networkRequests:[]};
const began=performance.now();
async function check(name,fn){
 const start=performance.now();
 try{const evidence=await fn();report.checks.push({name,pass:true,elapsedMs:performance.now()-start,evidence});console.log('PASS',name);}
 catch(e){report.checks.push({name,pass:false,elapsedMs:performance.now()-start,error:String(e.stack||e)});console.error('FAIL',name,String(e.message||e));}
}
const box=vm.createContext({window:{},console,performance});
for(const file of ['engine/core.js','engine/presentation.js','engine/row_presentation.js','engine/ribbon_art.js',
 'engine/stage.js','engine/quota_motion.js','engine/ribbons.js','background/engine/archive.js',
 'background/engine/appearance.js','background/engine/dynamics.js','engine/animator.js','engine/snapshot_feed.js']){
 vm.runInContext(fs.readFileSync(path.join(visual,file),'utf8'),box,{filename:file});
}
const RA=box.window.RA;
// Keep source-age tests deterministic on every future run.
const FIXED_WALL=Date.parse('2026-10-21T01:00:00.000Z');
const makeFeed=(wallNow=()=>FIXED_WALL)=>new RA.SnapshotFeed(wallNow);
const row=(extra={})=>({thread_id:'thread-a',turn_id:'turn-a',response_id:'response-a',model:'model-a',
 requested_model:'model-a',reported_model:'model-a',route:'match',color:'green',activity:'running',
 health_valid:true,outputting:false,speed_valid:false,speed_fresh:false,turn_anomaly:false,activity_scope:'turn',...extra});
const snap=(extra={})=>({rows:[row()],quota:{known:true,fresh:true,remaining_percent:87},ready:true,mode:'observed',revision:17,at:'2026-10-21T01:00:00.000Z',...extra});
const message=(sequence,snapshot=snap(),generation=1)=>({kind:'snapshot',generation,sequence,snapshot});
function advance(a,seconds,visit=()=>{},quality=0){
 let scene;for(let i=0;i<Math.round(seconds*120);i++){scene=a.frame(1/120,quality);visit(scene);}return scene;
}
async function nodeChecks(){
 await check('feed-keeps-one-latest-pending-snapshot',()=>{
  const f=makeFeed();
  for(let i=0;i<10000;i++)assert(f.receive(message(i,snap({rows:[row({model:'latest-'+i})]})),i/100));
  assert(f.pending&&!Array.isArray(f.pending));assert.equal(f.current,null);
  assert.equal(f.take(100).rows[0].model,'latest-9999');assert.equal(f.pending,null);
  assert.equal(f.take(100),null);
  return{received:10000,pendingSlots:1,finalModel:f.current.rows[0].model};
 });
 await check('same-revision-time-dependent-fields-are-consumed',()=>{
  const f=makeFeed();f.receive(message(1),0);f.take(0);
  const stale=snap({at:'2026-10-21T01:00:11.000Z',ready:false,
   quota:{known:true,fresh:false,remaining_percent:87},
   rows:[row({color:'yellow',health_valid:false,outputting:false,speed_fresh:false})]});
  assert(f.receive(message(2,stale),11));const s=f.take(11);
  assert.equal(s.revision,17);assert.equal(s.ready,false);assert.equal(s.rows[0].color,'yellow');assert.equal(s.quota.fresh,false);
  return{revision:s.revision,at:s.at,ready:s.ready,color:s.rows[0].color};
 });
 await check('feed-rejects-old-sequence-time-and-connection-generation',()=>{
  const f=makeFeed();assert(f.receive(message(9),0));f.take(0);
  assert.equal(f.receive(message(8),1),false);
  assert.equal(f.receive(message(10,snap({at:'2026-10-21T00:59:59.000Z'})),1),false);
  assert(f.receive(message(0,snap({revision:1,at:'2026-10-21T01:00:02.000Z'}),2),2));
  assert.equal(f.take(2).revision,1);
  assert.equal(f.receive(message(100000,snap({at:'2026-10-21T01:00:05.000Z'}),1),3),false);
  assert.equal(f.current.revision,1);
  return{generation:f.generation,sequence:f.sequence,revision:f.current.revision};
 });
 await check('disconnect-expires-current-and-pending-snapshots-before-display',()=>{
  const current=makeFeed();current.receive(message(1,snap({rows:[row({outputting:true,speed_fresh:true})]})),0);current.take(0);
  const expired=current.take(10.01);
  assert.equal(expired.ready,false);assert.equal(expired.rows[0].color,'yellow');
  assert.equal(expired.rows[0].outputting,false);assert.equal(expired.quota.fresh,false);
  const pending=makeFeed();pending.receive(message(1,snap({rows:[row({outputting:true,speed_fresh:true})]})),0);
  const firstPaint=pending.take(10.01);
  assert.equal(firstPaint.ready,false,'A snapshot held while hidden must already be stale on its first paint');
  assert.equal(firstPaint.rows[0].outputting,false);
  return{currentExpired:true,hiddenPendingExpiredBeforeFirstPaint:true};
 });
 await check('late-delivery-after-suspension-does-not-refresh-old-source-evidence',()=>{
  let wall=FIXED_WALL+20000;
  const f=makeFeed(()=>wall);
  const old=snap({rows:[row({outputting:true,speed_valid:true,speed_fresh:true,tokens_per_second:18.8})]});
  assert(f.receive(message(1,old),100),'Delivery itself is ordered and valid');
  const stale=f.take(100);
  assert.equal(stale.ready,false);assert.equal(stale.rows[0].color,'yellow');
  assert.equal(stale.rows[0].health_valid,false);assert.equal(stale.rows[0].outputting,false);
  assert.equal(stale.rows[0].speed_fresh,false);assert.equal(stale.quota.fresh,false);
  const a=new RA.Animator();a.set(stale);a.showImmediate();
  assert.equal(advance(a,3).rows[0].rateOp,0,'An old completion must remain closed after late delivery');
  wall+=1000;
  const fresh=snap({at:new Date(wall).toISOString(),rows:[row({outputting:true,estimated_visible_tokens_per_second:31})]});
  assert(f.receive(message(2,fresh),101));
  const recovered=f.take(101);assert.equal(recovered.ready,true);assert.equal(recovered.rows[0].color,'green');
  return{sourceAgeAtDeliveryMs:20000,receiveAgeAtFirstPaintSeconds:0,
   staleColor:stale.rows[0].color,oldOutputClosed:true,recoveredColor:recovered.rows[0].color};
 });
 await check('snapshot-normalization-bounds-rows-and-finite-display-values',()=>{
  const source=snap({rows:[null,{},row({model:null,color:'unknown',tokens_per_second:Infinity,
   estimated_visible_tokens_per_second:-4,output_activity:NaN}),row(),
   ...Array.from({length:300},(_,i)=>row({thread_id:'extra-'+i}))],
   quota:{known:true,fresh:true,remaining_percent:Infinity}});
  const s=RA.normalizeSnapshot(source);
  assert(s.rows.length<=256);assert.equal(new Set(s.rows.map(r=>r.thread_id)).size,s.rows.length);
  assert.equal(s.rows[0].model,'—');assert.equal(s.rows[0].color,'yellow');
  for(const r of s.rows)for(const k of ['tokens_per_second','estimated_visible_tokens_per_second','output_activity'])assert(Number.isFinite(r[k])&&r[k]>=0);
  assert.equal(s.quota.known,false);assert.equal(RA.normalizeSnapshot({rows:'invalid'}),null);
  return{rows:s.rows.length,firstModel:s.rows[0].model,quotaKnown:s.quota.known};
 });
 await check('malformed-bridge-messages-are-rejected-without-throwing',()=>{
  const f=makeFeed();
  assert.doesNotThrow(()=>assert.equal(f.receive(null,0),false));
  assert.equal(f.receive({generation:NaN,sequence:Infinity,snapshot:snap()},0),false);
  return{nullMessageRejected:true,invalidSequenceRejected:true};
 });
 await check('string-boolean-fields-do-not-become-trusted',()=>{
  const s=RA.normalizeSnapshot(snap({ready:'true',rows:[row({outputting:'false',speed_fresh:'false',speed_valid:'false',health_valid:'true'})],
   quota:{known:'false',fresh:'false',remaining_percent:87}}));
  assert.equal(s.ready,false);assert.equal(s.rows[0].outputting,false);
  assert.equal(s.rows[0].speed_fresh,false);assert.equal(s.rows[0].speed_valid,false);assert.equal(s.rows[0].health_valid,false);
  assert.equal(s.quota.known,false);assert.equal(s.quota.fresh,false);
  return{stringBooleansUntrusted:true};
 });
 await check('invalid-source-time-cannot-erase-ordering-watermark',()=>{
  const f=makeFeed();assert(f.receive(message(1),0));f.take(0);
  assert.equal(f.receive(message(2,snap({at:'invalid-date'})),1),false);
  assert.equal(f.receive(message(3,snap({at:'2026-10-20T23:59:00.000Z'})),2),false);
  return{sourceAt:f.sourceAt};
 });
 await check('completed-rate-is-presented-once-despite-repeated-fresh-snapshots',()=>{
  const a=new RA.Animator();a.set(snap());a.showImmediate();advance(a,3);
  const completed=snap({rows:[row({outputting:false,speed_valid:true,speed_fresh:true,tokens_per_second:18.8,ended_at:'2026-10-21T01:00:00.000Z'})]});
  let openings=0,previous='closed',maxOpen=0;
  for(let i=0;i<12*120;i++){
   if(i%12===0)a.set(completed);
   const s=a.frame(1/120,0),p=[...a.poses.values()][0];
   if(p.rowMotion.phase==='opening'&&previous!=='opening')openings++;
   previous=p.rowMotion.phase;maxOpen=Math.max(maxOpen,s.rows[0].rateOp);
  }
  assert.equal(openings,1);assert.equal(maxOpen,1);assert.equal(previous,'closed');
  assert.equal([...a.poses.values()][0].completion,null);
  return{refreshes:120,openings,maxOpen,finalPhase:previous};
 });
 await check('next-response-does-not-inherit-previous-completion-latch',()=>{
  const a=new RA.Animator();a.set(snap());a.showImmediate();advance(a,3);
  a.set(snap({rows:[row({speed_valid:true,speed_fresh:true,tokens_per_second:18.8})]}));advance(a,.5);
  a.set(snap({rows:[row({turn_id:'turn-b',response_id:'response-b',speed_valid:false,speed_fresh:false})]}));
  const s=advance(a,3),p=[...a.poses.values()][0];
  assert.equal(p.completion,null);assert.equal(s.rows[0].rateOp,0);
  a.set(snap({rows:[row({turn_id:'turn-b',response_id:'response-b',outputting:true,estimated_visible_tokens_per_second:31})]}));
  const streaming=advance(a,3);
  assert(Math.abs(streaming.rows[0].rate-31)<.01);assert.equal(streaming.rows[0].rateOp,1);
  return{oldTailCleared:true,newStreamingRate:streaming.rows[0].rate};
 });
 await check('static-mode-freezes-decoration-while-data-and-reopen-continue',()=>{
  const a=new RA.Animator();a.set(snap());a.showImmediate();advance(a,4);
  const phase=Array.from(a.ribbons.phase),motionTime=a.motionTime,breathe=a.breathe;
  a.setReducedMotion(true);
  a.set(snap({rows:Array.from({length:3},(_,i)=>row({thread_id:'static-'+i,model:'new-'+i,color:i?'green':'red'})),
   quota:{known:true,fresh:true,remaining_percent:0}}));
  const s=advance(a,3);
  assert.equal(s.rows.length,3);assert.equal(s.rows[0].model,'new-0');assert.equal(s.rows[0].color,'red');
  assert.equal(s.quota.text,'0%');assert.deepEqual(Array.from(a.ribbons.phase),phase);assert.equal(a.motionTime,motionTime);assert.equal(a.breathe,breathe);
  a.beginExit();a.frame(.1,3);assert(a.stage.hidden);
  a.beginReveal();const reopened=a.frame(.1,3);assert.equal(reopened.stage.hidden,false);assert.equal(reopened.rows.length,3);
  a.setReducedMotion(false);a.frame(.1,0);assert(a.motionTime>motionTime);
  return{rows:s.rows.length,quota:s.quota.text,phasesFrozen:true,reopened:true};
 });
}
async function browserChecks(){
 const {chromium,chromePath}=require('./runtime.cjs');
 const browser=await chromium.launch({executablePath:chromePath,headless:true});
 try{
  const page=await browser.newPage({viewport:{width:1000,height:900},deviceScaleFactor:1});
  page.setDefaultTimeout(15000);
  page.on('pageerror',e=>report.errors.push(String(e)));
  page.on('request',r=>{if(/^https?:/.test(r.url()))report.networkRequests.push(r.url());});
  const url=pathToFileURL(path.join(visual,'index.html')).href+'?pause&auto=0&count=16&output=on&zoom=1&dpr=1';
  await page.goto(url);await page.waitForFunction(()=>window.RA?.debug);
  await page.evaluate(()=>{
   RA.debug.advance(6);
   window.engineeringScene=()=>{
    const a=new RA.Animator();a.set(RA.synth(0,{count:16,mix:'green',output:'on',quota:'87'}));a.showImmediate();
    let s;for(let i=0;i<600;i++)s=a.frame(1/120,0);
    s.dt=0;s.motionDt=0;return s;
   };
  });
  await check('hundred-thousand-character-name-builds-only-visible-sdfs',async()=>{
   const v=await page.evaluate(()=>{
    const s=engineeringScene();s.rows=s.rows.slice(0,1);s.visible=s.total=1;
    s.rows[0].model=Array.from({length:100000},(_,i)=>String.fromCodePoint(0x4e00+i%20000)).join('');
    const a=new RA.Atlas(),l=new RA.Labels(a);let visits=0;
    const adv=a.advance;a.advance=function(...args){visits++;return adv.apply(this,args);};
    const before=performance.now();l.build(s);
    return{inputCodepoints:100000,displayCodepoints:Array.from(l.rowLayouts[0].name.text).length,
     sdfBuilds:a.built,widthVisits:visits,widthCache:a.widths.size,glyphs:l.n,elapsedMs:performance.now()-before};
   });
   assert(v.displayCodepoints<=64);assert(v.sdfBuilds<=128);assert(v.widthVisits<10000);assert(v.widthCache<256);
   return v;
  });
  await check('sixteen-long-renaming-rows-all-retain-name-and-speed-glyphs',async()=>{
   const v=await page.evaluate(()=>{
    const s=engineeringScene(),a=new RA.Atlas(),l=new RA.Labels(a),runs=[];
    for(const r of s.rows)Object.assign(r,{model:'i'.repeat(120),prev:'l'.repeat(120),mix:.5,rateOp:1,rateText:'999.9',reveal:1,dissolve:0});
    const original=l.text;
    l.text=function(...args){const start=this.n,collecting=this.collecting,value=original.apply(this,args);
     if(!collecting&&['name','rate'].includes(args[0]))runs.push({key:args[0],text:args[1],glyphs:this.n-start,baseline:args[3]});return value;};
    l.build(s);
    return{rows:s.rows.length,glyphs:l.n,limit:RA.GLYPH_MAX,lamps:l.orbN,
     nameRuns:runs.filter(r=>r.key==='name'),speedRuns:runs.filter(r=>r.key==='rate'&&r.text==='999.9'),
     zeroRuns:runs.filter(r=>r.glyphs===0)};
   });
   assert.equal(v.rows,16);assert.equal(v.lamps,16);assert.equal(v.nameRuns.length,32);assert.equal(v.speedRuns.length,16);
   assert.equal(v.zeroRuns.length,0);assert(v.glyphs<=v.limit);
   return{rows:v.rows,lamps:v.lamps,glyphs:v.glyphs,limit:v.limit,nameRuns:v.nameRuns.length,speedRuns:v.speedRuns.length,zeroRuns:v.zeroRuns.length};
  });
  await check('two-unicode-atlas-generations-match-a-clean-atlas-in-real-pixels',async()=>{
   const v=await page.evaluate(()=>{
    const s=engineeringScene(),a=new RA.Atlas(),l=new RA.Labels(a),d=RA.debug,g=d.renderer.gl;
    let rounds=0;
    for(;rounds<14&&a.generation<2;rounds++){
     for(let i=0;i<s.rows.length;i++)Object.assign(s.rows[i],{
      model:Array.from({length:12},(_,j)=>String.fromCodePoint(0x4e00+rounds*192+i*12+j)).join(''),prev:'',mix:1,rateText:'999.9'});
     l.build(s);d.renderer.uploadAtlas(a);
    }
    const shape=labels=>Array.from(labels.buf.subarray(0,labels.n*RA.GLYPH_FLOATS))
     .filter((_,i)=>i%RA.GLYPH_FLOATS<4||i%RA.GLYPH_FLOATS>=8);
    const agedShape=shape(l),fresh=new RA.Atlas(),fl=new RA.Labels(fresh);fl.build(s);
    const sameLayout=JSON.stringify(agedShape)===JSON.stringify(shape(fl));
    const oldLayers={...RA.layers.enabled};for(const key of Object.keys(RA.layers.enabled))RA.layers.enabled[key]=key==='info';
    const paint=(atlas,labels)=>{
     g.finish();d.renderer.ready();d.renderer.uploadAtlas(atlas);d.renderer.drift=0;
     d.renderer.render(s,labels,{count:0,inst:new Float32Array(0)},{scale:1,quality:0,glow:false,backdrop:'legacy',glassFocus:true});
     g.finish();d.renderer.ready();const pixels=new Uint8Array(g.drawingBufferWidth*g.drawingBufferHeight*4);
     g.readPixels(0,0,g.drawingBufferWidth,g.drawingBufferHeight,g.RGBA,g.UNSIGNED_BYTE,pixels);return pixels;
    };
    // Final aged image was uploaded during the bounded churn above.
    const aged=paint(a,l),clean=paint(fresh,fl);
    let maxDifference=0,differentChannels=0,nonzero=0;
    for(let i=0;i<aged.length;i++){const delta=Math.abs(aged[i]-clean[i]);maxDifference=Math.max(maxDifference,delta);if(delta)differentChannels++;if(aged[i])nonzero++;}
    Object.assign(RA.layers.enabled,oldLayers);
    return{rounds,generations:a.generation,built:a.built,entries:a.map.size,sameLayout,nonzero,differentChannels,maxDifference,glError:g.getError()};
   });
   assert(v.generations>=2);assert(v.sameLayout);assert(v.nonzero>100);assert(v.maxDifference<=3);assert.equal(v.glError,0);
   return v;
  });
  await check('time-sliced-glyph-preparation-publishes-only-complete-label-frames',async()=>{
   const v=await page.evaluate(()=>{
    const s=engineeringScene(),a=new RA.Atlas(),l=new RA.Labels(a);
    for(let i=0;i<s.rows.length;i++)s.rows[i].model=Array.from({length:12},(_,j)=>String.fromCodePoint(0x6000+i*12+j)).join('');
    const zero=l.build(s,0),zeroReady=l.ready,zeroBuilt=a.built;
    let steps=0,partialPublished=0,maxStepMs=0;
    do{const start=performance.now(),count=l.build(s,4);maxStepMs=Math.max(maxStepMs,performance.now()-start);if(!l.ready&&count)partialPublished++;steps++;}while(!l.ready&&steps<200);
    return{zero,zeroReady,zeroBuilt,steps,ready:l.ready,glyphs:l.n,partialPublished,maxStepMs};
   });
   assert.equal(v.zero,0);assert.equal(v.zeroReady,false);assert.equal(v.zeroBuilt,0);
   assert(v.ready);assert(v.steps<200);assert.equal(v.partialPublished,0);assert(v.glyphs>0);
   return v;
  });
  await check('busy-gpu-submits-no-extra-draws-and-feed-keeps-latest-state',async()=>{
   // ?pause is an explicit synchronous inspection mode. Use the ordinary host
   // and its user-facing pause API, which retains real GPU backpressure.
   const p=await browser.newPage({viewport:{width:700,height:750},deviceScaleFactor:1});
   p.on('pageerror',e=>report.errors.push(String(e)));
   await p.goto(url.replace('?pause&','?'));await p.waitForFunction(()=>window.RA?.debug);
   await p.evaluate(()=>{RA.debug.setPlaying(false);RA.debug.advance(6);});
   let v;try{v=await p.evaluate(async()=>{
    const d=RA.debug,g=d.renderer.gl;g.finish();d.renderer.ready();d.redraw();
    const wait=g.clientWaitSync,draw=g.drawArrays,instanced=g.drawArraysInstanced,upload=g.texSubImage2D;
    let draws=0,uploads=0;const waits=[];
    g.clientWaitSync=function(sync,flags,timeout){waits.push({flags,timeout});return this.TIMEOUT_EXPIRED;};
    g.drawArrays=function(...args){draws++;return draw.apply(this,args);};
    g.drawArraysInstanced=function(...args){draws++;return instanced.apply(this,args);};
    g.texSubImage2D=function(...args){uploads++;return upload.apply(this,args);};
    const before=d.renderer.submitted,beforeSkips=d.renderer.skipped,beforeTime=d.anim.t;let held;
    try{
     for(let i=0;i<5000;i++){
      const s=RA.synth(0,{count:1,mix:'green',output:'off',quota:'87'});
      s.at=new Date().toISOString();s.rows[0].model='busy-'+i;
      d.feed.receive({generation:2,sequence:i,snapshot:s},performance.now()/1000);
     }
     for(let i=0;i<30;i++)d.advance(1/30,1/30);
     held={draws,uploads,submissions:d.renderer.submitted-before,skips:d.renderer.skipped-beforeSkips,
      waits:waits.length,allNonblocking:waits.every(w=>w.flags===0&&w.timeout===0),pending:d.feed.pending,
      currentModel:d.feed.current.rows[0].model,animatorModel:d.anim.snapshot.rows[0].model,motionDuringBusy:d.anim.t-beforeTime};
    }finally{g.clientWaitSync=wait;g.drawArrays=draw;g.drawArraysInstanced=instanced;g.texSubImage2D=upload;}
    g.finish();await new Promise(resolve=>requestAnimationFrame(()=>requestAnimationFrame(resolve)));d.renderer.ready();d.redraw();
    return{...held,recoveredModel:d.anim.snapshot.rows[0].model,heldFrame:d.heldFrame,playing:d.playing,stats:{...d.presentationStats}};
   });}finally{await p.close();}
   assert.equal(v.draws,0);assert.equal(v.uploads,0);assert.equal(v.submissions,0);assert(v.skips>0);assert(v.allNonblocking);
   assert.equal(v.pending,null);assert.equal(v.currentModel,'busy-4999');assert.equal(v.motionDuringBusy,0);assert.equal(v.recoveredModel,'busy-4999',JSON.stringify(v));
   return v;
  });
  await check('unresponsive-gpu-falls-back-with-a-nonblocking-completion-check',async()=>{
   const p=await browser.newPage({viewport:{width:700,height:750},deviceScaleFactor:1});
   p.on('pageerror',e=>report.errors.push(String(e)));
   await p.goto(url.replace('?pause&','?'));await p.waitForFunction(()=>window.RA?.debug);
   let v;try{v=await p.evaluate(async()=>{
    const d=RA.debug;d.setPlaying(false);d.advance(6);
    const renderer=d.renderer,g=renderer.gl;g.finish();renderer.ready();d.redraw();
    const wait=g.clientWaitSync,waits=[];
    g.clientWaitSync=function(sync,flags,timeout){waits.push({flags,timeout});return this.TIMEOUT_EXPIRED;};
    renderer.pendingSince=performance.now()-3001;
    try{
     const before=performance.now();d.advance(.1);
     const elapsedMs=performance.now()-before;
     let microtask=false;await Promise.resolve().then(()=>{microtask=true;});
     const s=RA.synth(0,{count:1,mix:'red',output:'off',quota:'0'});
     s.at=new Date().toISOString();s.rows[0].model='timeout-latest';
     d.feed.receive({generation:7,sequence:1,snapshot:s},performance.now()/1000);d.advance(3);
     return{fallback:d.renderer.fallback,reason:d.renderer.reason,elapsedMs,
      allNonblocking:waits.every(w=>w.flags===0&&w.timeout===0),checks:waits.length,
      microtaskCompleted:microtask,model:d.anim.snapshot.rows[0].model,
      fallbackSubmissions:d.renderer.submitted};
    }finally{g.clientWaitSync=wait;}
   });}finally{await p.close();}
   assert(v.fallback);assert(/queue|responding/i.test(v.reason));assert(v.checks>0);
   assert(v.allNonblocking);assert(v.microtaskCompleted);assert.equal(v.model,'timeout-latest');assert(v.fallbackSubmissions>0);
   return v;
  });
  await check('runtime-graphics-failure-switches-to-live-canvas-fallback',async()=>{
   const v=await page.evaluate(()=>{
    const d=RA.debug;d.renderer.render=()=>{throw Error('isolated test: drawing failed');};
    d.advance(.1);const fallback=d.renderer.fallback,oldSubmitted=d.renderer.submitted;
    const s=RA.synth(0,{count:3,mix:'red',output:'off',quota:'0'});
    s.at=new Date().toISOString();
    d.feed.receive({generation:3,sequence:1,snapshot:s},performance.now()/1000);d.advance(5);
    return{fallback,rows:d.anim.snapshot.rows.length,quota:d.anim.snapshot.quota.remaining_percent,
     newSubmissions:d.renderer.submitted-oldSubmitted,reason:d.renderer.reason};
   });
   assert(v.fallback);assert.equal(v.rows,3);assert.equal(v.quota,0);assert(v.newSubmissions>0);
   return v;
  });
  await check('webgl-creation-failure-retains-readable-live-display',async()=>{
   const p=await browser.newPage({viewport:{width:700,height:750},deviceScaleFactor:1});
   const errors=[];p.on('pageerror',e=>errors.push(String(e)));
   await p.addInitScript(()=>{
    const get=HTMLCanvasElement.prototype.getContext;
    HTMLCanvasElement.prototype.getContext=function(type,...args){return type==='webgl2'?null:get.call(this,type,...args);};
    window.__fallbackText=[];
    const text=CanvasRenderingContext2D.prototype.fillText;
    CanvasRenderingContext2D.prototype.fillText=function(...args){window.__fallbackText.push(args[0]);return text.apply(this,args);};
   });
   await p.goto(url);await p.waitForFunction(()=>window.RA?.debug);
   const v=await p.evaluate(()=>{
    const d=RA.debug;d.advance(6);
    const s=RA.synth(0,{count:1,mix:'red',output:'off',quota:'0'});s.at=new Date().toISOString();s.rows[0].model='fallback-live';
    d.feed.receive({generation:1,sequence:1,snapshot:s},performance.now()/1000);d.advance(5);
    return{fallback:d.renderer.fallback,rows:d.anim.snapshot.rows.length,model:d.anim.snapshot.rows[0].model,
     visibleName:__fallbackText.includes('fallback-live'),visibleFallbackNotice:__fallbackText.some(t=>t.includes('数据继续更新'))};
   });
   await p.close();assert(v.fallback);assert(v.visibleName);assert(v.visibleFallbackNotice);assert.deepEqual(errors,[]);
   return{...v,errors};
  });
  await check('isolated-browser-checks-have-no-unhandled-errors-or-http-requests',()=>{
   assert.deepEqual(report.errors,[]);assert.deepEqual(report.networkRequests,[]);
   return{errors:report.errors,requests:report.networkRequests};
  });
 }finally{await browser.close();}
}
(async()=>{await nodeChecks();await browserChecks();})()
 .catch(e=>{report.errors.push(String(e.stack||e));console.error(e);})
 .finally(()=>{
  report.elapsedMs=performance.now()-began;report.passed=report.checks.filter(c=>c.pass).length;
  report.failed=report.checks.filter(c=>!c.pass).length;report.result=report.failed||report.errors.length?'FAIL':'PASS';
  fs.mkdirSync(path.dirname(out),{recursive:true});fs.writeFileSync(out,JSON.stringify(report,null,2));
  console.log(JSON.stringify({result:report.result,passed:report.passed,failed:report.failed,elapsedMs:report.elapsedMs}));
  if(report.result!=='PASS')process.exitCode=1;
 });
