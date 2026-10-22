const fs=require('fs'),path=require('path'),vm=require('vm'),assert=require('assert'),crypto=require('crypto'),{pathToFileURL}=require('url');
const{chromium}=require('./runtime.cjs');
const root=path.resolve(__dirname,'../visual'),hash=p=>crypto.createHash('sha256').update(fs.readFileSync(p)).digest('hex');
const result={version:'0.4.9',synthetic:true,nativeIntegrated:false,productionMutation:'NONE',checks:[],errors:[]};
const pass=(name,data={})=>{result.checks.push({name,pass:true,...data});console.log('PASS',name,JSON.stringify(data));};
const context=vm.createContext({window:{},console});
for(const f of ['core','presentation','row_presentation'])vm.runInContext(fs.readFileSync(path.join(root,'engine',f+'.js'),'utf8'),context);
const {RowMotion,RateTween,presentation:P}=context.window.RA;
function advance(m,t){for(let i=0;i<Math.round(t*120);i++)m.step(1/120,36);}
(async()=>{let browser;try{
 {
 assert.equal(P.rateTweenDuration,.65);assert.equal(P.rateRefresh,0);
 const rate=new RateTween(),started=10,duration=P.rateTweenDuration,near=(a,b)=>assert(Math.abs(a-b)<1e-9);
 rate.set(100,started);near(rate.step(started),0);near(rate.step(started+duration/2),50);
 near(rate.step(started+duration),100);near(rate.velocity(started+duration),0);
 const retarget=new RateTween();retarget.set(100,started);
 const changedAt=started+.25,before=retarget.value(changedAt),incoming=retarget.velocity(changedAt),target=before+2;
 retarget.set(target,changedAt);near(retarget.value(changedAt),before);
 near(retarget.tangent,Math.min(incoming,1.5*(target-before)/duration));
 let previous=before;
 for(let i=1;i<=120;i++){
  const value=retarget.step(changedAt+duration*i/120);
  assert(value>=previous-1e-9&&value<=target+1e-9);previous=value;
 }
 near(previous,target);retarget.set(target,changedAt+duration+.1);assert.equal(retarget.start,changedAt);
 const reverse=new RateTween();reverse.set(100,started);const turning=reverse.value(changedAt);
 reverse.set(0,changedAt);near(reverse.value(changedAt),turning);assert.equal(reverse.tangent,0);
 near(reverse.step(changedAt+duration),0);
 pass('rate-tween-settles-in-650ms-and-retargets-with-a-bounded-continuous-tangent',{duration,refreshInterval:P.rateRefresh,incomingTangent:incoming,boundedTangent:retarget.tangent});
 }
 const pulse=new RowMotion();pulse.setOutput(true);advance(pulse,.05);pulse.setOutput(false);advance(pulse,2);
 assert.equal(pulse.open,0);assert.equal(pulse.completed,0);
 pass('brief-output-pulse-is-coalesced-before-opening');
 const m=new RowMotion();m.setOutput(true);advance(m,1.2);assert.equal(m.open,1);
 m.setOutput(false);advance(m,.5);assert.equal(m.open,1);
 m.setOutput(true);advance(m,.2);assert.equal(m.phase,'open');
 m.setOutput(false);advance(m,2);assert.equal(m.open,0);
 pass('short-token-gaps-hold-the-expanded-row-and-long-pause-closes');
 const atomic=new RowMotion();atomic.setOutput(true);advance(atomic,.25);
 assert.equal(atomic.phase,'opening');atomic.setOutput(false);const before=atomic.open;
 advance(atomic,.5);assert.equal(atomic.phase,'open');assert(atomic.open>before);
 advance(atomic,.5);assert.equal(atomic.phase,'closing');atomic.setOutput(true);
 let reachedClosed=false;for(let i=0;i<180;i++){atomic.step(1/120,40);if(atomic.phase==='closed')reachedClosed=true;}
 assert(reachedClosed);assert.equal(atomic.phase,'open');
 pass('expand-and-collapse-complete-before-consuming-latest-target');
 const stress=new RowMotion();let previous=stress.angle,maxAngleStep=0;
 for(let i=0;i<18000;i++){
  if(i%7===0)stress.setOutput(Math.floor(i/7)%3!==0);
  stress.step(1/60,10+(i%90));assert(stress.open>=0&&stress.open<=1);
  const delta=(stress.angle-previous+Math.PI*2)%(Math.PI*2);maxAngleStep=Math.max(maxAngleStep,delta);previous=stress.angle;
  assert(Number.isFinite(stress.angle));assert(delta<=2*Math.PI*3.61/60);
 }
 stress.setOutput(false);advance(stress,3);assert.equal(stress.phase,'closed');
 pass('2572-automatic-target-updates-keep-phase-continuous-and-converge',{maxAngleStep,completed:stress.completed});
 browser=await chromium.launch({executablePath:require('./runtime.cjs').chromePath,headless:true});
 const page=await browser.newPage({viewport:{width:1400,height:1350},deviceScaleFactor:1}),network=[];
 page.on('pageerror',e=>result.errors.push(String(e)));page.on('request',r=>{if(/^https?:/.test(r.url()))network.push(r.url());});
 await page.goto(pathToFileURL(path.join(root,'index.html')).href+'?pause&auto=0&zoom=2&dpr=1&count=1&mix=green&output=off');
 await page.waitForFunction(()=>window.RA?.debug);
 await page.evaluate(()=>{
  RA.debug.advance(8);
  window.metrics=()=>{
   const d=RA.debug;d.redraw();const l=d.labels.rowLayouts[0],p=[...d.anim.poses.values()][0];
   return{...l,phase:p?.rowMotion.phase,turns:p?.rowMotion.turns,angle:p?.rowMotion.angle,
    glyphs:d.labels.n,foregroundError:d.renderer.gl.getError(),backgroundError:d.renderer.glass.painter.gl.getError()};
  };
 });
 const closed=await page.evaluate(()=>metrics());
 assert.equal(closed.open,0);assert.equal(closed.modelX,P.collapsedSessionX);
 assert.equal(closed.rateVisibility,0);assert.equal(closed.foregroundError,0);assert.equal(closed.backgroundError,0);
 await page.locator('#stage').screenshot({path:path.join(__dirname,'waiting-left-aligned.png')});
 pass('waiting-model-starts-at-fixed-left-anchor',{modelX:closed.modelX,center:closed.center,turns:closed.turns});
 await page.selectOption('#count','5');await page.evaluate(()=>RA.debug.advance(5));
 const waitingRows=await page.evaluate(()=>RA.debug.labels.rowLayouts.map(l=>({model:l.name.text,x:l.modelX,orbX:l.orbX,width:l.name.width,open:l.open})));
 assert.equal(waitingRows.length,5);assert(new Set(waitingRows.map(l=>l.width)).size>1);
 assert(waitingRows.every(l=>l.x===P.collapsedSessionX&&l.orbX===P.collapsedSessionX-P.orbOffset&&l.open===0));
 await page.locator('#stage').screenshot({path:path.join(__dirname,'five-rows-waiting.png')});
 pass('different-length-waiting-models-share-one-left-anchor',{rows:waitingRows});
 await page.selectOption('#count','1');await page.evaluate(()=>RA.debug.advance(5));
 await page.selectOption('#output','on');await page.evaluate(()=>RA.debug.advance(3));
 const open=await page.evaluate(()=>metrics());
 assert.equal(open.open,1);assert(open.modelX<=closed.modelX-20);assert.equal(open.rulerStart,open.modelX);
 assert.equal(open.rulerEnd,open.expandedRight);
 assert.equal(open.modelX,P.sessionX);assert.equal(open.rateRight,388-P.rowRightInset);
 assert.equal(open.rulerEnd-open.rulerStart,196);
 assert(open.turns>closed.turns*30);assert.equal(open.foregroundError,0);
 await page.locator('#stage').screenshot({path:path.join(__dirname,'output-expanded.png')});
 pass('output-expands-name-rate-guide-together-and-accelerates-orb',{modelX:open.modelX,nameShift:closed.modelX-open.modelX,turns:open.turns,ruler:[open.rulerStart,open.rulerEnd]});
 const digits=await page.evaluate(()=>{
  // Exercise the production snapshot/tween/text path without the demo clock
  // replacing the requested rates. Layout uses the actual warmed glyph atlas.
  const d=RA.debug,a=new RA.Animator(),labels=new RA.Labels(d.atlas),samples=[];
  const snapshot=RA.synth(d.simT,{count:1,mix:'green',output:'on',quota:'87',pending:false});
  snapshot.rows[0].estimated_visible_tokens_per_second=1.2;a.set(snapshot,0,500);a.showImmediate();
  for(let i=0;i<180;i++)a.frame(1/60,0);
  for(const value of [1.2,9.9,19.9,199.9,999]){
   snapshot.rows[0].estimated_visible_tokens_per_second=value;a.set(snapshot,0,500);
   const texts=[],positions=[],rights=[];let scene;
   for(let i=0;i<=Math.ceil(RA.presentation.rateTweenDuration*60);i++){
    scene=a.frame(1/60,0);labels.build(scene);const l=labels.rowLayouts[0];
    texts.push(scene.rows[0].rateText);positions.push(l.modelX);rights.push(l.rateRight);
   }
   samples.push({target:value,rate:scene.rows[0].rate,text:scene.rows[0].rateText,
    open:scene.rows[0].rateOp,modelX:positions.at(-1),rateRight:rights.at(-1),
    modelStable:positions.every(x=>x===positions[0]),rightStable:rights.every(x=>x===rights[0]),
    distinctTexts:new Set(texts).size});
  }
  return samples;
 });
 assert(digits.every(x=>Math.abs(x.rate-x.target)<1e-9&&x.text===x.target.toFixed(1)));
 assert(digits.every(x=>x.open===1&&x.modelStable&&x.rightStable&&x.modelX===P.sessionX&&x.rateRight===388-P.rowRightInset));
 assert(digits.at(-1).distinctTexts>20,'The 0.65-second digit tween must update on presented frames, without a 0.1-second text throttle');
 pass('throughput-digit-count-does-not-shift-the-model', {samples:digits});
 const grid=await page.evaluate(()=>{
  const d=RA.debug,L=RA.layers.enabled,was={...L};for(const key of Object.keys(L))L[key]=key==='frame';
  function sample(){
   d.redraw();const g=d.renderer.gl,layout=d.labels.rowLayouts[0],scale=g.drawingBufferWidth/(RA.PANEL_W+2*RA.MARGIN);
   const buf=new Uint8Array(g.drawingBufferWidth*g.drawingBufferHeight*4);g.readPixels(0,0,g.drawingBufferWidth,g.drawingBufferHeight,g.RGBA,g.UNSIGNED_BYTE,buf);
   const alpha=(x,y)=>{const px=Math.floor((x+RA.MARGIN)*scale),py=g.drawingBufferHeight-1-Math.floor((y+RA.MARGIN)*scale);return buf[(py*g.drawingBufferWidth+px)*4+3];};
   let guide=0,vertical=0;
   for(let x=layout.rulerStart+5;x<layout.rulerEnd-4;x+=.5)for(let y=layout.guideY-.5;y<layout.guideY+3;y+=.5)guide+=alpha(x,y);
   for(let y=34;y<d.anim.H.x-24;y+=.5)vertical+=alpha(150.5,y);
   return{guide,vertical};
  }
  const output=sample();document.getElementById('output').value='off';document.getElementById('output').dispatchEvent(new Event('change',{bubbles:true}));d.advance(3);
  const waiting=sample();Object.assign(L,was);d.redraw();return{output,waiting};
 });
 assert(grid.output.guide>100);assert.equal(grid.waiting.guide,0);assert.equal(grid.waiting.vertical,0);assert.equal(grid.output.vertical,0);
 pass('actual-frame-layer-hides-idle-guide-and-removes-vertical-ruler',grid);
 const variants=[];
 for(const count of ['2','5','16']){
  await page.selectOption('#count',count);await page.selectOption('#output','on');await page.evaluate(()=>RA.debug.advance(4));
  const v=await page.evaluate(()=>{
   const d=RA.debug;d.redraw();return{rows:d.labels.rowLayouts.map(l=>({left:l.groupLeft,modelX:l.modelX,rateRight:l.rateRight,right:l.expandedRight,gap:l.rateRight-l.rateWidth-l.nameEnd,open:l.open})),
    glyphs:d.labels.n,error:d.renderer.gl.getError()};
  });
  assert.equal(v.rows.length,Number(count));assert(v.rows.every(l=>l.open===1&&l.modelX===P.sessionX&&l.rateRight===388-P.rowRightInset&&l.left>=P.infoLeft-.01&&l.right<=388-P.infoRight+.01&&l.gap>=P.modelRateGap-.01));assert(v.glyphs<=640);assert.equal(v.error,0);variants.push({count,...v});
  if(count==='5')await page.locator('#stage').screenshot({path:path.join(__dirname,'five-rows-expanded.png')});
 }
 pass('two-five-and-sixteen-rows-have-fixed-left-and-right-alignment',{variants:variants.map(x=>({count:x.count,glyphs:x.glyphs,maxRight:Math.max(...x.rows.map(r=>r.right))}))});
 const long=await page.evaluate(()=>{
  const d=RA.debug;const s=RA.synth(d.simT,{count:1,mix:'green',output:'on',quota:'87',pending:false});
  s.rows[0].model='a-very-long-model-name-with-a-large-context-and-additional-capabilities';
  d.anim.set(s,0,500);let scene;
  for(let i=0;i<300;i++)scene=d.anim.frame(1/60,0);
  d.labels.build(scene);const l=d.labels.rowLayouts[0];return{display:l.name.text,left:l.groupLeft,right:l.expandedRight,glyphs:d.labels.n};
 });
 assert(long.display.endsWith('…'));assert(long.left>=P.infoLeft-.01&&long.right<=388-P.infoRight+.01);assert(long.glyphs<=640);
 pass('long-model-labels-fit-the-row-without-overlapping-speed',long);
 const stale=await page.evaluate(()=>{
  const d=RA.debug,s=RA.synth(d.simT,{count:1,mix:'green',output:'off',quota:'87',pending:false});
  Object.assign(s.rows[0],{speed_fresh:false,speed_valid:true,tokens_per_second:99});
  d.anim.set(s,0,500);for(let i=0;i<240;i++)d.anim.frame(1/60,0);
  return{phase:[...d.anim.poses.values()][0].rowMotion.phase};
 });assert.equal(stale.phase,'closed');pass('stale-completion-sample-does-not-revive-output');
 assert.deepEqual(result.errors,[]);assert.deepEqual(network,[]);pass('local-preview-has-no-network-requests-or-runtime-errors');
 result.sourceHashes={};
 for(const dir of ['engine','background']){
  function walk(folder){for(const item of fs.readdirSync(folder,{withFileTypes:true})){const p=path.join(folder,item.name);if(item.isDirectory())walk(p);else result.sourceHashes[path.relative(root,p).replaceAll('\\','/')]=hash(p);}}walk(path.join(root,dir));
 }
 result.sourceHashes['index.html']=hash(path.join(root,'index.html'));result.result='PASS';
}catch(e){result.result='FAIL';result.failure=String(e.stack||e);console.error(e);process.exitCode=1;}
finally{if(browser)await browser.close();fs.writeFileSync(path.join(__dirname,'RESULT.json'),JSON.stringify(result,null,2));}})();
