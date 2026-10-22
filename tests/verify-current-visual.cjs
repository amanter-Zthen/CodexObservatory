'use strict';
const fs=require('fs'),path=require('path'),crypto=require('crypto'),assert=require('assert'),{pathToFileURL}=require('url');
const {chromium,chromePath}=require('./runtime.cjs');
const root=path.resolve(__dirname,'..'),out=path.join(root,'verification/current-visual');
fs.mkdirSync(out,{recursive:true});
const result={at:new Date().toISOString(),checks:[],errors:[],requests:[],scope:'Offline current six-layer scene and ordinary browser scheduler'};
const check=(name,pass,detail={})=>{result.checks.push({name,pass,...detail});assert(pass,name);};
(async()=>{
 let browser;
 try{
  browser=await chromium.launch({headless:true,executablePath:chromePath});
  const page=await browser.newPage({viewport:{width:1200,height:1100}});
  page.on('pageerror',e=>result.errors.push(e.message));page.on('request',r=>{if(/^https?:/.test(r.url()))result.requests.push(r.url());});
  await page.goto(pathToFileURL(path.join(root,'visual/index.html')).href+'?pause&auto=0&count=0&output=on&zoom=1&dpr=1');
  await page.waitForFunction(()=>window.RA?.debug);
  const camera=await page.evaluate(()=>{
   const d=RA.debug,g=d.renderer.gl,samples=[];
   const draw=seconds=>{g.finish();if(seconds>0)d.advance(seconds,1/120);else d.redraw();g.finish();const s=d.anim.frame(0,0);return{crop:d.renderer.lastCrop,dock:s.quota.dock,rows:s.rows.length,phase:d.anim.lifecycle};};
   const set=n=>{const e=document.getElementById('count');e.value=String(n);e.dispatchEvent(new Event('change'));};
   const idle=draw(7);set(1);const immediately=draw(0);
   for(let i=0;i<50;i++)samples.push(draw(.05));
   const running=samples.at(-1);set(0);const returning=[];
   for(let i=0;i<100;i++)returning.push(draw(.05));
   const final=returning.at(-1),beforeBurst=draw(.01);set(1);draw(.2);set(0);draw(.1);set(1);const burst=draw(3);
   return{profile:d.opt.backdrop,idle,immediately,samples,running,returning,final,beforeBurst,burst};
  });
  result.camera=camera;
  check('automatic-idle-camera-is-closer',camera.profile==='auto'&&Math.abs(camera.idle.crop.width-954/1.08)<.1,{width:camera.idle.crop.width,original:954});
  check('camera-does-not-jump-when-a-model-arrives',Math.abs(camera.immediately.crop.center[0]-camera.idle.crop.center[0])<.3);
  const all=[...camera.samples,...camera.returning];
  check('camera-shares-the-quota-travel-curve',all.every(s=>Math.abs(s.crop.blend-s.dock)<1e-6&&Math.abs(s.crop.center[0]-(855+189.5*s.dock))<1e-5));
  check('running-camera-pulls-back-and-reaches-third-frame',camera.running.crop.width>camera.idle.crop.width*1.14&&Math.abs(camera.running.crop.center[0]-1044.5)<.1);
  check('empty-camera-returns-after-row-retirement',Math.abs(camera.final.crop.center[0]-855)<.1&&camera.final.rows===0);
  check('camera-crop-stays-bounded-through-retargeting',[...all,camera.burst].every(s=>s.crop.x>=0&&s.crop.y>=0&&s.crop.x+s.crop.width<=1672.001&&s.crop.y+s.crop.height<=941.001));
  const text=await page.evaluate(()=>{
   const d=RA.debug,r=d.renderer,g=r.gl,rows=[];
   const take=(s)=>{
    d.labels.build(s);r.uploadAtlas(d.atlas);r.render(s,d.labels,d.parts,{scale:1,quality:0,glow:true,backdrop:'running',glassFocus:true});g.finish();
    const buf=new Uint8Array(r.w*r.h*4);g.readPixels(0,0,r.w,r.h,g.RGBA,g.UNSIGNED_BYTE,buf);
    const l=d.labels.rowLayouts[0],row=s.rows[0],rect=(x,w)=>{
     const bytes=[];for(let y=Math.floor(row.y-row.font);y<Math.ceil(row.y+row.font*.6);y++)for(let dx=Math.floor(x);dx<Math.ceil(x+w);dx++){
      const i=((r.h-1-(y+RA.MARGIN))*r.w+dx+RA.MARGIN)*4;bytes.push(buf[i],buf[i+1],buf[i+2],buf[i+3]);
     }return bytes;
    };
    const shape=[];for(let i=0;i<d.labels.n;i++)shape.push(...d.labels.buf.slice(i*RA.GLYPH_FLOATS,i*RA.GLYPH_FLOATS+4));
    return{name:rect(l.modelX,l.name.width+4),rate:rect(l.rateRight-l.rateWidth,l.rateWidth+3),shape};
   };
   const delta=(a,b)=>a.reduce((n,v,i)=>n+(Math.abs(v-b[i])>1),0);
   for(const color of ['green','red','yellow']){
    const a=new RA.Animator(),snapshot=RA.synth(0,{count:1,mix:'green',output:'on',quota:'87'});snapshot.rows[0].color=color;
    a.set(snapshot);a.showImmediate();let s;for(let i=0;i<180;i++)s=a.frame(1/30,0);
    s={...s,t:8,dt:0,motionDt:0,rows:s.rows.map(v=>({...v,rateText:'26.4',outputLight:1}))};
    const active=[];for(const phase of [.18,.42,.70,.90]){s.rows[0].lightPhase=phase;active.push(take(s));}
    const idle=[];s.rows[0].outputLight=0;for(const phase of [.18,.90]){s.rows[0].lightPhase=phase;idle.push(take(s));}
    rows.push({color,nameChanged:Math.max(...active.slice(1).map(x=>delta(active[0].name,x.name))),rateChanged:Math.max(...active.slice(1).map(x=>delta(active[0].rate,x.rate))),
     positionsStable:active.every(x=>JSON.stringify(x.shape)===JSON.stringify(active[0].shape)),idlePhaseDelta:delta(idle[0].name,idle[1].name)+delta(idle[0].rate,idle[1].rate)});
    snapshot.rows[0].outputting=false;snapshot.rows[0].estimated_visible_tokens_per_second=0;a.set(snapshot);
    for(let i=0;i<240;i++)s=a.frame(1/60,0);
    rows.at(-1).stoppedLight=s.rows[0].outputLight;
    const phase=s.rows[0].lightPhase;a.setReducedMotion(true);for(let i=0;i<60;i++)s=a.frame(1/30,3);
    rows.at(-1).staticPhaseDelta=Math.abs(s.rows[0].lightPhase-phase);
   }
   return{rows,glError:g.getError()};
  });
  result.text=text;
  check('active-model-and-rate-both-have-moving-light',text.rows.every(x=>x.nameChanged>20&&x.rateChanged>20),{colors:text.rows});
  check('text-light-keeps-glyph-positions-stable',text.rows.every(x=>x.positionsStable));
  check('output-light-releases-on-stop-and-freezes-in-static-mode',text.rows.every(x=>x.stoppedLight<.001&&x.staticPhaseDelta===0&&x.idlePhaseDelta===0));
  check('text-light-webgl-is-valid',text.glError===0);
  await page.close();

  const live=await browser.newPage({viewport:{width:1100,height:900}});
  live.on('pageerror',e=>result.errors.push(e.message));
  await live.goto(pathToFileURL(path.join(root,'visual/index.html')).href+'?auto=0&count=1&output=on&zoom=1&dpr=1');
  await live.waitForFunction(()=>window.RA?.debug?.warmed,{},{timeout:30000});
  const warm=await live.evaluate(()=>({stages:RA.debug.warmStages,seconds:RA.debug.warmDuration/1000,hiddenPrep:document.querySelectorAll('[role="status"]').length,
   digits:[...'0123456789'].every(c=>RA.debug.atlas.map.has('rate\u0000'+c)),cjk:[...'额度未获取'].every(c=>RA.debug.atlas.map.has('cjk\u0000'+c))}));
  check('entry-steady-exit-and-common-glyphs-are-prepared',warm.stages===3&&warm.digits&&warm.cjk,{warm});
  const before=await live.evaluate(()=>{const d=RA.debug;window.savedReady=d.renderer.ready;d.renderer.ready=()=>false;return{t:d.anim.t,submits:d.renderer.submitted};});
  await live.waitForTimeout(450);
  const held=await live.evaluate(()=>({t:RA.debug.anim.t,submits:RA.debug.renderer.submitted}));
  check('busy-gpu-holds-animation-progress',held.t===before.t&&held.submits===before.submits,{before,held});
  await live.evaluate(()=>{RA.debug.renderer.ready=savedReady;});await live.waitForTimeout(200);
  const glyphStart=await live.evaluate(()=>{const d=RA.debug;window.savedBuild=d.labels.build;d.labels.build=function(){this.ready=false;return 0;};return d.anim.t;});
  await live.waitForTimeout(120);
  const glyphHeld=await live.evaluate(()=>({t:RA.debug.anim.t,pending:RA.debug.heldFrame}));
  await live.waitForTimeout(300);
  const glyphHeldAgain=await live.evaluate(()=>({t:RA.debug.anim.t,pending:RA.debug.heldFrame}));
  check('pending-glyph-frame-stays-frozen',glyphHeld.pending&&glyphHeldAgain.pending&&glyphHeld.t===glyphHeldAgain.t&&glyphHeld.t-glyphStart<=.034,{glyphStart,glyphHeld,glyphHeldAgain});
  await live.evaluate(()=>{RA.debug.labels.build=savedBuild;});await live.waitForTimeout(200);
  const resumed=await live.evaluate(()=>({held:RA.debug.heldFrame,stats:RA.debug.presentationStats,fallback:!!RA.debug.renderer.fallback,glError:RA.debug.renderer.gl.getError()}));
  check('display-resumes-with-one-bounded-pending-frame',!resumed.held&&resumed.stats.maxPending===1&&!resumed.fallback&&resumed.glError===0,{resumed});
  await live.close();
  check('no-page-errors-or-network-requests',result.errors.length===0&&result.requests.length===0);
  result.result='PASS';
 }catch(e){result.result='FAIL';result.error=String(e.stack||e);process.exitCode=1;}
 finally{
  await browser?.close();const lock=JSON.parse(fs.readFileSync(path.join(root,'layers.lock.json'),'utf8'));
  result.runtimeHashes=Object.fromEntries(Object.keys(lock.runtimeHashes).map(f=>[f,crypto.createHash('sha256').update(fs.readFileSync(path.join(root,'visual',f))).digest('hex')]));
  fs.writeFileSync(path.join(out,'verification.json'),JSON.stringify(result,null,2));
  console.log(JSON.stringify({result:result.result,checks:result.checks,error:result.error},null,2));
 }
})();
