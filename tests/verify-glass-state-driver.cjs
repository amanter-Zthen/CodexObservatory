'use strict';
// The live host and offline exporter must share state selection, but not render pacing.
const fs=require('fs'),path=require('path'),vm=require('vm'),assert=require('assert'),{pathToFileURL}=require('url');
const {chromium,chromePath}=require('./runtime.cjs');
const root=path.resolve(__dirname,'..'),out=path.join(root,'verification/glass-state');fs.mkdirSync(out,{recursive:true});
const box=vm.createContext({window:{},console});
for(const f of ['engine/core.js','engine/presentation.js','engine/row_presentation.js','engine/ribbon_art.js','engine/stage.js','engine/quota_motion.js','engine/ribbons.js','background/engine/archive.js','background/engine/appearance.js','background/engine/dynamics.js','engine/animator.js','engine/synthetic.js'])vm.runInContext(fs.readFileSync(path.join(root,'visual',f),'utf8'),box);
const R=box.window.RA,a=new R.Animator(),snapshot=(n,mix='green')=>R.synth(0,{count:n,mix,output:'on',quota:87,pending:false});
const advance=(seconds)=>{for(let i=0;i<Math.round(seconds*60);i++)a.frame(1/60,0);};
const checks=[],ok=(name,fn)=>{const evidence=fn();checks.push({name,pass:true,evidence});};
ok('empty-snapshot-selects-idle-without-preview-host',()=>{a.set(snapshot(0));assert.equal(a.glassDynamics.desiredState,'idle');a.showImmediate();advance(6);assert.equal(a.glassDynamics.view().state,'idle');assert.equal(a.glassDynamics.archive.records.size,0);});
ok('green-yellow-red-are-driven-by-the-same-snapshot',()=>{const states=[];for(const [n,mix,expected]of [[1,'green','green'],[2,'mixed','yellow'],[4,'mixed','red']]){a.set(snapshot(n,mix));assert.equal(a.glassDynamics.desiredState,expected);advance(4);assert.equal(a.glassDynamics.view().state,expected);assert(a.glassDynamics.settled());states.push(expected);}return states;});
ok('same-target-does-not-create-new-glass-commands',()=>{const g=a.glassDynamics.generation,c=a.glassDynamics.commands;a.set(snapshot(4,'mixed'));a.set(snapshot(4,'mixed'));assert.equal(a.glassDynamics.generation,g);assert.equal(a.glassDynamics.commands,c);});
ok('manual-override-survives-new-snapshots-and-auto-resolves-latest',()=>{a.setGlassState('red');a.set(snapshot(1));assert.equal(a.glassDynamics.desiredState,'red');a.setGlassState('auto');assert.equal(a.glassDynamics.desiredState,'green');a.set(snapshot(0));assert.equal(a.glassDynamics.desiredState,'idle');advance(6);assert.equal(a.glassDynamics.view().state,'idle');});
ok('synthetic-models-and-route-labels-are-consistent',()=>{const rows=snapshot(4,'mixed').rows;assert(rows.some(r=>r.model==='gpt-6-astra'));assert(!rows.some(r=>r.model.includes('mini')));for(const r of rows){if(r.color==='green')assert.equal(r.reported_model,r.requested_model);if(r.color==='red')assert.notEqual(r.reported_model,r.requested_model);if(r.color==='yellow')assert.equal(r.reported_model,'');}return rows.map(r=>({model:r.model,requested:r.requested_model,reported:r.reported_model,color:r.color}));});
(async()=>{let browser;try{
 browser=await chromium.launch({headless:true,executablePath:chromePath});const page=await browser.newPage();
 await page.goto(pathToFileURL(path.join(root,'visual/index.html')).href+'?pause&auto=0&count=2&mix=mixed&output=on&zoom=1&dpr=1');await page.waitForFunction(()=>window.RA?.debug);await page.evaluate(()=>RA.debug.advance(12));
 const evidence=await page.evaluate(()=>{
  const d=RA.debug,g=d.renderer.glass,gl=d.renderer.gl,dy=d.anim.glassDynamics;
  const draw=(interval)=>{g.lastTime=-Infinity;const start=g.draws;for(let i=0;i<61;i++){g.update(50+i/60,'auto',dy,interval);gl.finish();}return g.draws-start;};
  const live=draw(undefined),offline=draw(0),pixels=[];
  for(const state of ['green','yellow','red']){
   d.anim.setGlassState(state);for(let i=0;i<300;i++)d.anim.frame(1/60,0);g.update(d.anim.motionTime,'auto',dy,0);gl.finish();
   gl.bindFramebuffer(gl.FRAMEBUFFER,g.framebuffer);const b=new Uint8Array(1672*941*4);gl.readPixels(0,0,1672,941,gl.RGBA,gl.UNSIGNED_BYTE,b);gl.bindFramebuffer(gl.FRAMEBUFFER,null);
   let sum=0;for(let i=0;i<b.length;i+=4)sum=(sum+b[i]+3*b[i+1]+7*b[i+2])%4294967291;
   pixels.push({state:dy.view().state,desired:dy.desiredState,checksum:sum,records:dy.archive.records.size});
  }return{live,offline,pixels,glError:gl.getError()};
 });
 ok('live-thirty-hz-and-explicit-offline-every-frame',()=>{assert.equal(evidence.live,31);assert.equal(evidence.offline,61);return{live:evidence.live,offline:evidence.offline};});
 ok('actual-glass-pixels-follow-green-yellow-red',()=>{assert.equal(evidence.glError,0);assert.equal(new Set(evidence.pixels.map(s=>s.checksum)).size,3);assert.deepEqual(evidence.pixels.map(s=>s.state),['green','yellow','red']);return evidence.pixels;});
 const assembled=await page.evaluate(()=>{
  const d=RA.debug,config=d.configureCapture({scale:2,maxHeight:180}),snapshot=RA.synth(0,{count:4,mix:'mixed',output:'on',quota:87,pending:false});snapshot.at=new Date().toISOString();
  const accepted=d.feed.receive({kind:'snapshot',generation:3,sequence:1,snapshot},performance.now()/1000);d.advance(4,1/60);
  const before=d.renderer.glass.draws;for(let i=0;i<61;i++){d.advance(1/60,1/60);d.renderer.gl.finish();}
  return{accepted,config,draws:d.renderer.glass.draws-before,state:d.anim.glassDynamics.view().state,rows:d.anim.snapshot.rows.length,models:d.anim.snapshot.rows.map(r=>r.model)};
 });
 ok('assembled-preview-capture-uses-the-live-feed-and-all-layers',()=>{assert(assembled.accepted);assert.equal(assembled.draws,61);assert.equal(assembled.state,'red');assert.equal(assembled.rows,4);assert(assembled.models.includes('gpt-6-astra'));assert.equal(assembled.config.pipeline,'assembled-preview');return assembled;});
 await page.goto(pathToFileURL(path.join(root,'visual/index.html')).href+'?desktop&pause&auto=0&count=0');await page.waitForFunction(()=>window.RA?.debug);
 const guarded=await page.evaluate(()=>{try{RA.debug.configureCapture({scale:5});return false;}catch(e){return e.message.includes('standalone');}});
 ok('desktop-host-cannot-enable-unthrottled-capture-mode',()=>assert(guarded));
 fs.writeFileSync(path.join(out,'state-driver.json'),JSON.stringify({result:'PASS',checks},null,2));console.log(JSON.stringify({result:'PASS',checks:checks.length,evidence},null,2));
}finally{await browser?.close();}})().catch(e=>{console.error(e);process.exitCode=1;});
