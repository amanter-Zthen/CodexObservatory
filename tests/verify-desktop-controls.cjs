'use strict';
const fs=require('fs'),path=require('path'),assert=require('assert'),{pathToFileURL}=require('url');
const {chromium,chromePath}=require('./runtime.cjs');
const root=path.resolve(__dirname,'..'),checks=[];
(async()=>{
 const browser=await chromium.launch({headless:true,executablePath:chromePath});
 try {
  const page=await browser.newPage({viewport:{width:1000,height:900}});
  const errors=[];page.on('pageerror',e=>errors.push(e.message));
  await page.route('**/*',route=>/^(file:|data:)/.test(route.request().url())?route.continue():route.abort());
  await page.addInitScript(()=>{
   window.desktopMessages=[];window.desktopHandlers=[];
   window.chrome=window.chrome||{};
   window.chrome.webview={postMessage:m=>window.desktopMessages.push(m),addEventListener:(type,fn)=>{if(type==='message')window.desktopHandlers.push(fn);}};
   window.nativeMessage=m=>window.desktopHandlers.forEach(fn=>fn({data:m}));
  });
  await page.goto(pathToFileURL(path.join(root,'visual/index.html')).href+'?desktop=1&pause&zoom=1');
  await page.waitForFunction(()=>window.RA?.debug);
  const evidence=await page.evaluate(()=>{
   const d=RA.debug,send=m=>{nativeMessage(m);d.setPlaying(false);},advance=s=>d.advance(s);
   let sequence=0;
   const snap=(count=3,prefix='model')=>({
    mode:'observed',ready:true,revision:++sequence,at:new Date().toISOString(),
    quota:{known:true,fresh:true,remaining_percent:63},
    rows:Array.from({length:count},(_,i)=>({thread_id:`aaaaaaaa-aaaa-aaaa-aaaa-${String(i).padStart(12,'0')}`,
     turn_id:'current',model:`${prefix}-${i}`,color:'green',activity:'running',health_valid:true,outputting:true,
     estimated_visible_tokens_per_second:23,output_activity:10,speed_valid:false,speed_fresh:false}))
   });
   const offer=(n=3,p='model')=>{const s=snap(n,p);send({kind:'snapshot',generation:1,sequence,snapshot:s});};
   const results=[];
   const record=(name,pass,detail={})=>results.push({name,pass,...detail});
   offer();send({kind:'show'});advance(8);
   record('desktop-page-and-stage-are-transparent',getComputedStyle(document.body).backgroundColor==='rgba(0, 0, 0, 0)'&&getComputedStyle(document.getElementById('stage')).backgroundColor==='rgba(0, 0, 0, 0)');
   send({kind:'view',zoom:1.5,quality:0,automatic:true});advance(.2);
   record('zoom-waits-for-complete-exit',d.opt.zoom===1&&d.desktopState.pendingZoom===1.5);
   send({kind:'view',zoom:1,quality:0,automatic:true});advance(10);
   record('cancelled-zoom-restores-visible-state',d.opt.zoom===1&&!d.anim.stage.hidden&&d.anim.panelWanted);
   send({kind:'view',zoom:1.25,quality:0,automatic:true});
   send({kind:'view',zoom:2,quality:0,automatic:true});
   send({kind:'view',zoom:1.5,quality:0,automatic:true});advance(10);
   record('zoom-coalesces-latest-target',d.opt.zoom===1.5&&d.desktopState.pendingZoom===null&&!d.anim.stage.hidden);
   const beforeHide=desktopMessages.filter(x=>x.kind==='hidden-complete').length;
   send({kind:'hide'});advance(9);
   record('soft-hide-completes-before-hidden-notification',d.anim.stage.hidden&&!d.playing&&desktopMessages.filter(x=>x.kind==='hidden-complete').length===beforeHide+1);
   offer(4,'fresh');send({kind:'show'});advance(8);
   record('show-after-hide-uses-current-data',!d.anim.stage.hidden&&d.feed.current.rows[0].model==='fresh-0'&&d.anim.snapshot.rows.length===4);
   send({kind:'replay'});advance(.3);
   const replayStarted=d.anim.panelReplay||d.anim.lifecycle!=='visible';
   advance(12);
   record('replay-finishes-with-current-models',replayStarted&&!d.anim.stage.hidden&&d.anim.snapshot.rows.length===4);
   offer(30);advance(8);send({kind:'wheel',delta:1});advance(.1);
   const afterWheel=d.anim.offset;
   send({kind:'wheel',delta:999});advance(.1);
   record('native-wheel-is-bounded-and-validates-direction',afterWheel===1&&d.anim.offset===1,{offset:afterWheel});
   send({kind:'view',zoom:1.5,quality:3,automatic:false});advance(.2);
   record('manual-quality-disables-automatic-policy',d.opt.quality===3&&d.opt.automaticQuality===false);
   send({kind:'view',zoom:1.5,quality:0,automatic:true});advance(.2);
   record('automatic-policy-is-restored',d.opt.quality===0&&d.opt.automaticQuality===true);
   const sizes=desktopMessages.filter(x=>x.kind==='size');
   const last=sizes.at(-1);
   record('native-size-excludes-preview-footer-padding',Math.abs(parseFloat(document.getElementById('stage').style.height)-last.height)<1,{size:last});
   record('native-stage-clips-only-at-full-window-without-scrollbars',getComputedStyle(document.getElementById('stage')).overflow==='hidden');
   record('native-size-and-drag-margin-travel-together',sizes.every(s=>s.margin===128)&&last.width===Math.ceil((RA.PANEL_W+2*last.margin)*d.opt.zoom),{size:last});
   offer(0);advance(10);
   const box=document.getElementById('stage').getBoundingClientRect(),panel=document.getElementById('panel');
   const oldDrags=desktopMessages.filter(x=>x.kind==='drag').length;
   const click=(x,y)=>panel.dispatchEvent(new MouseEvent('mousedown',{bubbles:true,button:0,clientX:x,clientY:y}));
   click(box.left+box.width/2,box.bottom-40*d.opt.zoom);
   const afterMargin=desktopMessages.filter(x=>x.kind==='drag').length;
   click(box.left+box.width/2,box.top+(RA.MARGIN+30)*d.opt.zoom);
   record('empty-window-transparent-bottom-does-not-start-drag',afterMargin===oldDrags&&desktopMessages.filter(x=>x.kind==='drag').length===oldDrags+1);
   offer(30);send({kind:'view',zoom:1.75,quality:0,automatic:true,workHeightDIP:920});advance(12);
   const fitted=desktopMessages.filter(x=>x.kind==='size').at(-1),visible=d.anim.layout.vis;
   send({kind:'wheel',delta:1});advance(2);
   record('maximum-zoom-keeps-full-glow-inside-work-area-and-rows-scroll',fitted.height<=920&&visible<16&&visible>0&&d.anim.offset===1,{size:fitted,visibleRows:visible,total:d.anim.snapshot.rows.length});
   const glError=d.renderer.gl?.getError()??0;
   record('desktop-controls-preserve-renderer',glError===0,{glError});
   return results;
  });
  checks.push(...evidence);
  const boot=await browser.newPage({viewport:{width:800,height:700}});
  boot.on('pageerror',e=>errors.push(e.message));
  await boot.route('**/*',route=>/^(file:|data:)/.test(route.request().url())?route.continue():route.abort());
  await boot.addInitScript(()=>{
   window.bootFrames=[];
   function frame(){
    const d=window.RA?.debug,b=document.body,c=document.getElementById('panel');
    if(b&&c&&window.bootFrames.length<600){
     const cs=getComputedStyle(c);
     window.bootFrames.push({warmed:d?.warmed??false,t:d?.simT??0,canvasVisible:cs.visibility!=='hidden',
      background:getComputedStyle(b).backgroundColor,
      controlsVisible:[...document.querySelectorAll('header,.bar,.layers,.layer-actions,footer,#stats,[role=status]')]
       .some(e=>getComputedStyle(e).display!=='none'&&getComputedStyle(e).visibility!=='hidden')});
    }
    if(window.bootFrames.length<600)requestAnimationFrame(frame);
   }
   requestAnimationFrame(frame);
  });
  await boot.goto(pathToFileURL(path.join(root,'visual/index.html')).href+'?desktop=1&zoom=1');
  await boot.waitForFunction(()=>window.RA?.debug?.warmed&&window.bootFrames?.some(f=>f.warmed&&f.canvasVisible),{},{timeout:30000});
  const startup=await boot.evaluate(()=>({frames:bootFrames,warmDuration:RA.debug.warmDuration}));
  checks.push({name:'normal-boot-keeps-preparation-transparent',pass:startup.frames.length>0&&startup.frames.every(f=>!f.controlsVisible&&f.background==='rgba(0, 0, 0, 0)'&&(!f.warmed?!f.canvasVisible&&f.t===0:true)),
   frames:startup.frames.length,preparingFrames:startup.frames.filter(f=>!f.warmed).length,warmDurationMs:startup.warmDuration});
  await boot.close();
  checks.push({name:'no-page-errors',pass:errors.length===0,errors});
  const report={at:new Date().toISOString(),result:checks.every(c=>c.pass)?'PASS':'FAIL',scope:'Isolated page/native bridge; synthetic current snapshots; no production APIs',checks};
  const out=path.join(root,'verification/shell');fs.mkdirSync(out,{recursive:true});
  fs.writeFileSync(path.join(out,'desktop-controls.json'),JSON.stringify(report,null,2));
  console.log(JSON.stringify(report,null,2));assert(checks.every(c=>c.pass),'Desktop controls failed');
 } finally {await browser.close();}
})().catch(e=>{console.error(e);process.exitCode=1;});
