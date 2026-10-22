const fs=require('fs'),path=require('path'),vm=require('vm'),assert=require('assert'),crypto=require('crypto'),{pathToFileURL}=require('url');
const{chromium}=require('./runtime.cjs');
const root=path.resolve(__dirname,'../visual'),hash=p=>crypto.createHash('sha256').update(fs.readFileSync(p)).digest('hex');
const result={version:'0.4.9',synthetic:true,nativeIntegrated:false,productionMutation:'NONE',checks:[],errors:[]};
const pass=(name,data={})=>{result.checks.push({name,pass:true,...data});console.log('PASS',name,JSON.stringify(data));};
const context=vm.createContext({window:{},console});for(const name of['archive','appearance','dynamics'])vm.runInContext(fs.readFileSync(path.join(root,'background/engine',name+'.js'),'utf8'),context);
const B=context.window.BG16,A=B.appearance,D=B.dynamics.Dynamics;
function advance(c,seconds,dt=1/120){for(let i=0;i<Math.round(seconds/dt);i++)c.update(dt);}
(async()=>{let browser;try{
 const lock=JSON.parse(fs.readFileSync(path.join(__dirname,'../layers.lock.json')));
 for(const[f,h]of Object.entries(lock.runtimeHashes))assert.equal(hash(path.join(root,f)),h,f);
 pass('frozen-six-layer-runtime-hashes-match');
 const enter=new A.Controller();enter.immediate(false);enter.request('enter');advance(enter,.35);
 const front=A.value(enter,0),back=A.value(enter,1);assert(front>back+.5);assert.equal(back,-A.params.drop);
 const enterSamples=[];for(let t=.82;t<3.6;t+=.04){enter.elapsed=t;enterSamples.push(A.value(enter,.25));}
 assert(Math.max(...enterSamples)>.02&&Math.min(...enterSamples.filter((_,i)=>i>15))<-.004);
 enter.elapsed=0;advance(enter,4);assert.equal(enter.mode,'visible');assert.equal(A.value(enter,.5),0);
 pass('front-to-rear-lift-with-positive-and-negative-decaying-wake',{front,back,wakeMax:Math.max(...enterSamples),wakeMin:Math.min(...enterSamples.filter((_,i)=>i>15))});
 const exit=new A.Controller();exit.request('exit');advance(exit,A.params.anticipation*.8);
 const anticipation=A.value(exit,0);assert(anticipation>.15);assert.equal(A.value(exit,1),0);
 advance(exit,.6);assert(A.value(exit,0)<-2);advance(exit,2);assert.equal(exit.mode,'hidden');
 pass('exit-anticipation-before-front-to-rear-fall',{anticipation,hiddenOffset:A.value(exit,.5)});
 const atomic=new A.Controller(),baseline=new A.Controller();atomic.immediate(false);baseline.immediate(false);atomic.request('enter');baseline.request('enter');
 let maxDifference=0;
 for(let i=0;i<120;i++){
  advance(atomic,.02);advance(baseline,.02);
  const elapsed=atomic.elapsed;assert.equal(atomic.request(i%2?'enter':'exit'),false);assert.equal(atomic.elapsed,elapsed);
  for(let j=0;j<91;j++)maxDifference=Math.max(maxDifference,Math.abs(A.value(atomic,j/90)-A.value(baseline,j/90)));
 }
 assert.equal(maxDifference,0);advance(atomic,4);assert.equal(atomic.mode,'visible');assert.equal(atomic.completed,1);
 pass('120-in-flight-requests-leave-complete-appearance-clip-unchanged',{maxDifference,completed:atomic.completed});
 const gated=new D();gated.immediate(false);gated.show();gated.requestState('yellow');advance(gated,.3);gated.requestState('green');gated.requestState('red');advance(gated,1);
 assert.equal(gated.archive.state,'green');assert.equal(gated.applied,0);assert.equal(gated.view().pending,'red');
 advance(gated,8);assert.equal(gated.archive.state,'red');assert.equal(gated.applied,1);assert.equal(gated.view().pending,null);
 gated.hide();gated.requestState('yellow');advance(gated,3);assert.equal(gated.archive.state,'red');
 gated.requestState('idle');gated.requestState('green');gated.show();advance(gated,10);assert.equal(gated.archive.state,'green');
 pass('appearance-priority-and-single-latest-state-slot',{applied:gated.applied,history:gated.archive.history.length});
 const moving=new D();moving.requestState('red');advance(moving,.3);const movingBefore=moving.archive.z.value;moving.hide();moving.requestState('yellow');
 advance(moving,.4);assert.equal(moving.appearance.mode,'visible');assert(Math.abs(moving.archive.z.value-movingBefore)>0);
 assert.equal(moving.applied,1);let waited=0;while(moving.appearance.mode==='visible'&&waited<8){moving.update(1/120);waited+=1/120;}
 assert.equal(moving.appearance.mode,'exit');assert(moving.settled());moving.show();const exitGeneration=moving.appearance.generation;
 advance(moving,.6);assert.equal(moving.appearance.mode,'exit');assert.equal(moving.appearance.generation,exitGeneration);
 advance(moving,12);assert.equal(moving.archive.state,'yellow');assert.equal(moving.appearance.mode,'visible');
 pass('archive-roll-completes-before-exit-and-show-waits-for-exit',{waitedForArchive:waited,completedClips:moving.appearance.completed});
 const stress=new D();let maxRecords=0,maxPulses=0;
 for(let i=0;i<18000;i++){
  if(i%9===0)stress.requestState(['green','red','yellow','idle'][Math.floor(i/9)%4]);
  if(i%271===0)stress.hide();if(i%271===27)stress.show();
  const beforeMode=stress.appearance.mode,beforeTime=stress.appearance.elapsed,beforeState=stress.archive.state,beforeSettled=stress.settled();
  stress.update(1/60);
  if(beforeMode==='enter'||beforeMode==='exit'){
   assert(stress.appearance.mode===beforeMode||stress.appearance.mode===(beforeMode==='enter'?'visible':'hidden'));
   assert(stress.appearance.elapsed>=beforeTime);assert.equal(stress.archive.state,beforeState);
  }
  if(stress.archive.state!==beforeState)assert(beforeMode==='visible'&&beforeSettled);
  maxRecords=Math.max(maxRecords,stress.archive.records.size);maxPulses=Math.max(maxPulses,stress.archive.pulses.length);
  assert(Number.isFinite(stress.archive.x.value+stress.archive.z.value+stress.appearance.elapsed));
 }stress.requestState('red');stress.show();advance(stress,12);
 assert.equal(stress.archive.state,'red');assert.equal(stress.view().pending,null);assert(maxRecords<=8&&maxPulses<=6&&stress.archive.history.length<=32);
 pass('2000-automatic-state-requests-complete-current-clip-and-remain-bounded',{maxRecords,maxPulses,history:stress.archive.history.length,commands:stress.commands,applied:stress.applied,completedClips:stress.appearance.completed});
 browser=await chromium.launch({executablePath:require('./runtime.cjs').chromePath,headless:true});
 const page=await browser.newPage({viewport:{width:1400,height:1400},deviceScaleFactor:1}),network=[];
 page.on('pageerror',e=>result.errors.push(String(e)));page.on('request',r=>{if(/^https?:/.test(r.url()))network.push(r.url());});
 await page.addInitScript(()=>{
  window.__resources={};for(const s of ['Texture','Framebuffer','Program','Buffer','VertexArray'])for(const prefix of ['create','delete']){
   const key=prefix+s,fn=WebGL2RenderingContext.prototype[key];WebGL2RenderingContext.prototype[key]=function(...args){__resources[key]=(__resources[key]||0)+1;return fn.apply(this,args);};
  }
 });
 await page.goto(pathToFileURL(path.join(root,'index.html')).href+'?pause&auto=0&zoom=2&dpr=1&count=1&output=on');await page.waitForFunction(()=>window.RA?.debug);
 await page.evaluate(()=>{
  const d=RA.debug;window.qa=()=>{d.redraw();const g=d.renderer.gl,a=new Uint8Array(g.drawingBufferWidth*g.drawingBufferHeight*4);g.readPixels(0,0,g.drawingBufferWidth,g.drawingBufferHeight,g.RGBA,g.UNSIGNED_BYTE,a);
   let nonzero=0,bad=0;for(let i=0;i<a.length;i+=4){if(a[i+3])nonzero++;if(Math.max(a[i],a[i+1],a[i+2])>a[i+3]+1)bad++;}
   return{nonzero,bad,foregroundError:g.getError(),backgroundError:d.renderer.glass.painter.gl.getError(),glass:d.anim.glassDynamics.view().phase,panels:d.renderer.glass.lastStats?.panels};};
 });
 const stages=[];for(const dt of [.1,.25,.4,.5,1,2]){await page.evaluate(dt=>RA.debug.advance(dt),dt);const s=await page.evaluate(()=>qa());assert.equal(s.bad,0);assert.equal(s.foregroundError,0);assert.equal(s.backgroundError,0);stages.push(s);}
 assert(stages.at(-1).panels>0);pass('actual-composite-entry-frames-valid',{stages});
 await page.click('#glass-exit');await page.evaluate(()=>RA.debug.advance(3));const empty=await page.evaluate(()=>qa());assert.equal(empty.panels,0);assert(empty.nonzero>0);assert.equal(empty.backgroundError,0);
 await page.selectOption('#glass-state','red');await page.selectOption('#glass-state','yellow');await page.selectOption('#glass-state','green');
 await page.click('#glass-enter');await page.evaluate(()=>RA.debug.advance(8));assert.equal(await page.evaluate(()=>RA.debug.anim.glassDynamics.archive.state),'green');
 pass('empty-glass-renders-ground-and-latest-state-survives-hide',empty);
 await page.selectOption('#glass-state','red');await page.evaluate(()=>RA.debug.advance(.5));
 const viewBefore=await page.evaluate(()=>{const a=RA.debug.anim.glassDynamics.archive;window.ownedArchive=a;return{x:a.x.value,z:a.z.value,time:a.time};});
 await page.selectOption('#backdrop','idle');await page.selectOption('#backdrop','running');
 const viewAfter=await page.evaluate(()=>{const a=RA.debug.anim.glassDynamics.archive;return{x:a.x.value,z:a.z.value,time:a.time,same:a===window.ownedArchive};});
 assert(viewAfter.same);for(const k of ['x','z','time'])assert.equal(viewBefore[k],viewAfter[k]);
 pass('view-changes-preserve-owned-timeline',viewAfter);
 await page.evaluate(()=>RA.debug.advance(5));await page.click('#exit');await page.evaluate(()=>RA.debug.advance(.45));
 assert.equal(await page.evaluate(()=>RA.debug.anim.stage.exiting),false);
 await page.click('#reveal');assert.equal(await page.evaluate(()=>RA.debug.anim.lifecycle),'glass-exit');
 await page.evaluate(()=>RA.debug.advance(12));assert((await page.evaluate(()=>qa())).nonzero>0);
 await page.click('#exit');await page.evaluate(()=>RA.debug.advance(12));assert.equal((await page.evaluate(()=>qa())).nonzero,0);
 const hidden=await page.evaluate(()=>{const n=RA.debug.renderer.glass.draws;RA.debug.advance(2);return n===RA.debug.renderer.glass.draws;});assert(hidden);
 await page.click('#reveal');await page.evaluate(()=>RA.debug.advance(7));assert((await page.evaluate(()=>qa())).nonzero>0);
 pass('whole-exit-completes-overlapping-glass-and-foreground-before-latest-reveal');
 const resources=await page.evaluate(()=>{
  const d=RA.debug,before=JSON.stringify(__resources),slots=BG16.geometry.pool.slice();
  for(let i=0;i<120;i++){if(i%23===0)d.anim.glassDynamics.hide();if(i%23===9)d.anim.glassDynamics.show();d.advance(1/30);qa();}
  return{stable:before===JSON.stringify(__resources),pool:BG16.geometry.pool.length,sameSlots:slots.every((v,i)=>v===BG16.geometry.pool[i]),resources:__resources};
 });assert(resources.stable&&resources.sameSlots&&resources.pool===288);pass('render-resource-and-glass-pool-stability',resources);
 assert.deepEqual(result.errors,[]);assert.deepEqual(network,[]);pass('offline-no-runtime-errors');
 result.sourceHashes={};function walk(dir){for(const e of fs.readdirSync(dir,{withFileTypes:true})){const p=path.join(dir,e.name);if(e.isDirectory())walk(p);else result.sourceHashes[path.relative(root,p).replaceAll('\\','/')]=hash(p);}}
 walk(path.join(root,'engine'));walk(path.join(root,'background'));result.sourceHashes['index.html']=hash(path.join(root,'index.html'));result.result='PASS';
}catch(e){result.result='FAIL';result.failure=String(e.stack||e);console.error(e);process.exitCode=1;}
finally{if(browser)await browser.close();fs.writeFileSync(path.join(__dirname,'GLASS_REGRESSION.json'),JSON.stringify(result,null,2));}})();
