'use strict';
const fs=require('fs'),path=require('path'),vm=require('vm'),assert=require('assert'),{pathToFileURL}=require('url'),{performance}=require('perf_hooks');
const {chromium,chromePath}=require('./runtime.cjs'),root=path.resolve(__dirname,'..'),out=path.join(root,'verification/token-light');
const scripts=['engine/core.js','engine/presentation.js','engine/row_presentation.js','engine/ribbon_art.js','engine/stage.js','engine/quota_motion.js','engine/ribbons.js','background/engine/archive.js','background/engine/appearance.js','background/engine/dynamics.js','engine/animator.js'];
function load(old=false){const box=vm.createContext({window:{},console});for(const f of scripts){const backup=path.join(root,'tests/fixtures/token-light/before/visual',f);vm.runInContext(fs.readFileSync(old&&fs.existsSync(backup)?backup:path.join(root,'visual',f),'utf8'),box);}return box.window.RA;}
fs.mkdirSync(out,{recursive:true});
const RA=load(),Old=load(true),checks=[];function check(name,ok,detail={}){checks.push({name,pass:!!ok,...detail});assert(ok,name);}
function row(rate,extra={}){return{thread_id:'a',turn_id:'one',response_id:'sample-'+rate,model:'gpt-6-astra',health_valid:true,activity:'running',outputting:false,speed_valid:true,speed_fresh:false,tokens_per_second:rate,color:'green',...extra};}
const snap=r=>({ready:true,mode:'synthetic',rows:[r],quota:{known:false}});
function settle(a,seconds=3,quality=0){let s;for(let i=0;i<seconds*60;i++)s=a.frame(1/60,quality);return s.rows[0];}
const samples=[];
for(const [rate,tier,light]of[[15,0,.22],[35,1,.34],[70,2,.48],[120,3,.62]]){const a=new RA.Animator();a.showImmediate();a.set(snap(row(rate)));const r=settle(a);check('token-band-'+tier,r.lightTier===tier&&Math.abs(r.outputLight-light)<1e-5,{rate,light:r.outputLight});samples.push(r.outputLight);}
check('normal-token-light-stays-below-writing-effect',samples.every(x=>x>0&&x<1));
const a=new RA.Animator();a.showImmediate();a.set(snap(row(49)));settle(a);let stable=true;
for(let i=0;i<600;i++)if(a.frame(1/60,0).rows[0].lightTier!==1)stable=false;
check('decorative-number-wave-does-not-switch-light-band',stable);
const phase=a.poses.get('a').lightPhase;a.set(snap(row(51)));check('new-band-preserves-phase',a.poses.get('a').lightPhase===phase);let r=settle(a);
check('new-band-settles-through-existing-envelope',r.lightTier===2&&Math.abs(r.outputLight-.48)<1e-5);
a.set(snap(row(51,{activity:'tool_wait'})));r=settle(a);check('tool-wait-releases-added-light',r.outputLight<.00001&&r.lightTier===-1);
a.set(snap(row(51)));settle(a);const frozen=a.poses.get('a').lightPhase;settle(a,2,3);check('static-mode-freezes-flow-phase',a.poses.get('a').lightPhase===frozen);
const before=new Old.Animator(),after=new RA.Animator();before.showImmediate();after.showImmediate();const writing=row(50,{outputting:true,estimated_visible_tokens_per_second:250,output_activity:625});before.set(snap(writing));after.set(snap(writing));let parity=true;
for(let i=0;i<600;i++){const x=before.frame(1/60,0).rows[0],y=after.frame(1/60,0).rows[0];if(x.outputLight!==y.outputLight||x.lightPhase!==y.lightPhase)parity=false;}
check('existing-visible-writing-light-is-unchanged',parity);
(async()=>{let browser;const report={scope:'Synthetic data only; same six-layer renderer',checks};try{
 browser=await chromium.launch({headless:true,executablePath:chromePath});const page=await browser.newPage({viewport:{width:1300,height:1200}});const errors=[];page.on('pageerror',e=>errors.push(e.message));
 await page.goto(pathToFileURL(path.join(root,'visual/index.html')).href+'?pause&auto=0&count=0&zoom=1&dpr=1');await page.waitForFunction(()=>RA?.debug);
 const gpu=await page.evaluate(()=>{
  const d=RA.debug,g=d.renderer.gl,a=new RA.Animator(),r={thread_id:'light',turn_id:'one',response_id:'r',model:'gpt-6-astra',color:'green',activity:'running',health_valid:true,outputting:false,speed_valid:true,tokens_per_second:70};a.showImmediate();a.set({ready:true,mode:'synthetic',rows:[r],quota:{known:true,fresh:true,remaining_percent:87}});let s;for(let i=0;i<240;i++)s=a.frame(1/60,0);s.dt=s.motionDt=0;s.rows[0].rateText='70.0';
  const capture=phase=>{s.rows[0].lightPhase=phase;d.labels.build(s);d.renderer.uploadAtlas(d.atlas);d.renderer.render(s,d.labels,d.parts,{scale:1,quality:0,glow:true,backdrop:'running',glassFocus:true});g.finish();const bytes=new Uint8Array(d.renderer.w*d.renderer.h*4);g.readPixels(0,0,d.renderer.w,d.renderer.h,g.RGBA,g.UNSIGNED_BYTE,bytes);const layout=d.labels.rowLayouts[0],r=s.rows[0];const crop=(left,width)=>{let array=[];for(let y=Math.floor(r.y-r.font);y<Math.ceil(r.y+r.font*.6);y++)for(let x=Math.floor(left);x<Math.ceil(left+width);x++){const i=((d.renderer.h-1-y-RA.MARGIN)*d.renderer.w+x+RA.MARGIN)*4;array.push(bytes[i],bytes[i+1],bytes[i+2]);}return array;};return{name:crop(layout.modelX,layout.name.width+3),number:crop(layout.rateRight-layout.rateWidth,layout.rateWidth+3)};};
  capture(.2);capture(.6);let textures=0;const make=g.createTexture;g.createTexture=function(...args){textures++;return make.apply(this,args);};const first=capture(.2),second=capture(.6);g.createTexture=make;const count=(x,y)=>x.reduce((n,v,i)=>n+(Math.abs(v-y[i])>1),0);return{namePixels:count(first.name,second.name),numberPixels:count(first.number,second.number),newTextures:textures,glError:g.getError(),fallback:!!d.renderer.fallback};
 });
 check('normal-token-name-and-number-both-have-moving-light',gpu.namePixels>10&&gpu.numberPixels>10,gpu);check('light-reuses-existing-gpu-resources',gpu.newTextures===0&&gpu.glError===0&&!gpu.fallback);check('page-has-no-errors',errors.length===0,{errors});report.gpu=gpu;report.result='PASS';
 }catch(e){report.result='FAIL';report.error=String(e.stack||e);process.exitCode=1;}finally{await browser?.close();fs.writeFileSync(path.join(out,'verification.json'),JSON.stringify(report,null,2));console.log(JSON.stringify(report,null,2));}})();
