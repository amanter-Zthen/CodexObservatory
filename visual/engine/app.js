// Preview host: synthetic snapshots, demo script, controls, frame loop.
// The real host replaces only this file (Snapshot feed + window lifecycle).
(function (NS) {
  'use strict';
  const $ = (id) => document.getElementById(id);
  let canvas = $('panel');const statsEl = $('stats'), Q = new URLSearchParams(location.search);
  const desktop=Q.has('desktop'),bridge=window.chrome?.webview,feed=new NS.SnapshotFeed();
  let external=desktop?{rows:[],quota:{known:false},ready:false,mode:'disconnected'}:null,offset=0,closeRequested=false,hideRequested=false,lastHeight=0,lastWidth=0;
  let targetZoom=null,pendingZoom=null,workHeightDIP=null;
  if(desktop){document.body.classList.add('desktop');document.documentElement.style.background='transparent';}
  const opt = { count: 1, mix: 'green', output: 'auto', quota: '87', pending: true, quality: 0, automaticQuality:true, zoom: 2, speed: 1, glow: true, auto: false, backdrop:'auto' };
  let renderer;
  function fallback(e){
    if(renderer?.fallback)return;
    const previous=renderer;
    renderer=new NS.CanvasFallback(canvas,e.message||e);canvas=renderer.canvas;
    if(previous?.gl&&!previous.gl.isContextLost())previous.gl.getExtension('WEBGL_lose_context')?.loseContext();
    statsEl.textContent='简化显示：'+renderer.reason;
  }
  try { renderer = new NS.Renderer(canvas); } catch (e) { fallback(e); }
  canvas.addEventListener('webglcontextlost',e=>{e.preventDefault();fallback('图形上下文已失效');sizeCanvas();});
  const atlas = new NS.Atlas(), labels = new NS.Labels(atlas);
  let anim, parts, scale = 1, simT = 0, lastSet = -1, dirty = true, demoStart = 0, fired = 0, count = opt.count;

  // Boot from empty space -> sessions weave in -> one session leaves -> snake-out exit.
  const DEMO = [[0, 'reveal', 1], [3.4, 'count', 2], [6.0, 'count', 3], [10.5, 'count', 4], [15.0, 'count', 2], [19.5, 'exit'], [23.0, 'restart']];
  function fresh(hidden) {
    anim = new NS.Animator(); parts = new NS.Particles();
    if (hidden) anim.hideImmediate();
    dirty = true;
  }
  function demo() {
    // Elapsed time is re-read every event; a restart ends this frame's pass.
    while (fired < DEMO.length && simT - demoStart >= DEMO[fired][0]) {
      const [, act, n] = DEMO[fired++];
      if (act === 'reveal') { count = n; anim.set(snap(), 0, 500); anim.beginReveal(); }
      if (act === 'count') { count = n; dirty = true; }
      if (act === 'exit') anim.beginExit();
      if (act === 'restart') {
        if(!anim.stage.hidden){fired--;break;}
        demoStart = simT; fired = 0; break;
      }
    }
  }
  const snap = () => external||NS.synth(simT, { count, mix: opt.mix, output: opt.output, quota: opt.quota, pending: opt.pending });

  // ?pause = no rAF loop (inspect with RA.debug.advance), ?dpr=N caps backing scale,
  // ?zoom/count/quality/mix/quota/output/speed preset the controls, ?auto=0 disables the demo.
  const PAUSE = Q.has('pause'), DPR_CAP = Q.has('dpr') ? Math.max(0.5, +Q.get('dpr') || 1) : 3;
  for (const k of ['count', 'zoom', 'quality', 'mix', 'quota', 'output', 'speed','backdrop']) if (Q.has(k) && $(k)) $(k).value = Q.get(k);
  if (Q.get('auto') === '0') $('auto').checked = false;
  let res = 1;
  function sizeCanvas() {
    const z = opt.zoom, dpr = Math.min(DPR_CAP, 3 / z, window.devicePixelRatio || 1) * res;
    const w = NS.PANEL_W + 2 * NS.MARGIN, h = 500 + 2 * NS.MARGIN;
    canvas.style.width = w * z + 'px'; canvas.style.height = h * z + 'px';
    const width=Math.round(w*z*dpr),height=Math.round(h*z*dpr);
    if(canvas.width!==width)canvas.width=width;
    if(canvas.height!==height)canvas.height=height;
    scale = canvas.width / w;
  }
  function bind(id, key, conv) {
    const el = $(id); if (!el) return;
    const read = () => { opt[key] = conv ? conv(el.type === 'checkbox' ? el.checked : el.value) : (el.type === 'checkbox' ? el.checked : el.value); dirty = true; };
    el.addEventListener('change', () => { read(); if (key === 'count') count = opt.count; if (key === 'zoom') sizeCanvas(); if (key === 'auto' && opt.auto) { demoStart = simT; fired = 0; } NS.debug?.redraw(); });
    read();
  }
  bind('count', 'count', Number); bind('mix', 'mix'); bind('output', 'output'); bind('quota', 'quota'); bind('pending', 'pending');
  bind('quality', 'quality', Number); bind('zoom', 'zoom', Number); bind('speed', 'speed', Number); bind('glow', 'glow'); bind('auto', 'auto');
  bind('backdrop','backdrop');bind('glass-focus','glassFocus');bind('glass-state','glassState');
  $('bg').addEventListener('change', (e) => { $('stage').dataset.bg = e.target.value; });
  const manual = () => { opt.auto = false; $('auto').checked = false; count = opt.count; };
  $('reveal').onclick = () => { manual(); anim.beginReveal();NS.debug?.redraw(); };
  $('exit').onclick = () => { manual(); anim.beginExit();NS.debug?.redraw(); };
  $('replay').onclick = () => { manual(); anim.beginReplay();NS.debug?.redraw(); };
  // Glass-only previews leave the foreground timeline available for later
  // user-directed composition. Inputs update targets; each clip finishes.
  $('glass-enter').onclick=()=>{
    manual();anim.showGlass();NS.debug?.redraw();
  };
  $('glass-exit').onclick=()=>{manual();anim.hideGlass();NS.debug?.redraw();};
  $('glass-replay').onclick=()=>{
    manual();anim.replayGlass();
    NS.debug?.redraw();
  };
  window.addEventListener('keydown', (e) => { if (e.code === 'Space' && !e.target.closest('input,select,button,textarea,[contenteditable]')) { e.preventDefault(); manual(); anim.panelWanted ? anim.beginExit() : anim.beginReveal(); } });

  fresh(true); sizeCanvas(); window.addEventListener('resize', sizeCanvas);
  if(!opt.auto){anim.set(snap(),0,500);anim.beginReveal();}
  // First-use shader pipelines and common glyphs are prepared before starting
  // the visible clip clock. Native window messages and the latest data slot
  // remain available; this preparation is not a history or frame replay buffer.
  let warmed=PAUSE||!!renderer.fallback,warmSubmitted=false,warmScene=null,warmIndex=0,warmDigitsReady=false;
  const warmStages=['visible','entry','exit'];
  const warmStarted=performance.now();let warmDuration=0,warmNote=null;
  if(warmed)document.documentElement.classList.remove('preparing');
  if(!warmed){
    canvas.style.visibility='hidden';
    if(!desktop){
      warmNote=document.createElement('div');warmNote.textContent='正在准备显示…';warmNote.setAttribute('role','status');
      warmNote.style.cssText='position:fixed;left:50%;top:45%;transform:translate(-50%,-50%);color:#6d6a63;font:13px "Segoe UI";pointer-events:none';
      document.body.append(warmNote);
    }
  }
  function makeWarmScene(phase){
    const a=new NS.Animator();a.set(NS.synth(0,{count:16,mix:'mixed',output:'on',quota:'87'}));
    if(phase==='entry'){a.hideImmediate();a.beginReveal();}else a.showImmediate();
    let s;for(let i=0;i<(phase==='entry'?16:60);i++)s=a.frame(.05,0);
    if(phase==='exit'){a.beginExit();for(let i=0;i<20;i++)s=a.frame(.05,0);}
    return s;
  }
  function finishWarmup(){
    warmed=true;warmScene=null;warmDuration=performance.now()-warmStarted;
    canvas.style.visibility='';warmNote?.remove();warmNote=null;
    document.documentElement.classList.remove('preparing');
  }
  function prepareDisplay(){
    if(warmed)return true;
    try{
      if(warmSubmitted){
        if(!renderer.ready())return false;
        warmSubmitted=false;warmScene=null;warmIndex++;
      }
      if(!warmDigitsReady){
        atlas.requested.clear();
        for(const key of ['rate','quota'])for(const ch of '0123456789.%/s—')atlas.want(key,ch);
        for(const ch of '额度未获取')atlas.want('cjk',ch);
        warmDigitsReady=atlas.prepare(4);if(!warmDigitsReady)return false;
      }
      if(warmIndex>=warmStages.length){
        const gl=renderer.gl;gl.bindFramebuffer(gl.FRAMEBUFFER,null);gl.viewport(0,0,canvas.width,canvas.height);
        gl.clearColor(0,0,0,0);gl.clear(gl.COLOR_BUFFER_BIT);
        finishWarmup();return true;
      }
      if(!warmSubmitted){
        if(!warmScene)warmScene=makeWarmScene(warmStages[warmIndex]);
        labels.build(warmScene,4);if(!labels.ready)return false;
        const warmParts=new NS.Particles();warmParts.update(1/30,warmScene,labels.emitters,0);
        renderer.uploadAtlas(atlas);renderer.render(warmScene,labels,warmParts,{scale,quality:0,glow:true,backdrop:opt.backdrop,glassFocus:opt.glassFocus});
        warmSubmitted=true;return false;
      }
    }catch(e){fallback(e);sizeCanvas();finishWarmup();return true;}
  }
  let last = performance.now() / 1000, paint=last-1/30, fps = 30, cpu = 0, shown = 0, raf=0, playing=!PAUSE;
  // Preserve full resolution and effects. Slow machines reduce submission
  // cadence; the GPU completion fence independently bounds in-flight work.
  let slow = 0, quick = 0, observedFrames=0,frameRate=30;
  function govern(interval) {
    if(!opt.automaticQuality)return;
    // Allow the first upload/pipeline warm-up to complete. A 30 fps host
    // judges recovery against its 33.3 ms budget, not a 60 fps threshold.
    if(++observedFrames<=2)return;
    const budget=1/frameRate;
    if (interval > budget*1.5) { slow++; quick = 0; } else if (interval < budget*1.1) { quick++; slow = 0; } else { slow = quick = 0; }
    if (slow >= 12 && frameRate>15) { frameRate=15;slow=quick=0; }
    else if (quick >= 180 && frameRate<30) { frameRate=30;slow=quick=0; }
  }
  function tick(ms) {
    raf=0;if(!playing||document.hidden)return;
    const now = ms / 1000, frame=opt.quality===3?.25:1/frameRate;
    if(!prepareDisplay()){last=paint=now;raf=requestAnimationFrame(tick);return;}
    if(pendingFrame){step(0,now,true);last=paint=now;raf=requestAnimationFrame(tick);return;}
    if(now-paint>=frame-.00025){
      const interval=Math.max(0,now-last),real=Math.min(.1,interval);last=Math.max(last,now);
      govern(interval);step(real,now,true);
      paint+=Math.max(1,Math.floor((now-paint+.00025)/frame))*frame;
    }
    raf=requestAnimationFrame(tick);
  }
  function setPlaying(value){playing=Boolean(value);if(raf){cancelAnimationFrame(raf);raf=0;}last=performance.now()/1000;paint=last-1/30;observedFrames=slow=quick=0;$('pause').textContent=playing?'暂停':'播放';if(playing&&!document.hidden)raf=requestAnimationFrame(tick);}
  $('pause').onclick=()=>setPlaying(!playing);
  $('pause').textContent=playing?'暂停':'播放';
  document.addEventListener('visibilitychange',()=>{if(raf){cancelAnimationFrame(raf);raf=0;}last=performance.now()/1000;paint=last-1/30;observedFrames=slow=quick=0;if(playing&&!document.hidden)raf=requestAnimationFrame(tick);});
  // One prepared frame owns all mutable animation state until it is submitted.
  // New inputs replace the latest target while that frame waits for resources.
  let pendingFrame=null;
  const presentationStats={gpuWaits:0,glyphWaits:0,submitted:0,maxPending:0};
  function retryPaused(){if(!playing&&!repaintRaf)repaintRaf=requestAnimationFrame(()=>{repaintRaf=0;redraw();});}
  function step(real, now, draw) {
    const incoming=feed.take(performance.now()/1000);
    if(incoming){external=incoming;dirty=true;bridge?.postMessage({kind:'consumed',sequence:feed.sequence});}
    const t0 = performance.now();
    const paced=draw&&!PAUSE;
    if(paced){
      try{if(!renderer.ready()){presentationStats.gpuWaits++;retryPaused();return;}}
      catch(e){fallback(e);sizeCanvas();}
    }
    let s=paced?pendingFrame:null;
    if(!s){
      const dt=(paced?Math.min(real,opt.quality===3?.1:1/frameRate):real)*opt.speed;
      simT+=dt;
      if(opt.auto)demo();else count=opt.count;
      const maxHeight=desktop&&workHeightDIP!==null?Math.max(112,Math.min(500,workHeightDIP/opt.zoom-2*NS.MARGIN-2)):500;
      if(dirty||simT-lastSet>.2){anim.set(snap(),offset,maxHeight);offset=anim.offset;lastSet=simT;dirty=false;}
      anim.setReducedMotion(opt.quality===3);
      const rows=anim.snapshot.rows||[],shape=opt.glassState!=='auto'?opt.glassState:
        !rows.length?'idle':rows.some(r=>r.color==='red')?'red':rows.some(r=>r.color==='yellow')?'yellow':'green';
      anim.glassDynamics.requestState(shape);
      if(desktop&&pendingZoom!==null&&!closeRequested&&!hideRequested&&!anim.stage.entering&&!anim.stage.exiting&&!anim.stage.hidden)anim.beginExit();
      s=anim.frame(dt,opt.quality);
      if(desktop&&pendingZoom!==null&&s.stage.hidden){
        opt.zoom=pendingZoom;pendingZoom=null;sizeCanvas();
        bridge?.postMessage({kind:'view-state',zoom:opt.zoom,targetZoom:targetZoom??opt.zoom});
        if(!closeRequested&&!hideRequested){anim.beginReveal();s=anim.frame(0,opt.quality);}
      }
      if(paced){pendingFrame=s;presentationStats.maxPending=1;}
    }
    if(s.stage.hidden){labels.n=0;labels.emitters.length=0;labels.ready=true;}
    else labels.build(s,playing?4:Infinity);
    if(draw&&!labels.ready){presentationStats.glyphWaits++;retryPaused();return;}
    parts.update(s.motionDt,s,labels.ready?labels.emitters:[],opt.quality);
    let painted=false;
    if(draw){
      try{
        renderer.uploadAtlas(atlas);renderer.render(s,labels,parts,{scale,quality:opt.quality,glow:opt.glow,backdrop:opt.backdrop,glassFocus:opt.glassFocus});
      }catch(e){fallback(e);sizeCanvas();renderer.render(s,labels,parts,{scale});}
      painted=true;pendingFrame=null;presentationStats.submitted++;
    }
    $('stage').style.height=Math.ceil((s.H+2*NS.MARGIN)*opt.zoom+(desktop?0:52))+'px';
    if(desktop){
      const height=Math.ceil((s.H+2*NS.MARGIN)*opt.zoom),width=Math.ceil((s.W+2*NS.MARGIN)*opt.zoom);
      if(height!==lastHeight||width!==lastWidth){lastHeight=height;lastWidth=width;bridge?.postMessage({kind:'size',width,height,margin:NS.MARGIN});}
      if(painted&&closeRequested&&s.stage.hidden){closeRequested=false;bridge?.postMessage({kind:'closed'});}
      else if(painted&&hideRequested&&s.stage.hidden){hideRequested=false;bridge?.postMessage({kind:'hidden-complete'});setPlaying(false);}
    }
    cpu += (performance.now() - t0 - cpu) * 0.05;if(real>0)fps += (1 / Math.max(1e-3, real) - fps) * 0.05;
    if (now - shown > 0.25) {
      shown = now;
      const st = s.stage.hidden ? '透明' : s.stage.entering ? '启动' : s.stage.exiting ? '消散' : '运行';
      const glass=anim.glassDynamics.view();
      statsEl.textContent = `${st} · ${s.status.text.replace(/[ .]+$/, '')} | 玻璃 ${glass.phase} · ${glass.state}${glass.pending?' → '+glass.pending:''} | 会话 ${s.rows.length} | 曲线 ${s.ribbons.count || 0}/${NS.RIBBON_MAX} | CPU ${cpu.toFixed(2)} ms | ${fps.toFixed(0)} fps | ${renderer.fallback?'简化显示':renderer.fmt.hdr ? 'RGBA16F' : 'RGBA8'} · ${canvas.width}×${canvas.height}`;
    }
  }
  if (!PAUSE) raf=requestAnimationFrame(tick);
  // Inspection hook: simulate `seconds` at fixed dt, draw only the last frame.
  const advance = (seconds, dt) => {
    if(!warmed)finishWarmup(); // Explicit inspection controls own their clock.
    pendingFrame=null;
    dt = dt || 1 / 60; const n = Math.max(1, Math.round(seconds / dt));
    for (let k = 0; k < n; k++) step(dt, (last += dt), k === n - 1);
  };
  let repaintRaf=0;
  const redraw=()=>{if(!playing)step(0,performance.now()/1000,true);};
  NS.debug = { get anim() { return anim; }, opt, labels, parts, get renderer(){return renderer;}, atlas, feed, advance,setPlaying,redraw,presentationStats,get heldFrame(){return !!pendingFrame;},get warmStages(){return warmIndex;},get warmed(){return warmed;},get warmDuration(){return warmDuration;},get playing(){return playing;}, get simT() { return simT; },get desktopState(){return{targetZoom,pendingZoom,closeRequested,hideRequested};} };
  canvas.addEventListener('wheel',e=>{e.preventDefault();offset+=Math.sign(e.deltaY);dirty=true;redraw();},{passive:false});
  if(bridge&&desktop){
    bridge.addEventListener('message',event=>{
      const m=event.data;if(!m||typeof m!=='object')return;
      if(m.kind==='snapshot'){if(!feed.receive(m,performance.now()/1000))bridge.postMessage({kind:'consumed',sequence:m.sequence});}
      if(m.kind==='view'){
        if(Number.isFinite(m.workHeightDIP)&&m.workHeightDIP>=100&&m.workHeightDIP<=16000)workHeightDIP=m.workHeightDIP;
        if(Number.isFinite(m.zoom)&&m.zoom>=.75&&m.zoom<=2){
          targetZoom=m.zoom;
          if(!warmed||anim.stage.hidden){opt.zoom=m.zoom;pendingZoom=null;sizeCanvas();}
          else pendingZoom=pendingZoom!==null||Math.abs(m.zoom-opt.zoom)>.001?m.zoom:null;
          bridge.postMessage({kind:'view-state',zoom:opt.zoom,targetZoom});
        }
        if(Number.isInteger(m.quality)&&m.quality>=0&&m.quality<=3)opt.quality=m.quality;
        if(typeof m.automatic==='boolean'){opt.automaticQuality=m.automatic;if(!m.automatic)frameRate=30;}
        dirty=true;
      }
      if(m.kind==='close'){closeRequested=true;hideRequested=false;anim.beginExit();setPlaying(true);}
      if(m.kind==='hide'){hideRequested=true;closeRequested=false;anim.beginExit();setPlaying(true);}
      if(m.kind==='show'){closeRequested=hideRequested=false;anim.beginReveal();setPlaying(true);}
      if(m.kind==='replay'){closeRequested=hideRequested=false;anim.beginReplay();setPlaying(true);}
      if(m.kind==='wheel'&&(m.delta===1||m.delta===-1)){offset+=m.delta;dirty=true;redraw();}
      if(m.kind==='hidden')setPlaying(false);
    });
    document.addEventListener('contextmenu',e=>{e.preventDefault();bridge.postMessage({kind:'menu'});});
    document.addEventListener('mousedown',e=>{
      if(e.button!==0||e.target!==canvas)return;
      const r=canvas.getBoundingClientRect(),v=$('stage').getBoundingClientRect(),margin=NS.MARGIN*opt.zoom;
      // The canvas reserves 500 DIP of rows; the visible window follows current H.
      if(e.clientX>=Math.max(r.left,v.left)+margin&&e.clientX<=Math.min(r.right,v.right)-margin&&
         e.clientY>=r.top+margin&&e.clientY<=Math.min(r.bottom,v.bottom)-margin)
        bridge.postMessage({kind:'drag'});
    });
    bridge.postMessage({kind:'ready'});
  }
})(window.RA);
