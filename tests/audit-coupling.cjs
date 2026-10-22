'use strict';
// Review-only audit: leaves all frozen runtime files unchanged.
const fs=require('fs'),path=require('path'),vm=require('vm'),crypto=require('crypto');
const root=path.resolve(__dirname,'..'),visual=path.join(root,'visual');
const report={version:'0.4.9',scope:'Animation coupling audit, synthetic inputs, no runtime edits',
  at:new Date().toISOString(),checks:[],findings:[],errors:[]};
const out=path.join(root,process.argv.includes('--candidate')?'verification/engineering/coupling':'verification/coupling-audit');
fs.mkdirSync(out,{recursive:true});
const box=vm.createContext({window:{},console});
for(const file of ['engine/core.js','engine/presentation.js','engine/row_presentation.js',
 'engine/ribbon_art.js','engine/stage.js','engine/quota_motion.js','engine/ribbons.js',
 'background/engine/archive.js','background/engine/appearance.js','background/engine/dynamics.js',
 'engine/animator.js','engine/synthetic.js']){
 vm.runInContext(fs.readFileSync(path.join(visual,file),'utf8'),box,{filename:file});
}
const RA=box.window.RA;
const snap=(n=1,output='on',tag='')=>{
 const s=RA.synth(0,{count:n,mix:'green',output,quota:'87'});
 if(tag)for(const r of s.rows)r.thread_id=tag+r.thread_id;
 return s;
};
function advance(a,seconds,dt=1/120,observe=()=>{}){
 let scene;for(let n=0;n<Math.round(seconds/dt);n++){scene=a.frame(dt,0);observe(scene);}
 return scene;
}
const check=(name,ok,evidence={},finding)=>{
 report.checks.push({name,pass:!!ok,evidence});
 if(!ok)report.findings.push({id:finding?.id||name,...finding,evidence});
 console.log(ok?'PASS':'GAP',name);
};
const hash=p=>crypto.createHash('sha256').update(fs.readFileSync(p)).digest('hex');
async function audit(){
 const lock=JSON.parse(fs.readFileSync(path.join(root,'layers.lock.json')));
 for(const [file,h]of Object.entries(lock.runtimeHashes)){
  if(hash(path.join(visual,file))!==h)throw Error('Frozen source changed: '+file);
 }
 report.runtimeHashes=lock.runtimeHashes;
 check('frozen-runtime-identity',true,{files:Object.keys(lock.runtimeHashes).length});
 // Vary interruption across parent entry, name reveal, output expansion and exit.
 let cases=0,violations=[];
 for(const count of [0,1,16])for(const dt of [1/120,1/30,.1])
 for(const exitAt of [.1,.49,1.4,2.4,4.4,4.8,6])
 for(const returnDelay of [.05,.45,1.5]){
  const a=new RA.Animator();a.hideImmediate();a.set(snap(count));a.beginReveal();
  let exitSent=false,returnSent=false,earlyFall=0,earlyText=0,earlyOpen=0;
  for(let i=0;i<Math.ceil(18/dt);i++){
   if(!exitSent&&a.t>=exitAt){a.beginExit();exitSent=true;}
   if(exitSent&&!returnSent&&a.t>=exitAt+returnDelay){a.beginReveal();returnSent=true;}
   const s=a.frame(dt,0);
   const falling=a.glassAppearance.mode==='exit'&&a.glassAppearance.elapsed>=box.window.BG16.appearance.params.anticipation;
   if(falling&&s.rows.some(r=>r.rateOp>1e-9))earlyFall++;
   if(s.rows.some(r=>r.dissolve>0&&r.rateOp>1e-9))earlyText++;
   if(s.rows.some(r=>r.rateOp>0&&(r.reveal<.999999||r.mix<.999)))earlyOpen++;
  }
  const converged=a.lifecycle==='visible'&&a.glassAppearance.mode==='visible';
  if(earlyFall||earlyText||earlyOpen||!converged)violations.push({count,dt,exitAt,returnDelay,earlyFall,earlyText,earlyOpen,lifecycle:a.lifecycle});
  cases++;
 }
 check('whole-panel-interruption-phase-matrix',!violations.length,{cases,violations:violations.slice(0,10)});
 // Combine archive changes and explicit glass/panel commands; final intent wins.
 const a=new RA.Animator();a.hideImmediate();a.set(snap(4));a.beginReveal();
 let brokenClips=0;
 for(let i=0;i<1200;i++){
  if(i%11===0)a.glassDynamics.requestState(['red','yellow','green','idle'][Math.floor(i/11)%4]);
  if(i%109===0)a.beginExit();
  if(i%109===19)a.beginReveal();
  const g=a.glassAppearance,mode=g.mode,elapsed=g.elapsed;
  a.frame(1/60,0);
  if((mode==='enter'||mode==='exit')&&g.mode===mode&&g.elapsed<elapsed)brokenClips++;
 }
 a.beginReveal();a.glassDynamics.requestState('red');advance(a,20);
 check('archive-and-whole-panel-latest-target-convergence',
  !brokenClips&&a.lifecycle==='visible'&&a.glassDynamics.archive.state==='red'&&a.glassDynamics.settled(),
  {brokenClips,lifecycle:a.lifecycle,glass:a.glassAppearance.mode,state:a.glassDynamics.archive.state,
   records:a.glassDynamics.archive.records.size,pulses:a.glassDynamics.archive.pulses.length});
 const glass=new RA.Animator();glass.set(snap(1));advance(glass,7);
 glass.hideGlass();advance(glass,.3);glass.showGlass();advance(glass,10);
 glass.replayGlass();advance(glass,.25);glass.beginExit();advance(glass,12);
 check('glass-only-commands-then-whole-exit',glass.lifecycle==='hidden'&&glass.glassAppearance.mode==='hidden',
  {lifecycle:glass.lifecycle,glass:glass.glassAppearance.mode,completed:glass.glassAppearance.completed});
 const replayCases=[];
 for(const phase of ['hidden','enter','visible','rows-exit','glass-exit','panel-exit']){
  const replay=new RA.Animator();replay.hideImmediate();replay.set(snap(1));replay.beginReveal();
  if(phase==='enter')advance(replay,1);
  else if(phase!=='hidden'){
   advance(replay,7);
   if(phase!=='visible'){
    replay.beginExit();
    for(let i=0;i<2400&&replay.lifecycle!==phase;i++)replay.frame(1/120,0);
    if(replay.lifecycle!==phase)throw Error('Replay test could not reach '+phase);
   }
  }
  const initial=replay.lifecycle;replay.beginReplay();
  let previous=replay.lifecycle,entries=0,hides=0;const transitions=[];
  advance(replay,22,1/120,()=>{
   if(replay.lifecycle!==previous){
    if(replay.lifecycle==='enter')entries++;
    if(replay.lifecycle==='hidden')hides++;
    transitions.push(replay.lifecycle);previous=replay.lifecycle;
   }
  });
  replayCases.push({initial,entries,hides,transitions,final:replay.lifecycle});
 }
 check('replay-during-an-existing-exit-produces-one-new-entry',
  replayCases.every(c=>c.entries===1&&c.final==='visible'),{cases:replayCases},
  {id:'C05',priority:'P2',title:'Replay requested during exit survives into the next entry and causes another full cycle',
   trigger:'While rows-exit, glass-exit or panel-exit is underway, request beginReplay once.',
   source:'visual/engine/animator.js',
   mechanism:'panelReplay remains true through hidden -> enter and is consumed only after the fresh entry is already visible.'});
 // Quota ring erasure precedes recentering, even with new rows while erasing.
 const quota=new RA.Animator();quota.set(snap(0,'off'));advance(quota,5);
 let badArc=0,earlyCenter=0;
 for(const [count,seconds]of [[4,4],[0,.2],[2,.35],[0,.3],[16,4],[0,5]]){
  quota.set(snap(count,'off'));
  advance(quota,seconds,1/120,s=>{
   if(s.quota.arcStart>s.quota.arcEnd+1e-8)badArc++;
   if(quota.quotaMotion.dock.target===0&&s.rows.some(r=>r.dissolve<.999))earlyCenter++;
  });
 }
 check('quota-row-removal-return-and-ring-reversal',!badArc&&!earlyCenter&&quota.quotaMotion.phase==='center',
  {badArc,earlyCenter,phase:quota.quotaMotion.phase,remainingPoses:quota.poses.size});
 // Same row returns halfway through its own disappearance.
 const revive=new RA.Animator();revive.set(snap(1,'off'));advance(revive,5);
 revive.set(snap(0,'off'));const before=advance(revive,.4).rows[0];
 revive.set(snap(1,'off'));const after=revive.frame(1/120,0).rows[0];
 check('returning-row-completes-or-continuously-reverses-dissolve',
  Math.abs(after.dissolve-before.dissolve)<.03,
  {before:{dissolve:before.dissolve,reveal:before.reveal,rateOp:before.rateOp},
   after:{dissolve:after.dissolve,reveal:after.reveal,rateOp:after.rateOp}},
  {id:'C01',priority:'P2',title:'Returning row abruptly resets its disappearance',
   trigger:'Remove one waiting row, advance 0.40 s, re-add the same thread_id.',
   source:'visual/engine/animator.js',mechanism:'set() sets gone=-1 before the local dissolve clip completes.'});
 // Rename while a prior name crossfade is still in flight.
 const rename=new RA.Animator();rename.set(snap(1,'off'));advance(rename,5);
 const b=snap(1,'off');b.rows[0].model='model-B';rename.set(b);rename.frame(1/120,0);
 const first=advance(rename,.1).rows[0];
 const c=snap(1,'off');c.rows[0].model='model-C';rename.set(c);const second=rename.frame(1/120,0).rows[0];
 check('successive-renames-finish-the-current-name-transition',second.model===first.model&&second.mix>=first.mix,
  {before:{model:first.model,prev:first.prev,mix:first.mix},after:{model:second.model,prev:second.prev,mix:second.mix}},
  {id:'C02',priority:'P2',title:'A second rename interrupts the current name crossfade',
   trigger:'Waiting row A -> B; 0.10 s into the crossfade, request C.',
   source:'visual/engine/animator.js',mechanism:'closed RowMotion permits pendingModel replacement even when mix is unfinished.'});
 // Realistic feed cadence, entirely new displayed rows each update.
 const churn=new RA.Animator();churn.set(snap(16,'on','initial-'));advance(churn,7);
 let peakPoses=churn.poses.size,peakRows=0,peakVisible=0,peakSources=0;
 for(let generation=0;generation<50;generation++){
  churn.set(snap(16,'on','generation-'+generation+'-'));
  advance(churn,.2,1/60,s=>{
   peakPoses=Math.max(peakPoses,churn.poses.size);peakRows=Math.max(peakRows,s.rows.length);
   peakVisible=Math.max(peakVisible,s.rows.filter(r=>r.reveal>.01&&r.dissolve<.99).length);
   peakSources=Math.max(peakSources,s.ribbons.slots.length);
  });
 }
 advance(churn,7);
 check('rapid-row-replacement-remains-within-render-row-capacity',peakVisible<=16,
  {updates:50,cadenceHz:5,peakPoses,peakRows,peakVisible,peakSources,finalPoses:churn.poses.size},
  {id:'C03',priority:'P1',title:'Outgoing and incoming rows exceed the 16-row lamp/ruler capacity',
   trigger:'Replace a full 16-row snapshot with new thread IDs while previous rows retire.',
   source:'visual/engine/animator.js + visual/engine/labels.js',mechanism:'Pose map retains outgoing rows, but lamp/ruler uniforms contain only 16 slots.'});
 // Stationary idle can pause without animation progress or lost input slots.
 const paused=new RA.Animator();paused.set(snap(3));advance(paused,8);
 const t=paused.t,angle=[...paused.poses.values()][0].rowMotion.angle;
 for(let i=0;i<100;i++)paused.frame(0,0);
 check('zero-delta-does-not-advance-running-animation',paused.t===t&&[...paused.poses.values()][0].rowMotion.angle===angle,
  {frames:100,timeDelta:paused.t-t,angleDelta:[...paused.poses.values()][0].rowMotion.angle-angle});
 // Browser-backed probes inspect real glyph and GPU storage, not mock geometry.
 const {chromium,chromePath}=require('./runtime.cjs'),{pathToFileURL}=require('url');
 let browser;
 try{
  browser=await chromium.launch({executablePath:chromePath,headless:true});
  const p=await browser.newPage({viewport:{width:1200,height:1200},deviceScaleFactor:1});
  p.on('pageerror',e=>report.errors.push(String(e)));
  const requests=[];p.on('request',r=>{if(/^https?:/.test(r.url()))requests.push(r.url());});
  await p.addInitScript(()=>{
   window.__auditGPU={};
   for(const kind of ['Texture','Framebuffer','Program','Buffer','VertexArray']){
    for(const verb of ['create','delete']){
     const key=verb+kind,fn=WebGL2RenderingContext.prototype[key];
     WebGL2RenderingContext.prototype[key]=function(...args){
      __auditGPU[key]=(__auditGPU[key]||0)+1;return fn.apply(this,args);
     };
    }
   }
  });
  await p.goto(pathToFileURL(path.join(visual,'index.html')).href+'?pause&auto=0&count=1&output=off&zoom=1&dpr=1');
  await p.waitForFunction(()=>window.RA?.debug);await p.evaluate(()=>RA.debug.advance(8));
  const replacement=await p.evaluate(()=>{
   const a=new RA.Animator(),snap=tag=>{const s=RA.synth(0,{count:16,mix:'green',output:'on',quota:'87'});s.rows.forEach(r=>r.thread_id=tag+r.thread_id);return s;};
   let scene;a.set(snap('old-'));for(let i=0;i<840;i++)scene=a.frame(1/120,0);
   a.set(snap('new-'));for(let i=0;i<102;i++)scene=a.frame(1/120,0);
   const d=RA.debug;d.labels.build(scene);
   const rows=scene.rows.filter(r=>r.reveal>.01&&r.dissolve<.99);
   const lamps=scene.rows.slice(0,d.labels.orbN).filter(r=>r.reveal>.01&&r.dissolve<.99);
   d.renderer.uploadAtlas(d.atlas);
   d.renderer.render(scene,d.labels,d.parts,{scale:1,quality:0,glow:true,backdrop:'running',glassFocus:true});
   return{rows:scene.rows.length,visibleRows:rows.length,lampSlots:d.labels.orbN,
    newVisible:rows.filter(r=>r.id.startsWith('new-')).length,
    newWithLamp:lamps.filter(r=>r.id.startsWith('new-')).length,glyphs:d.labels.n,error:d.renderer.gl.getError()};
  });
  const capacityFinding=report.findings.find(f=>f.id==='C03');
  if(capacityFinding)capacityFinding.renderEvidence=replacement;
  report.replacementRenderEvidence=replacement;
  await p.locator('#stage').screenshot({path:path.join(out,'replacement-overlap.png')});
  const fragments=await p.evaluate(()=>{
   const a=new RA.Animator();a.set(RA.synth(0,{count:1,mix:'green',output:'off',quota:'87'}));
   let s;for(let i=0;i<840;i++)s=a.frame(1/120,0);
   s.rows[0].dissolve=.5;s.dt=0;
   const result=[];
   for(const hz of [30,60,120])for(const quality of [0,1,2]){
    const parts=new RA.Particles();let spawned=0,maxEmitters=0,nominalRate=0;
    parts.spawn=()=>{spawned++;};
    for(let n=0;n<hz;n++){
     RA.debug.labels.build(s);const e=RA.debug.labels.emitters;
     maxEmitters=Math.max(maxEmitters,e.length);nominalRate=e.reduce((sum,v)=>sum+v.rate,0);
     parts.update(1/hz,s,e,quality);
    }
    result.push({hz,quality,spawned,maxEmitters,nominalRate});
   }
   return result;
  });
  const full=fragments.filter(x=>x.quality===0);
  check('glyph-dissolve-emission-is-independent-of-frame-cadence',full.every(x=>x.spawned>0)&&Math.max(...full.map(x=>x.spawned))-Math.min(...full.map(x=>x.spawned))<3,
   {samples:fragments},{id:'C04',priority:'P2',title:'Text shatter emitter loses fractional time at each label rebuild',
    trigger:'The same dissolving name sampled at 30 / 60 / 120 Hz.',
    source:'visual/engine/labels.js + visual/engine/particles.js',
    mechanism:'Labels recreates emitters each frame; Particles stores the fractional accumulator on those discarded objects.'});
  // Reset browser host scene; inspect quality/zoom/profile controls and resources.
  await p.evaluate(()=>RA.debug.redraw());
  const resources=await p.evaluate(()=>{
   const live=()=>Object.fromEntries(['Texture','Framebuffer','Program','Buffer','VertexArray'].map(k=>[k,(__auditGPU['create'+k]||0)-(__auditGPU['delete'+k]||0)]));
   const before=live(),d=RA.debug,archive=d.anim.glassDynamics.archive,time=archive.time,errors=[];
   const select=(id,value)=>{const e=document.getElementById(id);e.value=value;e.dispatchEvent(new Event('change',{bubbles:true}));};
   for(let i=0;i<18;i++){
    select('zoom',['1','1.25','2'][i%3]);select('quality',String(i%3));
    select('backdrop',['running','idle','legacy'][i%3]);
    const e=d.renderer.gl.getError(),b=d.renderer.glass.painter.gl.getError();if(e||b)errors.push([e,b]);
   }
   select('backdrop','running');select('quality','0');select('zoom','1');
   return{before,after:live(),sameArchive:archive===d.anim.glassDynamics.archive,timeDelta:archive.time-time,errors};
  });
  check('zoom-quality-and-crop-switches-preserve-time-and-live-gpu-budget',
   JSON.stringify(resources.before)===JSON.stringify(resources.after)&&resources.sameArchive&&resources.timeDelta===0&&!resources.errors.length,resources);
  const auto=await p.evaluate(()=>{
   const d=RA.debug,e=document.getElementById('auto');e.checked=true;e.dispatchEvent(new Event('change',{bubbles:true}));
   let hiddenSeen=0,reveals=0,last=d.anim.lifecycle,maxParticles=0;
   // Use simulation steps but only a few actual full-frame paints.
   for(let i=0;i<120;i++){
    d.advance(1);
    if(d.anim.lifecycle==='hidden')hiddenSeen++;
    if(d.anim.lifecycle==='enter'&&last!=='enter')reveals++;
    last=d.anim.lifecycle;maxParticles=Math.max(maxParticles,d.parts.count);
   }
   return{hiddenSeen,reveals,maxParticles,finalLifecycle:d.anim.lifecycle,glass:d.anim.glassAppearance.mode,
    foregroundError:d.renderer.gl.getError(),backgroundError:d.renderer.glass.painter.gl.getError()};
  });
  check('host-auto-demo-repeats-with-bounded-particles',auto.reveals>=3&&auto.maxParticles<=210&&!auto.foregroundError&&!auto.backgroundError,auto);
  check('browser-audit-offline-and-error-free',!report.errors.length&&!requests.length,{errors:report.errors,requests});
 }finally{if(browser)await browser.close();}
 report.result=report.findings.length?'GAPS_FOUND':'PASS';
}
audit().catch(error=>{report.result='AUDIT_ERROR';report.errors.push(String(error.stack||error));console.error(error);process.exitCode=2;})
.finally(()=>{
 report.passed=report.checks.filter(c=>c.pass).length;report.failed=report.checks.filter(c=>!c.pass).length;
 fs.writeFileSync(path.join(out,'result.json'),JSON.stringify(report,null,2));
 console.log(JSON.stringify({result:report.result,passed:report.passed,gaps:report.failed,findings:report.findings.map(f=>f.id)}));
});
