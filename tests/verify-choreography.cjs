const fs=require('fs'),path=require('path'),assert=require('assert'),{pathToFileURL}=require('url');
const{chromium}=require('./runtime.cjs');
const result={version:'0.4.9',checks:[],errors:[]};
const pass=(name,data)=>{result.checks.push({name,pass:true,...data});console.log('PASS',name,JSON.stringify(data));};
(async()=>{let browser;try{
 browser=await chromium.launch({executablePath:require('./runtime.cjs').chromePath,headless:true});
 const page=await browser.newPage({viewport:{width:1400,height:1350},deviceScaleFactor:1}),network=[];
 page.on('pageerror',e=>result.errors.push(String(e)));page.on('request',r=>{if(/^https?:/.test(r.url()))network.push(r.url());});
 await page.goto(pathToFileURL(path.resolve(__dirname,'../visual/index.html')).href+'?pause&auto=0&zoom=2&dpr=1&count=1&output=on');
 await page.waitForFunction(()=>window.RA?.debug);
 const timeline=await page.evaluate(()=>{
  const P=RA.presentation,step=1/120,check=(v,m)=>{if(!v)throw Error(m);};
  const snapshot=(n=1)=>RA.synth(0,{count:n,mix:'green',output:'on',quota:'87',pending:false});
  const tick=(a,t)=>{let s;for(let i=0;i<Math.round(t/step);i++)s=a.frame(step,0);return s;};
  const a=new RA.Animator();a.hideImmediate();a.set(snapshot());a.beginReveal();
  let fullAt=null,expandedAt=null,early=0;
  for(let i=0;i<960;i++){
   const s=a.frame(step,0),r=s.rows[0];
   if(r.reveal>=.999999&&r.mix>=.999&&fullAt===null)fullAt=a.t;
   if(r.rateOp>0){
    if(r.reveal<.999999||r.mix<.999||a.lifecycle!=='visible')early++;
    if(expandedAt===null)expandedAt=a.t;
   }
  }
  check(!early&&expandedAt>fullAt+P.rowNameHold-.02,'Expansion preceded the complete model name');
  const entry={fullAt,expandedAt,early};
  a.beginExit();const exitAt=a.t;let rowClosed=null,glassExit=null,glassFall=null,textExit=null,firstHidden=null,restored=null,earlyDissolves=0;
  const phases=[];let phase='';
  for(let i=0;i<1800;i++){
   if(i===18)a.beginReveal(); // New intent while closing: finish the old lifecycle.
   const s=a.frame(step,0),r=s.rows[0];
   if(a.lifecycle!==phase){phase=a.lifecycle;phases.push(phase);}
   if(rowClosed===null&&r.rateOp===0)rowClosed=a.t-exitAt;
   if(glassExit===null&&a.glassAppearance.mode==='exit')glassExit=a.t-exitAt;
   const falling=a.glassAppearance.mode==='exit'&&a.glassAppearance.elapsed>=BG16.appearance.params.anticipation;
   if(glassFall===null&&falling)glassFall=a.t-exitAt;
   if(textExit===null&&r.dissolve>0)textExit=a.t-exitAt;
   if((r.dissolve>0||falling)&&r.rateOp>0)earlyDissolves++;
   if(a.lifecycle==='hidden'&&firstHidden===null)firstHidden=a.t-exitAt;
   if(firstHidden!==null&&r.rateOp===1&&restored===null)restored=a.t-exitAt;
  }
  check(rowClosed!==null&&glassExit>=rowClosed-P.glassExitLead-.02&&glassFall>=rowClosed&&textExit>=glassFall&&!earlyDissolves,'Falling or dissolving started before the row retracted');
  check(firstHidden!==null&&restored>firstHidden,'Latest reveal skipped the complete exit');
  const exit={rowClosed,glassExit,glassFall,textExit,firstHidden,restored,earlyDissolves,phases};
  const removed=new RA.Animator();removed.set(snapshot());tick(removed,5);removed.set(snapshot(0));
  let removedEarly=0,closedBeforeRemoval=false;
  for(let i=0;i<480;i++){
   const s=removed.frame(step,0),r=s.rows[0];
   if(r?.rateOp===0)closedBeforeRemoval=true;
   if(r?.dissolve>0&&r.rateOp>0)removedEarly++;
  }
  check(!removedEarly&&closedBeforeRemoval&&removed.poses.size===0,'A removed model dissolved before retracting');
  const removal={removedEarly,closedBeforeRemoval,remainingPoses:removed.poses.size};
  const rename=new RA.Animator();rename.set(snapshot());tick(rename,5);
  let next=snapshot();next.rows[0].model='gpt-6-sol';rename.set(next);tick(rename,.15);
  next=snapshot();next.rows[0].model='gpt-6.1-sol-mini';rename.set(next);
  let premature=0;
  for(let i=0;i<960;i++){
   const s=rename.frame(step,0),r=s.rows[0];
   if(r.mix<.999&&r.rateOp>0)premature++;
  }
  const renamed=[...rename.poses.values()][0];
  check(!premature&&renamed.model==='gpt-6.1-sol-mini'&&renamed.rowMotion.phase==='open','Rename sequencing failed');
  const nameChange={premature,finalModel:renamed.model,phase:renamed.rowMotion.phase};
  const many=new RA.Animator();many.hideImmediate();many.set(snapshot(16));many.beginReveal();let earlyRows=0,s;
  for(let i=0;i<1080;i++){
   s=many.frame(step,0);
   earlyRows+=s.rows.filter(r=>r.rateOp>0&&(r.reveal<.999999||r.mix<.999)).length;
  }
  check(!earlyRows&&s.rows.length===16&&s.rows.every(r=>r.rateOp===1&&r.reveal===1),'Some lower rows never completed entry');
  const height=many.H.x;many.set(snapshot(0));let clippedWhileRetiring=0;
  for(let i=0;i<480;i++){s=many.frame(step,0);if(many.poses.size&&s.H<height-.1)clippedWhileRetiring++;}
  check(!clippedWhileRetiring&&many.poses.size===0,'Window shrank before retiring rows completed');
  const edge=new RA.Animator();edge.set(snapshot());
  for(let i=0;i<600&&[...edge.poses.values()][0].rowMotion.phase!=='opening';i++)edge.frame(step,0);
  check([...edge.poses.values()][0].rowMotion.phase==='opening','Opening edge case did not start');
  edge.beginExit();let prematureFall=0,edgeHidden=false,edgeRestored=false;
  for(let i=0;i<480;i++){
   if(i===3)edge.beginReveal();
   const s=edge.frame([1/30,.1,1/60,1/120][i%4],0),r=s.rows[0];
   const falling=edge.glassAppearance.mode==='exit'&&edge.glassAppearance.elapsed>=BG16.appearance.params.anticipation;
   if((falling||r.dissolve>0)&&r.rateOp>0)prematureFall++;
   if(edge.lifecycle==='hidden')edgeHidden=true;
   if(edgeHidden&&r.rateOp===1)edgeRestored=true;
  }
  check(!prematureFall&&edgeHidden&&edgeRestored,'Exit during opening or uneven frame intervals broke row priority');
  return{entry,exit,removal,nameChange,multiple:{earlyRows,clippedWhileRetiring,height},openingInterruption:{prematureFall,edgeHidden,edgeRestored}};
 });
 for(const [name,data]of Object.entries(timeline))pass('ordered-'+name,data);
 const geometry=await page.evaluate(()=>{
  const d=RA.debug;d.advance(8);
  const scene=d.anim.frame(0,0),r=scene.rows[0],saved={...RA.layers.enabled};
  for(const k of Object.keys(RA.layers.enabled))RA.layers.enabled[k]=k==='frame';
  const samples=[];
  for(const amount of [0,.15,.35,.65,1]){
   r.rateOp=amount;d.labels.build(scene);d.renderer.uploadAtlas(d.atlas);
   d.renderer.render(scene,d.labels,d.parts,{scale:2,quality:0,glow:false,backdrop:'running',glassFocus:true});
   const l=d.labels.rowLayouts[0],g=d.renderer.gl,buf=new Uint8Array(g.drawingBufferWidth*g.drawingBufferHeight*4);
   g.readPixels(0,0,g.drawingBufferWidth,g.drawingBufferHeight,g.RGBA,g.UNSIGNED_BYTE,buf);
   const alpha=(x,y)=>{const px=Math.floor((x+RA.MARGIN)*2),py=g.drawingBufferHeight-1-Math.floor((y+RA.MARGIN)*2);return buf[(py*g.drawingBufferWidth+px)*4+3];};
   const cap=(x)=>{let v=0;for(let dx=-.5;dx<=.5;dx+=.5)for(let dy=-.5;dy<=.5;dy+=.5)v=Math.max(v,alpha(x+dx,l.guideY+dy));return v;};
   samples.push({amount,left:l.rulerStart,right:l.rulerEnd,origin:l.rulerOrigin,
    ink:alpha(l.rulerOrigin+3.5,l.guideY),leftCap:cap(l.rulerStart),rightCap:cap(l.rulerEnd),error:g.getError()});
  }
  Object.assign(RA.layers.enabled,saved);d.redraw();
  return samples;
 });
 assert.equal(geometry[0].left,geometry[0].right);assert.equal(geometry[0].ink,0);
 for(let i=1;i<geometry.length;i++){
  assert(geometry[i].left<geometry[i-1].left&&geometry[i].right>geometry[i-1].right);
  assert(geometry[i].leftCap>geometry[i].ink&&geometry[i].rightCap>geometry[i].ink);
  assert.equal(geometry[i].error,0);
 }
 assert(Math.max(...geometry.slice(1).map(x=>x.ink))-Math.min(...geometry.slice(1).map(x=>x.ink))<=1);
 pass('actual-guide-retracts-geometrically-with-two-caps-and-constant-ink',{samples:geometry});
 const curve=await page.evaluate(()=>{
  const values=Array.from({length:101},(_,i)=>RA.rowTravel(i/100));
  return{min:Math.min(...values),max:Math.max(...values),monotonic:values.every((v,i)=>!i||v>=values[i-1]),
   firstStep:values[1]-values[0],middleStep:values[51]-values[50],lastStep:values[100]-values[99]};
 });
 assert(curve.monotonic&&curve.min===0&&curve.max===1);
 assert(curve.firstStep<curve.middleStep/20&&curve.lastStep<curve.middleStep/20);
 pass('shared-travel-curve-starts-and-settles-without-overshoot',curve);
 assert.deepEqual(result.errors,[]);assert.deepEqual(network,[]);result.result='PASS';
}catch(e){result.result='FAIL';result.failure=String(e.stack||e);console.error(e);process.exitCode=1;}
finally{if(browser)await browser.close();fs.writeFileSync(path.join(__dirname,'CHOREOGRAPHY_RESULT.json'),JSON.stringify(result,null,2));}})();
