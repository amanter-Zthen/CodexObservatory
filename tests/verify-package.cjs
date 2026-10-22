'use strict';
const fs=require('fs'),path=require('path'),crypto=require('crypto'),assert=require('assert'),{pathToFileURL}=require('url');
const {chromium,chromePath}=require('./runtime.cjs');
const root=path.resolve(__dirname,'..'),visual=path.join(root,'visual');
fs.mkdirSync(path.join(root,'verification'),{recursive:true});
const result={version:'0.4.9',checks:[],errors:[],externalRequests:[]};
const pass=(name,data={})=>result.checks.push({name,pass:true,...data});
(async()=>{let browser;try{
 const lock=JSON.parse(fs.readFileSync(path.join(root,'layers.lock.json')));
 for(const[f,h]of Object.entries(lock.runtimeHashes)){
  assert.equal(crypto.createHash('sha256').update(fs.readFileSync(path.join(visual,f))).digest('hex'),h,f);
 }
 assert.equal(lock.layers.length,6);assert(lock.layers.every(x=>x.files.length&&x.version==='0.4.9'));
 pass('all-six-layers-and-shared-runtime-match-lock',{files:Object.keys(lock.runtimeHashes).length});
 const html=fs.readFileSync(path.join(visual,'index.html'),'utf8'),refs=[...html.matchAll(/(?:src|href)="([^"#?]+)[^"]*"/g)].map(m=>m[1]);
 for(const ref of refs){assert(!/^[a-z]+:/i.test(ref),ref);assert(fs.existsSync(path.resolve(visual,ref)),ref);}
 pass('preview-assets-and-documentation-links-resolve',{references:refs.length});
 browser=await chromium.launch({executablePath:chromePath,headless:true});
 const p=await browser.newPage({viewport:{width:1400,height:1400},deviceScaleFactor:1});
 p.on('pageerror',e=>result.errors.push(String(e)));
 p.on('request',r=>{if(/^https?:/.test(r.url()))result.externalRequests.push(r.url());});
 await p.goto(pathToFileURL(path.join(visual,'index.html')).href+'?pause&auto=0&count=3&mix=mixed&output=off&zoom=2&dpr=1');
 await p.waitForFunction(()=>window.RA?.debug);
 await p.evaluate(()=>RA.debug.advance(12));
 const singles=[];
 for(const layer of ['background','curves','particles','frame','info','glow']){
  await p.locator(`[data-solo="${layer}"]`).click();
  const sample=await p.evaluate(()=>{
   const d=RA.debug;d.redraw();const g=d.renderer.gl,b=new Uint8Array(g.drawingBufferWidth*g.drawingBufferHeight*4);
   g.readPixels(0,0,g.drawingBufferWidth,g.drawingBufferHeight,g.RGBA,g.UNSIGNED_BYTE,b);
   let sum=0,nonzero=0;for(let i=0;i<b.length;i++){sum=(sum+b[i]*(i%31+1))%4294967291;if((i&3)===3&&b[i])nonzero++;}
   return{sum,nonzero,error:g.getError()};
  });
  assert(sample.nonzero>0);assert.equal(sample.error,0);
  singles.push({layer,...sample});
  await p.locator('[data-solo="'+layer+'"]').click();
 }
 assert.equal(new Set(singles.map(s=>s.sum)).size,6);pass('six-layer-solo-controls-produce-distinct-real-pixels',{singles});
 await p.locator('#layers-all').click();
 await p.locator('#stage').screenshot({path:path.join(root,'verification/frozen-preview.png')});
 const complete=await p.evaluate(()=>Object.values(RA.layers.enabled).every(Boolean));
 assert(complete);pass('full-composite-restores-after-all-layer-isolations');
 assert.deepEqual(result.errors,[]);assert.deepEqual(result.externalRequests,[]);
 pass('standalone-file-preview-has-no-runtime-errors-or-http-requests');
 result.result='PASS';
}catch(e){result.result='FAIL';result.failure=String(e.stack||e);console.error(e);process.exitCode=1;}
finally{if(browser)await browser.close();fs.writeFileSync(path.join(root,'verification/package-smoke.json'),JSON.stringify(result,null,2));}})();
