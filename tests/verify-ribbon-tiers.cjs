const fs=require('fs'),path=require('path'),assert=require('assert'),{pathToFileURL}=require('url');
const{chromium}=require('./runtime.cjs');
const result={version:'0.4.9',checks:[],errors:[]},root=path.resolve(__dirname,'../visual');
const pass=(name,data)=>{result.checks.push({name,pass:true,...data});console.log('PASS',name,JSON.stringify(data));};
(async()=>{let browser;try{
 browser=await chromium.launch({executablePath:require('./runtime.cjs').chromePath,headless:true});
 const page=await browser.newPage({viewport:{width:1400,height:1400},deviceScaleFactor:1});
 page.on('pageerror',e=>result.errors.push(String(e)));
 await page.goto(pathToFileURL(path.join(root,'index.html')).href+'?pause&auto=0&zoom=2&dpr=1&count=1&output=off');
 await page.waitForFunction(()=>window.RA?.debug);
 const tiers=await page.evaluate(()=>{
  const make=n=>Array.from({length:n},(_,i)=>({id:'tier-'+i,y:40+18*i,alive:true,outputting:false,rate:0,energy:.1}));
  const stage=new RA.Stage(),T=stage.timeline(),tiers=[];
  window.tierRibbons=[];
  for(const count of [0,1,2,3,4,5,16]){
   const r=new RA.Ribbons();r.sync(make(count),0);
   for(let i=0;i<360;i++)r.update(1/60,(i+1)/60,170,stage,T,0,0,.1);
   tiers.push({sessions:count,curves:r.count,opacities:Array.from({length:r.count},(_,i)=>r.C[i*4+2]),details:r.details.length});
   if([1,2,4].includes(count))tierRibbons.push(r);
  }
  const r=new RA.Ribbons(),owned=r.details.slice(),arraySizes=[r.A.length,r.B.length,r.C.length,r.D.length],before=r.details.map(d=>d.phase);
  let maxSources=0,maxCurves=0;
  for(let i=0;i<12000;i++){
   if(i%5===0)r.sync(make([1,2,3,4,16,0][Math.floor(i/5)%6]),i/60);
   r.update(1/60,i/60,170,stage,T,0,0,.1);
   maxSources=Math.max(maxSources,r.slots.length);maxCurves=Math.max(maxCurves,r.count);
   if(r.C.some(v=>!Number.isFinite(v)))throw Error('Invalid ribbon state');
  }
  r.sync(make(1),201);for(let i=0;i<360;i++)r.update(1/60,201+i/60,170,stage,T,0,0,.1);
  return{tiers,stress:{sameObjects:owned.every((v,i)=>v===r.details[i]),arraySizes,finalCurves:r.count,maxCurves,maxSources,
   continuousPhases:r.details.every((d,i)=>d.phase>before[i])}};
 });
 assert.deepEqual(tiers.tiers.map(t=>t.curves),[2,2,4,4,6,6,6]);assert(tiers.tiers.every(t=>t.details===4));
 pass('two-and-four-session-thresholds-add-two-visible-bands-each',{tiers:tiers.tiers});
 assert(tiers.stress.sameObjects&&tiers.stress.continuousPhases&&tiers.stress.finalCurves===2&&tiers.stress.maxCurves===6&&tiers.stress.maxSources<=32);
 pass('rapid-threshold-reversals-reuse-four-detail-objects-and-converge',tiers.stress);
 const energy=await page.evaluate(()=>{
  const d=RA.debug;d.advance(8);const scene=d.anim.frame(0,0),was={...RA.layers.enabled};
  scene.H=170;scene.dt=0;
  for(const k of Object.keys(RA.layers.enabled))RA.layers.enabled[k]=k==='curves';
  const out=[];
  for(const r of tierRibbons){
   scene.ribbons=r;d.labels.build(scene);d.renderer.render(scene,d.labels,d.parts,{scale:2,quality:0,glow:false,backdrop:'running',glassFocus:true});
   const g=d.renderer.gl,b=new Uint8Array(g.drawingBufferWidth*g.drawingBufferHeight*4);
   g.readPixels(0,0,g.drawingBufferWidth,g.drawingBufferHeight,g.RGBA,g.UNSIGNED_BYTE,b);
   let ink=0;for(let i=3;i<b.length;i+=4)ink+=b[i];out.push({curves:r.count,ink,error:g.getError()});
  }
  Object.assign(RA.layers.enabled,was);d.redraw();return out;
 });
 assert(energy.every(e=>e.error===0));
 assert(energy[1].ink>energy[0].ink*1.04&&energy[2].ink>energy[1].ink*1.04);
 pass('actual-rendered-curve-coverage-increases-at-both-thresholds',{samples:energy});
 for(const n of [1,2,4]){
  await page.selectOption('#count',String(n));await page.evaluate(()=>RA.debug.advance(5));
  await page.locator('#stage').screenshot({path:path.join(__dirname,`curves-${n}-sessions.png`)});
 }
 assert.deepEqual(result.errors,[]);result.result='PASS';
}catch(e){result.result='FAIL';result.failure=String(e.stack||e);console.error(e);process.exitCode=1;}
finally{if(browser)await browser.close();fs.writeFileSync(path.join(__dirname,'RIBBON_TIERS_RESULT.json'),JSON.stringify(result,null,2));}})();
