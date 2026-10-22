'use strict';
// Offline, bounded rendering of the actual six-layer scene. All data is synthetic.
// Usage: node tools/render-public-demo.cjs --ffmpeg <exe> --music <file> [--out <dir>] [--probe]
const fs=require('fs'),path=require('path'),{spawn}=require('child_process'),{once}=require('events'),{pathToFileURL}=require('url');
const {chromium,chromePath}=require('../tests/runtime.cjs');
const args=process.argv.slice(2),arg=(k,d)=>args.includes(k)?args[args.indexOf(k)+1]:d;
const root=path.resolve(__dirname,'..'),out=path.resolve(arg('--out',path.join(root,'media')));
const ffmpeg=arg('--ffmpeg',process.env.FFMPEG||'ffmpeg'),music=arg('--music',null),probe=args.includes('--probe');
const FPS=60,DURATION=37,WIDTH=3840,HEIGHT=2160,SCALE=5;
fs.mkdirSync(out,{recursive:true});
(async()=>{
 const browser=await chromium.launch({headless:true,executablePath:chromePath});
 let encoder,encoderResult;
 try{
  const page=await browser.newPage({viewport:{width:1400,height:1000}});
  await page.addInitScript(()=>{let seed=0x49ad12;Math.random=()=>{seed=(Math.imul(seed,1664525)+1013904223)>>>0;return seed/4294967296;};});
  await page.goto(pathToFileURL(path.join(root,'visual/index.html')).href+'?pause&auto=0&count=0&zoom=1&dpr=1');
  await page.waitForFunction(()=>window.RA?.debug);
  await page.evaluate(({W,H,scale,fps})=>{
   const d=RA.debug;d.setPlaying(false);d.configureCapture({scale,maxHeight:180});
   const cv=document.getElementById('panel'),ren=d.renderer,anim=d.anim;
   let sequence=0;
   const send=snapshot=>{snapshot.at=new Date().toISOString();if(!d.feed.receive({kind:'snapshot',generation:1,sequence:++sequence,snapshot},performance.now()/1000))throw Error('Assembled preview rejected snapshot');};
   const output=document.createElement('canvas');output.width=W;output.height=H;const ctx=output.getContext('2d');
   send(RA.synth(0,{count:0,quota:87,pending:false}));d.advance(.001,.001,false);anim.showImmediate();
   // Prepare the real idle state before capture, using its ordinary transition.
   // No wall-clock skip or material override is used in the exported sequence.
   d.advance(6,1/60,false);
   if(anim.glassDynamics.view().state!=='idle'||!anim.glassDynamics.settled())throw Error('Idle glass preparation did not settle');
   anim.hideImmediate();
   const smooth=v=>{v=Math.max(0,Math.min(1,v));return v*v*v*(v*(v*6-15)+10);};
   let entered=false,exited=false,lastFeed=-1;
   const timeline=t=>t<6?{n:0,mix:'green',zh:'静待灵感，光先抵达',en:'Light arrives before the first idea.'}:
    t<12?{n:1,mix:'green',zh:'模型启动 · 镜头拉远 · 信息展开',en:'A model starts. The camera opens. The signal unfolds.'}:
    t<18?{n:2,mix:'mixed',zh:'待核验的路由，让玻璃档案随之切换',en:'A pending route moves the glass archive into WAITING.'}:
    t<25?{n:4,mix:'mixed',zh:'发现标签异常，红色档案进入视野',en:'A label mismatch brings the red ANOMALY archive into view.'}:
    t<28?{n:4,mix:'green',zh:'新回合恢复一致，信息有序收拢',en:'A new turn matches. Information folds back into place.'}:
    t<32?{n:0,mix:'green',zh:'回到安静的待机镜头',en:'The scene returns to a quiet standby.'}:
    {n:0,mix:'green',zh:'光影退场，留一份清晰',en:'The light dissolves. Clarity stays.'};
   window.publicFrame=(i,png=false,raster=true)=>{
    const t=i/fps,state=timeline(t);
    if(t>=.5&&!entered){anim.beginReveal();entered=true;}
    if(t>=32&&!exited){anim.beginExit();exited=true;}
    if(i%12===0||lastFeed<0){
     const snap=RA.synth(t,{count:state.n,mix:state.mix,output:t<8||t>=25?'off':'on',quota:87,pending:false});
     for(const r of snap.rows)if(r.color==='yellow'){r.outputting=false;r.estimated_visible_tokens_per_second=0;}
     if(t>=25)for(const r of snap.rows)r.turn_id='new-turn-'+r.thread_id;
     send(snap);lastFeed=i;
    }
    // Advance the assembled preview: SnapshotFeed -> app.step -> Animator -> all six layers.
    d.advance(1/fps,1/fps,raster);const s=anim.frame(0,0);
    const glass=anim.glassDynamics.view(),evidence={time:t,hidden:s.stage.hidden,rows:s.rows.length,height:s.H,glassState:glass.state,glassDesired:glass.desiredState,glassPhase:glass.phase,glassTime:glass.geometryTime,models:(anim.snapshot.rows||[]).map(r=>r.model),glassDraws:ren.glass.draws};
    if(!raster)return evidence;
    ren.gl.finish();
    evidence.glassDraws=ren.glass.draws;
    ctx.fillStyle='#f6f3ec';ctx.fillRect(0,0,W,H);
    // The full 128-DIP transparent render allowance remains inside the video.
    const visibleH=Math.ceil((s.H+2*RA.MARGIN)*scale);
    ctx.drawImage(cv,0,0,cv.width,visibleH,(W-cv.width)/2,(H-visibleH)/2+20,cv.width,visibleH);
    const a=smooth(t/.8)*(1-smooth((t-35.6)/1.4));ctx.globalAlpha=a;
    ctx.fillStyle='#76786b';ctx.textAlign='center';ctx.font='500 28px "Segoe UI","Microsoft YaHei UI",sans-serif';
    ctx.fillText('CODEX OBSERVATORY   /   0.4.9',W/2,176);
    ctx.fillStyle='#3a493d';ctx.font='600 66px "Segoe UI","Microsoft YaHei UI",sans-serif';ctx.fillText('把路由看清，把工作点亮',W/2,282);
    ctx.fillStyle='#72776a';ctx.font='400 34px "Segoe UI",sans-serif';ctx.fillText('See your routes. Light up your workflow.',W/2,352);
    const cuts=[0,6,12,18,25,28,32,37];let start=cuts.filter(x=>x<=t).pop(),end=cuts.find(x=>x>t)||37;
    ctx.globalAlpha=a*smooth((t-start)/.32)*(1-smooth((t-end+.3)/.3));
    ctx.fillStyle='#455247';ctx.font='500 38px "Segoe UI","Microsoft YaHei UI",sans-serif';ctx.fillText(state.zh,W/2,1858);
    ctx.fillStyle='#777c70';ctx.font='400 27px "Segoe UI",sans-serif';ctx.fillText(state.en,W/2,1913);
    ctx.globalAlpha=a;ctx.fillStyle='#93978c';ctx.font='400 23px "Segoe UI","Microsoft YaHei UI",sans-serif';
    ctx.fillText('合成数据演示 / SYNTHETIC DATA DEMO     ·     6 个可编辑图层 / 6 EDITABLE LAYERS',W/2,2040);ctx.globalAlpha=1;
    return {...evidence,image:output.toDataURL(png?'image/png':'image/jpeg',.98).split(',')[1]};
   };
   window.publicSolo=(key)=>{
    RA.layers.solo(key);d.redraw();const s=anim.frame(0,0);ren.gl.finish();
    const c=document.createElement('canvas');c.width=cv.width;c.height=Math.ceil((s.H+2*RA.MARGIN)*scale);const x=c.getContext('2d');x.fillStyle='#f6f3ec';x.fillRect(0,0,c.width,c.height);x.drawImage(cv,0,0);RA.layers.all();return c.toDataURL('image/png').split(',')[1];
   };
  },{W:WIDTH,H:HEIGHT,scale:SCALE,fps:FPS});
  const started=Date.now(),trace=[];let last;
  if(!probe){
   const cmd=['-hide_banner','-loglevel','warning','-y','-f','image2pipe','-vcodec','mjpeg','-framerate',String(FPS),'-i','pipe:0'];
   if(music)cmd.push('-i',music);
   cmd.push('-map','0:v:0');if(music)cmd.push('-map','1:a:0','-c:a','aac','-b:a','256k','-af','afade=t=in:st=0:d=0.5,afade=t=out:st=34.5:d=2.5');
   cmd.push('-t',String(DURATION),'-c:v','libx264','-preset','fast','-crf','18','-pix_fmt','yuv420p','-threads','4','-map_metadata','-1','-movflags','+faststart',path.join(out,'CodexObservatory-0.4.9-4K60.mp4'));
   encoder=spawn(ffmpeg,cmd,{stdio:['pipe','ignore','pipe'],windowsHide:true});let errors='';encoder.stderr.on('data',x=>{errors=(errors+x).slice(-8000);});
   encoderResult=new Promise((resolve,reject)=>{encoder.on('error',reject);encoder.on('close',c=>c===0?resolve():reject(Error('FFmpeg '+c+': '+errors)));});
   encoderResult.catch(()=>{});encoder.stdin.on('error',()=>{});
  }
  const total=FPS*DURATION;
  for(let i=0;i<total;i++){
   // Probe skips raster work between selected frames, while preserving every simulation tick.
   last=await page.evaluate(({i,raster})=>publicFrame(i,false,raster),{i,raster:!probe||i%60===0||i===total-1});
   if(encoder&&!encoder.stdin.write(Buffer.from(last.image,'base64')))await once(encoder.stdin,'drain');
   if(i%30===0||i===total-1){const {image,...state}=last;trace.push(state);}
   if(last.image&&[60,240,600,900,960,1260,1380,1620,1800,1980,2100,2160].includes(i)){
    fs.writeFileSync(path.join(out,'frame-'+String(i).padStart(4,'0')+'.jpg'),Buffer.from(last.image,'base64'));
   }
   if(i===1380){
    fs.writeFileSync(path.join(out,'poster.jpg'),Buffer.from(last.image,'base64'));
    for(const key of ['background','curves','particles','frame','info','glow'])fs.writeFileSync(path.join(out,'layer-'+key+'.png'),Buffer.from(await page.evaluate(key=>publicSolo(key),key),'base64'));
   }
   if(i%120===0)console.log(JSON.stringify({frame:i,total,seconds:+((Date.now()-started)/1000).toFixed(1)}));
  }
  if(encoder){encoder.stdin.end();await encoderResult;}
  const glassStates=[...new Set(trace.map(s=>s.glassState))],models=[...new Set(trace.flatMap(s=>s.models))];
  if(!last.hidden||!['idle','green','yellow','red'].every(s=>glassStates.includes(s))||models.some(s=>s.includes('mini'))||!models.includes('gpt-6-astra'))throw Error('Demo state coverage is incomplete');
  const report={version:'0.4.9',resolution:[WIDTH,HEIGHT],fps:FPS,duration:DURATION,frames:total,source:'assembled preview page: SnapshotFeed -> app.step -> Animator -> six-layer WebGL renderer',synthetic:true,maxFramesInFlight:1,encoderThreads:4,glassFrameInterval:0,idlePreparationSeconds:6,glassStates,models,endHidden:last.hidden,elapsedSeconds:(Date.now()-started)/1000,musicIncluded:!!music};
  fs.writeFileSync(path.join(out,'demo-state-trace.json'),JSON.stringify(trace,null,2)+'\n');
  fs.writeFileSync(path.join(out,probe?'probe.json':'demo-render.json'),JSON.stringify(report,null,2)+'\n');console.log(JSON.stringify(report));
 }finally{if(encoder&&!encoder.killed&&encoder.exitCode===null)encoder.kill();await browser.close();}
})().catch(e=>{console.error(e);process.exitCode=1;});
