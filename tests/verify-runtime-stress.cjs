'use strict';
// Two minutes of real rAF playback. Synthetic data only; no production endpoint.
// The GPU-busy segment injects TIMEOUT_EXPIRED rather than overloading the device.
const fs=require('fs'),path=require('path'),crypto=require('crypto'),assert=require('assert');
const {pathToFileURL}=require('url'),{chromium,chromePath}=require('./runtime.cjs');
const root=path.resolve(__dirname,'..'),visual=path.join(root,'visual');
const output=path.join(root,'verification/engineering/runtime-stress.json');
const files=[...fs.readFileSync(path.join(visual,'index.html'),'utf8').matchAll(/<script src="([^"]+)"/g)].map(m=>m[1]);
const hashes=()=>Object.fromEntries(files.map(f=>[f,crypto.createHash('sha256').update(fs.readFileSync(path.join(visual,f))).digest('hex')]));
const result={started:new Date().toISOString(),durationTargetSeconds:120,sourceHashes:hashes(),runtimeErrors:[],externalRequests:[],heapSamples:[],
 method:'Cold-start preparation recorded separately, then wait for RA.debug.warmed before 120 seconds of actual 30 fps-capped rAF playback with ordinary ready() backpressure; 5 Hz in-memory SnapshotFeed inputs; no ?pause harness, gl.finish or readPixels during playback',
 limitations:['One Chrome headless run on the current machine; another isolated WebView2 test may run briefly during this period.',
  'Busy-return injection exercises queue/recovery behavior without deliberately saturating the GPU.',
  'GL object counts are measured; actual GPU memory bytes are unavailable.',
  'A two-minute bounded test cannot establish that the machine can never freeze.'],
 phases:[
  {seconds:[0,8],name:'initial-playback-stable-16'},
  {seconds:[8,38],name:'five-hz-full-row-replacement'},
  {seconds:[38,50],name:'still-mode-with-live-data'},
  {seconds:[50,60],name:'animated-row-replacement'},
  {seconds:[60,66],name:'ordinary-pause-with-live-input'},
  {seconds:[66,74],name:'resume-with-latest-data'},
  {seconds:[74,75],name:'injected-gpu-busy'},
  {seconds:[75,90],name:'stable-target-recovery'},
  {seconds:[90,112],name:'five-hz-row-replacement'},
  {seconds:[112,120],name:'final-stable-target'}
 ]};
