const fs=require('fs'),path=require('path'),assert=require('assert'),{pathToFileURL}=require('url');
const{chromium}=require('./runtime.cjs');
const result={version:'0.4.9',checks:[],errors:[]};
(async()=>{let browser;try{
 browser=await chromium.launch({executablePath:require('./runtime.cjs').chromePath,headless:true});
 async function sample(){
  const p=await browser.newPage();p.on('pageerror',e=>result.errors.push(String(e)));
  await p.goto(pathToFileURL(path.resolve(__dirname,'../visual/index.html')).href+'?pause&auto=0');
  await p.waitForFunction(()=>window.RA?.debug);
  const values=await p.evaluate(()=>[0,1,16].map(count=>{
   const a=new RA.Animator(),dt=1/120;a.hideImmediate();
   a.set(RA.synth(0,{count,mix:'green',output:'on',quota:'87'}));a.beginReveal();
   let enter=null,entryEnd=null,closed=null,exitStart=null,fall=null,curtain=null,glassHidden=null,hidden=null;
   for(let i=0;i<840;i++){
    a.frame(dt,0);
    if(enter===null&&a.glassAppearance.mode==='enter')enter=a.t;
    if(enter!==null&&entryEnd===null&&a.glassAppearance.mode==='visible')entryEnd=a.t;
   }
   const start=a.t;a.beginExit();
   for(let i=0;i<600;i++){
    const s=a.frame(dt,0);
    if(closed===null&&s.rows.every(r=>r.rateOp===0))closed=a.t-start;
    if(exitStart===null&&a.glassAppearance.mode==='exit')exitStart=a.t-start;
    if(fall===null&&a.glassAppearance.mode==='exit'&&a.glassAppearance.elapsed>=BG16.appearance.params.anticipation)fall=a.t-start;
    if(curtain===null&&s.T.exit>0)curtain=a.t-start;
    if(glassHidden===null&&a.glassAppearance.mode==='hidden')glassHidden=a.t-start;
    if(hidden===null&&a.lifecycle==='hidden')hidden=a.t-start;
   }
   return{count,enter,entryEnd,entryDuration:entryEnd-enter,closed,exitStart,fall,curtain,glassHidden,hidden,
    params:{rise:BG16.appearance.params.rise,wakeDelay:BG16.appearance.params.wakeDelay,anticipation:BG16.appearance.params.anticipation,fall:BG16.appearance.params.fall}};
  }));
  await p.close();return values;
 }
 const current=await sample();
 for(const after of current){
  assert(Math.abs(after.enter-.30)<.018);
  assert(Math.abs(after.entryDuration-3.92)<.018);
  assert.equal(after.params.rise,1.22);assert.equal(after.params.wakeDelay,1.07);
  assert.equal(after.params.anticipation,.12);assert.equal(after.params.fall,.82);
  if(after.count){
   assert(Math.abs(after.closed-.5)<.018);
   assert(Math.abs(after.exitStart-.4083333333)<.018);
   assert(Math.abs(after.fall-.525)<.018);
   assert(Math.abs(after.hidden-2.525)<.018);
  }else{assert(after.exitStart>=0&&after.exitStart<.018);}
  assert(after.fall>=after.closed&&Math.abs(after.fall-after.curtain)<.009&&after.hidden>after.fall);
  result.checks.push({name:`${after.count}-rows-approved-entry-advance-and-preserved-exit`,pass:true,current:after});
 }
 assert.deepEqual(result.errors,[]);result.result='PASS';console.log(JSON.stringify(result));
}catch(e){result.result='FAIL';result.failure=String(e.stack||e);console.error(e);process.exitCode=1;}
finally{if(browser)await browser.close();fs.writeFileSync(path.join(__dirname,'TIMING_RESULT.json'),JSON.stringify(result,null,2));}})();
