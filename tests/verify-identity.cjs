const fs=require('fs'),path=require('path'),assert=require('assert'),{pathToFileURL}=require('url');
const{chromium}=require('./runtime.cjs');
const result={version:'0.4.9',checks:[],errors:[]},root=path.resolve(__dirname,'../visual');
const pass=(name,data)=>{result.checks.push({name,pass:true,...data});console.log('PASS',name,JSON.stringify(data));};
function lab(rgb){
 const[r,g,b]=rgb.map(v=>v<=.04045?v/12.92:((v+.055)/1.055)**2.4);
 const l=Math.cbrt(.4122214708*r+.5363325363*g+.0514459929*b),m=Math.cbrt(.2119034982*r+.6806995451*g+.1073969566*b),s=Math.cbrt(.0883024619*r+.2817188376*g+.6299787005*b);
 const L=.2104542553*l+.793617785*m-.0040720468*s,a=1.9779984951*l-2.428592205*m+.4505937099*s,c=.0259040371*l+.7827717662*m-.808675766*s;
 return{L,a,b:c,C:Math.hypot(a,c),h:(Math.atan2(c,a)*180/Math.PI+360)%360};
}
const delta=(a,b)=>Math.hypot(a.L-b.L,a.a-b.a,a.b-b.b);
(async()=>{let browser;try{
 browser=await chromium.launch({executablePath:require('./runtime.cjs').chromePath,headless:true});
 const page=await browser.newPage({viewport:{width:1400,height:1400},deviceScaleFactor:1});
 page.on('pageerror',e=>result.errors.push(String(e)));
 await page.goto(pathToFileURL(path.join(root,'index.html')).href+'?pause&auto=0&zoom=2&dpr=1&count=3&mix=mixed&output=off');
 await page.waitForFunction(()=>window.RA?.debug);
 const motion=await page.evaluate(()=>{
  const a=new RA.Animator(),dt=1/120,check=(v,m)=>{if(!v)throw Error(m);};
  a.hideImmediate();a.set(RA.synth(0,{count:3,mix:'green',output:'on',quota:'87'}));a.beginReveal();
  let glassStart=null,early=0;
  for(let i=0;i<840;i++){
   const s=a.frame(dt,0);
   if(a.glassAppearance.mode==='enter'&&glassStart===null)glassStart={time:a.t,foregroundProgress:s.T.enter};
   if(s.T.enter<RA.presentation.glassEnterAt&&a.glassAppearance.mode!=='hidden')early++;
  }
  check(!early&&glassStart.foregroundProgress>=RA.presentation.glassEnterAt,'Glass entered before its configured foreground cue');
  a.beginExit();const start=a.t;let closed=null,glassExit=null,curtain=null,overlap=null;
  for(let i=0;i<600;i++){
   const s=a.frame(dt,0);
   if(closed===null&&s.rows.every(r=>r.rateOp===0))closed=a.t-start;
   if(glassExit===null&&a.glassAppearance.mode==='exit')glassExit=a.t-start;
   if(curtain===null&&s.T.exit>0){curtain=a.t-start;overlap={glassMode:a.glassAppearance.mode,elapsed:a.glassAppearance.elapsed,slabOut:s.T.slabOut};}
  }
  check(glassExit>=closed-RA.presentation.glassExitLead-.02&&curtain>=closed&&overlap.glassMode==='exit'&&overlap.slabOut>0,'Curtain and falling glass did not overlap after row closure');
  check(Math.abs(curtain-glassExit-BG16.appearance.params.anticipation)<.025,'Curtain missed the first-fall cue');
  const samples=[];
  for(const t of [.8,1.05,1.25,1.45,1.7,2.15,2.5]){
   const T={enter:t/RA.STAGE.ENTER_DUR,exit:0};
   samples.push({time:t,header:[0,7,16].map(i=>RA.hudMotion.glyph('header',i,20,T)),
    footer:[0,7,14].map(i=>RA.hudMotion.glyph('footer',i,15,T)),bars:RA.hudMotion.bars(T)});
  }
  const h=samples[2].header,f=samples[3].footer;
  check(h[0].entering>h[1].entering&&h[1].entering>h[2].entering,'Header glyphs lack stagger');
  check(f[0].entering<f[1].entering&&f[1].entering<f[2].entering,'Footer stagger is not reversed');
  check(h.every(g=>g.x>=0)&&f.every(g=>g.x<=0),'Caption entry directions differ from the request');
  const exitSamples=[.1,.2,.3].map(t=>({header:RA.hudMotion.glyph('header',0,20,{enter:1,exit:t/2}),
   footer:RA.hudMotion.glyph('footer',14,15,{enter:1,exit:t/2})}));
  check(exitSamples.every(s=>s.header.x<0&&s.footer.x>0),'Caption exit directions differ from the request');
  check(Math.abs(exitSamples[2].header.x-exitSamples[1].header.x)>Math.abs(exitSamples[1].header.x-exitSamples[0].header.x),'Exit did not accelerate');
  const end=RA.hudMotion.bars({enter:1,exit:1});check(Object.values(end).every(v=>v===0),'Bars survive the completed exit');
  return{glass:{glassStart,early,closed,glassExit,curtain,overlap},glyphs:{samples,exitSamples},barsEnd:end};
 });
 pass('foreground-entry-cue-and-first-fall-curtain-overlap',motion.glass);
 pass('staggered-caption-travel-is-opposed-and-exit-accelerates',motion.glyphs);
 const actual=await page.evaluate(()=>{
  const d=RA.debug;d.advance(14);
  window.readCanvas=()=>{const g=d.renderer.gl,b=new Uint8Array(g.drawingBufferWidth*g.drawingBufferHeight*4);g.readPixels(0,0,g.drawingBufferWidth,g.drawingBufferHeight,g.RGBA,g.UNSIGNED_BYTE,b);return b;};
  window.samplePixel=(b,x,y)=>{const g=d.renderer.gl,px=Math.floor((x+RA.MARGIN)*2),py=g.drawingBufferHeight-1-Math.floor((y+RA.MARGIN)*2);return [...b.slice((py*g.drawingBufferWidth+px)*4,(py*g.drawingBufferWidth+px)*4+4)];};
  const s=d.anim.frame(0,0),L=RA.layers.enabled,was={...L},opts={scale:2,quality:0,glow:false,backdrop:'running',glassFocus:true};
  for(const k of Object.keys(L))L[k]=k==='info';
  d.labels.build(s);d.renderer.render(s,d.labels,d.parts,opts);let b=readCanvas(),blueAlpha=0;
  for(let y=9;y<=14;y+=.5)for(let x=9;x<=14;x+=.5)blueAlpha+=samplePixel(b,x,y)[3];
  for(const k of Object.keys(L))L[k]=k==='frame';
  const barPixels=[];
  for(const t of [.50,.65,.85,1.02,1.20,1.50,2.5]){
   s.T.enter=t/2.5;s.T.exit=0;d.renderer.render(s,d.labels,d.parts,opts);b=readCanvas();
   const bound=(x0,x1,y0,y1)=>{
    const pts=[];for(let x=x0;x<=x1;x+=.25)for(let y=y0;y<=y1;y+=.25)if(samplePixel(b,x,y)[3]>60)pts.push([x,y]);
    return pts.length?{left:Math.min(...pts.map(p=>p[0])),right:Math.max(...pts.map(p=>p[0])),top:Math.min(...pts.map(p=>p[1])),bottom:Math.max(...pts.map(p=>p[1]))}:null;
   };
   barPixels.push({time:t,top:bound(16,28,20,24),bottom:bound(355,371,s.H-11,s.H-8),bars:RA.hudMotion.bars(s.T)});
  }
  Object.assign(L,was);d.redraw();return{blueAlpha,barPixels,error:d.renderer.gl.getError(),palette:RA.statusPalette};
 });
 assert.equal(actual.blueAlpha,0);assert.equal(actual.error,0);
 pass('actual-info-layer-has-no-header-blue-light',{alpha:actual.blueAlpha});
 const short=actual.barPixels.filter(x=>x.top);
 assert(short[0].top.top>=short.at(-1).top.top);assert(short.every(x=>x.top.bottom===short.at(-1).top.bottom));
 const bottom=actual.barPixels.filter(x=>x.bottom);
 assert(bottom[0].bottom.left>=bottom.at(-1).bottom.left);assert(bottom.every(x=>Math.abs(x.bottom.right-bottom.at(-1).bottom.right)<=.5));
 pass('actual-bar-pixels-expand-from-fixed-bottom-and-right-anchors',{samples:actual.barPixels});
 await page.locator('#stage').screenshot({path:path.join(__dirname,'three-status-colours.png')});
 async function inspectColours(p){
  return p.evaluate(()=>{
   const d=RA.debug;d.advance(14);const s=d.anim.frame(0,0),was={...RA.layers.enabled},opts={scale:2,quality:0,glow:true,backdrop:'running',glassFocus:true};
   const read=()=>{const g=d.renderer.gl,b=new Uint8Array(g.drawingBufferWidth*g.drawingBufferHeight*4);g.readPixels(0,0,g.drawingBufferWidth,g.drawingBufferHeight,g.RGBA,g.UNSIGNED_BYTE,b);return b;};
   const pixel=(b,x,y)=>{const g=d.renderer.gl,px=Math.floor((x+RA.MARGIN)*2),py=g.drawingBufferHeight-1-Math.floor((y+RA.MARGIN)*2);return [...b.slice((py*g.drawingBufferWidth+px)*4,(py*g.drawingBufferWidth+px)*4+3)].map(x=>x/255);};
   for(const k of Object.keys(RA.layers.enabled))RA.layers.enabled[k]=k==='info'||k==='glow';
   s.t=8;for(const r of s.rows)r.orbAngle=0;
   d.labels.build(s);d.labels.n=0;d.renderer.render(s,d.labels,d.parts,opts);let b=read();
   const lamps=s.rows.map((r,i)=>({color:r.color,rgb:pixel(b,d.labels.orbA[i*4],d.labels.orbA[i*4+1])}));
   const greenIndex=s.rows.findIndex(r=>r.color==='green'),breath=[];
   for(let i=0;i<=24;i++){
    s.t=8+i*RA.presentation.lampBreathPeriod/24;d.renderer.render(s,d.labels,d.parts,opts);b=read();
    const x=d.labels.orbA[greenIndex*4],y=d.labels.orbA[greenIndex*4+1];
    let value=0;for(let j=0;j<16;j++){const a=j*Math.PI/8;value+=pixel(b,x+9*Math.cos(a),y+9*Math.sin(a))[1];}
    breath.push(value/16);
   }
   s.t=8;
   for(const k of Object.keys(RA.layers.enabled))RA.layers.enabled[k]=k==='background';
   d.renderer.render(s,d.labels,d.parts,opts);b=read();
   const bg=[];for(let y=30;y<s.H-20;y+=9)for(let x=110;x<360;x+=11)bg.push(pixel(b,x,y));
   const background=[0,1,2].map(k=>bg.reduce((sum,c)=>sum+c[k],0)/bg.length);
   Object.assign(RA.layers.enabled,was);d.redraw();return{lamps,background,breath,error:d.renderer.gl.getError()};
  });
 }
 const current=await inspectColours(page);
 const colours=current.lamps.map(c=>{
  const body=lab(actual.palette[c.color].ink),core=lab(c.rgb);
  const glow=lab(actual.palette[c.color].glow);
  assert(core.L>body.L);assert(delta(core,body)<.22);
  assert(glow.L>body.L&&glow.C>body.C);
  return{color:c.color,body,core,glow,currentDistanceToInk:delta(core,body)};
 });
 assert.equal(current.error,0);
 pass('three-rendered-lamps-stay-near-ink-with-brighter-glow',{colours,background:{rgb:current.background,oklch:lab(current.background)}});
 const floor=Math.min(...current.breath),ceiling=Math.max(...current.breath);
 assert(floor>5/255&&ceiling>floor&&floor/ceiling>.55);
 pass('green-light-stays-visible-during-the-entire-breath',{floor,ceiling,minimumToMaximum:floor/ceiling});
 assert.deepEqual(result.errors,[]);result.result='PASS';
}catch(e){result.result='FAIL';result.failure=String(e.stack||e);console.error(e);process.exitCode=1;}
finally{if(browser)await browser.close();fs.writeFileSync(path.join(__dirname,'IDENTITY_RESULT.json'),JSON.stringify(result,null,2));}})();