const sleep=ms=>new Promise(resolve=>setTimeout(resolve,ms));
(async()=>{
 let browser;
 try{
  browser=await chromium.launch({executablePath:chromePath,headless:true});
  const page=await browser.newPage({viewport:{width:1400,height:1500},deviceScaleFactor:1});
  page.on('pageerror',e=>result.runtimeErrors.push(String(e)));
  page.on('request',r=>{if(/^https?:/.test(r.url()))result.externalRequests.push(r.url());});
  await page.addInitScript(()=>{
   const kinds=['Texture','Framebuffer','Program','Buffer','VertexArray','Shader','Sync'];
   window.__stressGL={live:Object.fromEntries(kinds.map(k=>[k,new Set()])),max:Object.fromEntries(kinds.map(k=>[k,0])),created:{},deleted:{}};
   for(const kind of kinds){
    const create=kind==='Sync'?'fenceSync':'create'+kind,remove=kind==='Sync'?'deleteSync':'delete'+kind;
    const make=WebGL2RenderingContext.prototype[create],drop=WebGL2RenderingContext.prototype[remove];
    WebGL2RenderingContext.prototype[create]=function(...args){
     const value=make.apply(this,args),s=__stressGL;if(value){s.live[kind].add(value);s.created[kind]=(s.created[kind]||0)+1;s.max[kind]=Math.max(s.max[kind],s.live[kind].size);}return value;
    };
    WebGL2RenderingContext.prototype[remove]=function(value){
     const s=__stressGL;if(value&&s.live[kind].delete(value))s.deleted[kind]=(s.deleted[kind]||0)+1;return drop.call(this,value);
    };
   }
   window.__stressLongTasks=[];
   if(typeof PerformanceObserver==='function'&&PerformanceObserver.supportedEntryTypes.includes('longtask')){
    new PerformanceObserver(list=>{for(const e of list.getEntries())if(__stressLongTasks.length<4096)__stressLongTasks.push({start:e.startTime,duration:e.duration});}).observe({type:'longtask',buffered:true});
   }
  });
  // The ordinary host is deliberately used. ?pause would bypass GPU backpressure.
  result.preparationStarted=new Date().toISOString();
  await page.goto(pathToFileURL(path.join(visual,'index.html')).href+'?auto=0&zoom=2&dpr=1&count=16&mix=mixed&output=on');
  await page.waitForFunction(()=>window.RA?.debug);
  await page.waitForFunction(()=>RA.debug.warmed);
  result.preparation=await page.evaluate(()=>({readyAt:performance.now(),warmed:RA.debug.warmed,
   hostWarmDurationMs:RA.debug.warmDuration,simTimeAtReady:RA.debug.simT,
   fallback:!!RA.debug.renderer.fallback,canvasVisibility:document.getElementById('panel').style.visibility}));
  result.preparationFinished=new Date().toISOString();
  console.log('PREPARATION '+JSON.stringify(result.preparation));
  let cdp;
  try{cdp=await page.context().newCDPSession(page);await cdp.send('Performance.enable');}catch(e){result.heapUnavailable=String(e);}
  await page.evaluate(()=>{
   const d=RA.debug,r=d.renderer,g=r.gl;
   if(!g||r.fallback)throw Error('Real WebGL is required by this test');
   const s=window.__runtimeStress={start:performance.now(),warmedAtStart:d.warmed,warmDurationAtStart:d.warmDuration,renderMarginAtStart:RA.MARGIN,done:false,frames:0,accepted:0,rejected:0,sequence:0,forceBusy:false,forcedWaits:0,
    max:{rows:0,poses:0,pending:0,fences:0,glyphs:0,particles:0,atlasRequested:0,atlasEntries:0,atlasWidths:0},
    samples:[],violations:[],phaseTransitions:[],phase:'',still:{frames:0,maxPhaseDelta:0,revisions:new Set()},pause:{},busy:{},recovery:{},final:{},glErrors:[],lastConsumed:-1,consumedRevisions:0};
   const resourceCounts=()=>Object.fromEntries(Object.entries(__stressGL.live).map(([k,v])=>[k,v.size]));
   const originalFrame=d.anim.frame,originalLabels=d.labels.build,originalWait=g.clientWaitSync;
   function violation(kind,value,limit){if(s.violations.length<50)s.violations.push({at:(performance.now()-s.start)/1000,kind,value,limit});}
   d.anim.frame=function(...args){
    const scene=originalFrame.apply(this,args);s.frames++;
    s.max.rows=Math.max(s.max.rows,scene.rows.length);s.max.poses=Math.max(s.max.poses,this.poses.size);
    if(scene.rows.length>16)violation('rows',scene.rows.length,16);
    if(this.poses.size>32)violation('poses',this.poses.size,32);
    const rev=this.snapshot.revision;
    if(rev!==s.lastConsumed){s.lastConsumed=rev;s.consumedRevisions++;}
    if(d.opt.quality===3){
     if(s.still.reference==null)s.still.reference=this.ribbons.phase[0];
     s.still.frames++;s.still.maxPhaseDelta=Math.max(s.still.maxPhaseDelta,Math.abs(this.ribbons.phase[0]-s.still.reference));s.still.revisions.add(rev);
    }
    const sec=(performance.now()-s.start)/1000;
    if(sec>=75&&sec<90&&!s.recovery.latestPaintedAt&&scene.rows.length===16&&scene.rows.every(q=>q.id.startsWith('recover-')&&q.reveal>.999&&q.dissolve<.001&&q.mix>=.999))
     s.recovery.latestPaintedAt=sec;
    if(sec>=112&&scene.rows.length===16&&scene.rows.every(q=>q.id.startsWith('final-')&&q.reveal>.999&&q.dissolve<.001&&q.mix>=.999))
     s.final.latestPresentedAt??=sec;
    return scene;
   };
   d.labels.build=function(...args){const value=originalLabels.apply(this,args);s.max.glyphs=Math.max(s.max.glyphs,this.n);if(this.n>2304)violation('glyphs',this.n,2304);return value;};
   g.clientWaitSync=function(...args){if(s.forceBusy&&args[0]===r.pendingGPU){s.forcedWaits++;return g.TIMEOUT_EXPIRED;}return originalWait.apply(this,args);};
   const phase=sec=>sec<8?'warmup':sec<38?'churn':sec<50?'still':sec<60?'churn-2':sec<66?'pause':sec<74?'resume':sec<75?'busy':sec<90?'recover':sec<112?'churn-3':'final';
   function inject(){
    if(s.done)return;
    const sec=(performance.now()-s.start)/1000,current=phase(sec);
    if(current!==s.phase){
     s.phase=current;s.phaseTransitions.push({at:sec,phase:current});
     d.opt.quality=current==='still'?3:0;
     if(current==='pause'){s.pause.startRevision=d.anim.snapshot.revision;s.pause.startAccepted=s.accepted;s.pause.startSim=d.simT;d.setPlaying(false);}
     if(current==='resume'){s.pause.endRevision=d.anim.snapshot.revision;s.pause.endSim=d.simT;s.pause.endAccepted=s.accepted;s.pause.pendingAtResume=!!d.feed.pending;s.pause.targetSequence=d.feed.sequence;d.setPlaying(true);}
     s.forceBusy=current==='busy';
     if(current==='busy'){s.busy.startSubmitted=r.submitted;s.busy.startSkipped=r.skipped;}
     if(current==='recover'){s.busy.endSubmitted=r.submitted;s.busy.endSkipped=r.skipped;s.recovery.startSubmitted=r.submitted;}
    }
    const tag=current==='warmup'?'warmup':current==='recover'?'recover':current==='final'?'final':'batch-'+s.sequence;
    const snap=RA.synth(sec,{count:16,mix:s.sequence%3===0?'mixed':'green',output:'on',quota:String(50+s.sequence%40),pending:false});
    snap.at=new Date().toISOString();snap.revision=++s.sequence;
    for(let i=0;i<snap.rows.length;i++){
     const row=snap.rows[i];row.thread_id=tag+'-'+i;row.turn_id='turn-'+tag;row.response_id='response-'+tag+'-'+i;
     const nameTag=['warmup','recover','final'].includes(current)?current:s.sequence%20;
     row.model=i%5===0?'模型-观测-'+nameTag+'-gpt-6.1-sol':'gpt-6.1-sol-'+(i%4);
    }
    if(d.feed.receive({generation:1,sequence:s.sequence,snapshot:snap},performance.now()/1000))s.accepted++;else s.rejected++;
    s.max.pending=Math.max(s.max.pending,d.feed.pending?1:0);
   }
   function observe(){
    const sec=(performance.now()-s.start)/1000,resources=resourceCounts();
    const sample={at:sec,phase:s.phase,rows:d.labels.rowLayouts.length,poses:d.anim.poses.size,pending:d.feed.pending?1:0,
     fences:resources.Sync,glyphs:d.labels.n,particles:d.parts.count,atlasRequested:d.atlas.requested.size,atlasEntries:d.atlas.map.size,atlasWidths:d.atlas.widths.size,
     accepted:s.accepted,sequence:s.sequence,consumedRevision:d.anim.snapshot.revision,submitted:r.submitted,skipped:r.skipped,resources,
     playing:d.playing,quality:d.opt.quality,simTime:d.simT,fallback:!!d.renderer.fallback,canvas:[r.canvas.width,r.canvas.height],glass:[r.glass.canvas.width,r.glass.canvas.height]};
    s.samples.push(sample);
    for(const k of ['fences','particles','atlasRequested','atlasEntries','atlasWidths'])s.max[k]=Math.max(s.max[k],sample[k]);
    if(resources.Sync>1)violation('fences',resources.Sync,1);
    if(d.renderer.fallback)violation('unexpected-fallback',true,false);
    const error=g.getError();if(error)s.glErrors.push({at:sec,error});
    if(sec>=67&&s.pause.resumeConsumedAt==null&&d.anim.snapshot.revision>=s.pause.targetSequence)s.pause.resumeConsumedAt=sec;
    if(sec>=90&&s.recovery.endSubmitted==null)s.recovery.endSubmitted=r.submitted;
    if(s.violations.length||resources.Texture>128||resources.Framebuffer>128){s.aborted='Resource invariant failure';finish();}
   }
   function finish(){
    if(s.done)return;s.done=true;s.elapsed=(performance.now()-s.start)/1000;
    clearInterval(s.inputTimer);clearInterval(s.observeTimer);clearTimeout(s.endTimer);
    s.forceBusy=false;s.final.submitted=r.submitted;s.final.skipped=r.skipped;s.final.snapshotRevision=d.anim.snapshot.revision;
    s.final.ids=(d.anim.snapshot.rows||[]).map(q=>q.thread_id);s.final.resources=resourceCounts();
    s.final.atlasBuilt=d.atlas.built;s.final.atlasGenerations=d.atlas.generation;
    s.longTasks=__stressLongTasks.filter(e=>e.start>=s.start&&e.start<=s.start+s.elapsed*1000);
    s.preparationLongTasks=__stressLongTasks.filter(e=>e.start<s.start);
    s.longTaskSupported=typeof PerformanceObserver==='function'&&PerformanceObserver.supportedEntryTypes.includes('longtask');
    s.still.revisions=[...s.still.revisions];s.resourceMax=__stressGL.max;s.resourceCreated=__stressGL.created;s.resourceDeleted=__stressGL.deleted;
    d.setPlaying(false);d.anim.frame=originalFrame;d.labels.build=originalLabels;g.clientWaitSync=originalWait;
   }
   inject();s.inputTimer=setInterval(inject,200);s.observeTimer=setInterval(observe,1000);s.endTimer=setTimeout(()=>{observe();finish();},120000);
  });
  const begun=new Date().toISOString();result.playbackStarted=begun;
  console.log('PLAYBACK_STARTED '+begun+' expected_duration_seconds=120');
  let poll=0;
  for(;;){
   await sleep(5000);
   if(cdp){
    try{
     const data=await cdp.send('Performance.getMetrics'),m=Object.fromEntries(data.metrics.map(x=>[x.name,x.value]));
     const usage=await cdp.send('Runtime.getHeapUsage');
     result.heapSamples.push({elapsed:++poll*5,jsHeapUsed:m.JSHeapUsedSize,jsHeapTotal:m.JSHeapTotalSize,...usage});
     if(usage.usedSize>512*1024*1024)throw Error('Test heap exceeded the isolated 512 MiB guard');
    }catch(e){if(String(e).includes('guard'))throw e;result.heapUnavailable=String(e);cdp=null;}
   }else poll++;
   const state=await page.evaluate(()=>({done:__runtimeStress.done,elapsed:(performance.now()-__runtimeStress.start)/1000,phase:__runtimeStress.phase,max:__runtimeStress.max,accepted:__runtimeStress.accepted,skipped:RA.debug.renderer.skipped,submitted:RA.debug.renderer.submitted}));
   if(poll%3===0||state.done)console.log('PROGRESS '+JSON.stringify(state));
   if(state.done)break;
   if(state.elapsed>135)throw Error('Playback deadline exceeded');
  }
  const observed=await page.evaluate(()=>{
   const s=__runtimeStress;
   return Object.fromEntries(Object.entries(s).filter(([key])=>!['inputTimer','observeTimer','endTimer'].includes(key)));
  });
  result.observed=observed;
  result.sourceHashesAfter=hashes();result.sourceUnchanged=JSON.stringify(result.sourceHashes)===JSON.stringify(result.sourceHashesAfter);
  const steady=observed.samples.filter(s=>s.at>=10),resourceKeys=['Texture','Framebuffer','Program','Buffer','VertexArray','Shader'];
  result.resourceStability=Object.fromEntries(resourceKeys.map(k=>[k,{min:Math.min(...steady.map(s=>s.resources[k])),max:Math.max(...steady.map(s=>s.resources[k]))}]));
  const tasks=observed.longTasks.map(e=>e.duration).sort((a,b)=>a-b);
  result.longTasks={supported:observed.longTaskSupported,count:tasks.length,maxMs:tasks.at(-1)||0,totalMs:tasks.reduce((a,b)=>a+b,0),perMinute:tasks.length*60/observed.elapsed,p95Ms:tasks.length?tasks[Math.floor((tasks.length-1)*.95)]:0,first20:observed.longTasks.slice(0,20)};
  result.longTasks.earlyPlaybackCount=observed.longTasks.filter(e=>(e.start-observed.start)/1000<8).length;
  result.longTasks.afterInitialEightSecondsCount=tasks.length-result.longTasks.earlyPlaybackCount;
  const preparationTasks=observed.preparationLongTasks.map(e=>e.duration);
  result.preparation.longTasks={count:preparationTasks.length,maxMs:Math.max(0,...preparationTasks),totalMs:preparationTasks.reduce((a,b)=>a+b,0),entries:observed.preparationLongTasks};
  result.preparation.playbackStartPerformanceTime=observed.start;
  result.responsivenessFinding=tasks.length?'PLAYBACK_LONG_TASKS_OBSERVED':'NO_PLAYBACK_LONG_TASKS_OBSERVED_PREPARATION_RECORDED_SEPARATELY';
  result.recoveryMeasurement='latestPaintedAt/latestPresentedAt record scene readiness in Animator.frame, not a pixel readback; render submissions and GL checks are measured independently.';
  const heap=result.heapSamples.filter(s=>s.elapsed>=10),last=heap.at(-1),first=heap[0];
  if(heap.length){
   result.heapTrend={samples:heap.length,firstUsed:first.usedSize,lastUsed:last.usedSize,minUsed:Math.min(...heap.map(s=>s.usedSize)),maxUsed:Math.max(...heap.map(s=>s.usedSize)),
    differenceUsed:last.usedSize-first.usedSize,interpretation:'Unforced samples include normal GC sawtooth; two-minute trend is not proof of leak absence.'};
  }
  result.checks={
   elapsedAtLeast119Seconds:observed.elapsed>=119,
   preparationCompleted:observed.warmedAtStart&&observed.warmDurationAtStart>0&&!result.preparation.fallback,
   sourceUnchanged:result.sourceUnchanged,
   bounds:observed.max.rows<=16&&observed.max.poses<=32&&observed.max.pending<=1&&observed.max.fences<=1&&observed.resourceMax.Sync<=1&&observed.max.glyphs<=2304,
   glResourcesStable:Object.values(result.resourceStability).every(v=>v.min===v.max),
   stillDataLiveAndMotionFrozen:observed.still.frames>0&&observed.still.revisions.length>10&&observed.still.maxPhaseDelta===0,
   pauseCoalesced:observed.pause.endAccepted-observed.pause.startAccepted>=20&&observed.pause.startSim===observed.pause.endSim&&observed.pause.pendingAtResume&&observed.pause.resumeConsumedAt!=null,
   busyWasBounded:observed.forcedWaits>0&&observed.busy.endSubmitted===observed.busy.startSubmitted&&observed.busy.endSkipped>observed.busy.startSkipped,
   latestTargetRecovered:observed.recovery.latestPaintedAt!=null&&observed.recovery.endSubmitted>observed.recovery.startSubmitted&&observed.final.latestPresentedAt!=null&&observed.final.ids.every(id=>id.startsWith('final-')),
   fullResolutionPreserved:observed.renderMarginAtStart===128&&observed.samples.every(s=>s.canvas[0]===(388+2*observed.renderMarginAtStart)*2&&s.canvas[1]===(500+2*observed.renderMarginAtStart)*2&&s.glass[0]===1672&&s.glass[1]===941),
   offlineAndErrorFree:!result.externalRequests.length&&!result.runtimeErrors.length&&!observed.glErrors.length&&!observed.violations.length&&!observed.aborted,
   noPlaybackLongTasks:tasks.length===0
  };
  result.result=Object.values(result.checks).every(Boolean)?'BOUNDED_STRESS_PASS':'STRESS_GAPS';
  if(result.result!=='BOUNDED_STRESS_PASS')process.exitCode=1;
 }catch(e){result.result='ERROR';result.failure=String(e.stack||e);process.exitCode=1;console.error(e);}
 finally{
  if(browser)await browser.close();
  result.finished=new Date().toISOString();fs.mkdirSync(path.dirname(output),{recursive:true});fs.writeFileSync(output,JSON.stringify(result,null,2));
  console.log(JSON.stringify({result:result.result,checks:result.checks,longTasks:result.longTasks,heapTrend:result.heapTrend}));
 }
})();
