'use strict';
const fs=require('fs'),path=require('path'),vm=require('vm'),assert=require('assert'),{performance}=require('perf_hooks');
const root=path.resolve(__dirname,'..'),visual=path.join(root,'visual'),out=path.join(root,'verification/hybrid-speed');
const files=['engine/core.js','engine/presentation.js','engine/row_presentation.js','engine/ribbon_art.js','engine/stage.js','engine/quota_motion.js','engine/ribbons.js','background/engine/archive.js','background/engine/appearance.js','background/engine/dynamics.js','engine/animator.js'];
function load(before=false){const c=vm.createContext({window:{},console,performance});for(const f of files){const old=path.join(root,'tests/fixtures/hybrid-speed/before/visual',f);vm.runInContext(fs.readFileSync(before&&fs.existsSync(old)?old:path.join(visual,f),'utf8'),c,{filename:f});}return c.window.RA;}
fs.mkdirSync(out,{recursive:true});
const RA=load(),Old=load(true),checks=[];
function check(name,condition,detail){checks.push({name,pass:!!condition,...detail});assert(condition,name);}
const base={thread_id:'a',turn_id:'one',response_id:'r1',model:'gpt-6-astra',activity:'running',health_valid:true,color:'green',outputting:true,speed_valid:false,speed_fresh:false,estimated_visible_tokens_per_second:250,output_activity:625,tokens_per_second:0};
const snapshot=row=>({mode:'synthetic',ready:true,at:new Date().toISOString(),rows:[row],quota:{known:true,fresh:true,remaining_percent:87}});
const a=new RA.Animator();a.showImmediate();
function set(row){a.set(snapshot(row));}
function step(seconds){let s;for(let i=0;i<Math.round(seconds*60);i++)s=a.frame(1/60,0);return s;}
set(base);let s=step(3);
check('first-visible-writing-uses-original-large-value',s.rows[0].rateSource==='text'&&Math.abs(s.rows[0].rate-250)<1e-6&&s.rows[0].rateOp===1);
const measured={...base,outputting:false,activity:'tool_wait',speed_valid:true,speed_fresh:false,tokens_per_second:20,estimated_visible_tokens_per_second:0,output_activity:0};
const previous=s.rows[0].rate;set(measured);
check('source-switch-keeps-tween-continuous',Math.abs(a.poses.get('a').rate.value(a.t)-previous)<1e-6);
s=step(1);
check('tool-gap-shows-token-rate-without-collapsing',s.rows[0].rateSource==='token'&&Math.abs(s.rows[0].rate-20)<1e-6&&s.rows[0].rateOp===1);
const next={...measured,response_id:'r2',activity:'running',speed_valid:false,tokens_per_second:0};set(next);
const values=new Set();for(let i=0;i<240;i++){s=a.frame(1/60,0);values.add(s.rows[0].rateText);}
check('no-writing-running-state-has-token-motion',values.size>12&&s.rows[0].tokenAnchor===20,{distinctDigits:values.size});
set({...next,outputting:true,estimated_visible_tokens_per_second:480,output_activity:1200});s=step(1);
check('visible-text-covers-token-baseline',s.rows[0].rateSource==='text'&&Math.abs(s.rows[0].rate-480)<1e-6&&s.rows[0].tokenAnchor===20);
set({...measured,response_id:'r2',tokens_per_second:35});s=step(1);
check('completed-response-updates-anchor-even-after-fresh-window',s.rows[0].tokenAnchor===35&&Math.abs(s.rows[0].rate-35)<1e-6);
set({...next,turn_id:'two'});s=step(2);
check('new-turn-does-not-reuse-previous-token-rate',s.rows[0].tokenAnchor===0&&s.rows[0].rateSource==='none'&&s.rows[0].rateOp===0);
set({...measured,turn_id:'two',response_id:'r3',tokens_per_second:42});step(1);a.set({...snapshot(measured),ready:false});s=step(2);
check('offline-clears-token-memory',s.rows[0].tokenAnchor===0&&s.rows[0].rateOp===0);
set({...measured,activity:'completed',speed_fresh:true,response_id:'r4',tokens_per_second:50});step(2);s=step(6);
check('finished-turn-keeps-only-existing-finite-tail',s.rows[0].rateOp===0);
const old=new Old.Animator(),modern=new RA.Animator();old.showImmediate();modern.showImmediate();let maxError=0;
for(let i=0;i<900;i++){
 if(i%15===0){const r={...base,estimated_visible_tokens_per_second:60+300*(.5+.5*Math.sin(i/90))};old.set(snapshot(r));modern.set(snapshot(r));}
 const x=old.frame(1/60,0).rows[0],y=modern.frame(1/60,0).rows[0];maxError=Math.max(maxError,Math.abs(x.rate-y.rate));
}
check('original-writing-cadence-remains-identical',maxError<1e-8,{maxValueError:maxError});
const churn=new RA.Animator();churn.showImmediate();let maxPoses=0;
for(let i=0;i<1200;i++){
 if(i%10===0)churn.set({...snapshot(base),rows:Array.from({length:16},(_,j)=>({...measured,thread_id:'row-'+(Math.floor(i/10)*16+j),turn_id:'turn-'+i}))});
 churn.frame(1/60,0);maxPoses=Math.max(maxPoses,churn.poses.size);
}
churn.set({...snapshot(base),rows:[]});for(let i=0;i<360;i++)churn.frame(1/60,0);
check('row-churn-remains-bounded-and-clears',maxPoses<=32&&churn.poses.size===0,{maxPoses,remaining:churn.poses.size});
function benchmark(NS){const anim=new NS.Animator();anim.showImmediate();const r={...measured,activity:'running'},snap={...snapshot(r),rows:Array.from({length:16},(_,j)=>({...r,thread_id:'bench-'+j}))};anim.set(snap);for(let i=0;i<1000;i++)anim.frame(1/60,0);const started=performance.now();for(let i=0;i<12000;i++){if(i%15===0)anim.set(snap);anim.frame(1/60,0);}return (performance.now()-started)/12000;}
const bench={beforeFrameMs:benchmark(Old),afterFrameMs:benchmark(RA)};
const result={result:'PASS',checks,benchmark:bench,scope:'Synthetic current-state cases and CPU animation calculation; GPU performance measured separately',runtimeHistoryStored:false};
fs.writeFileSync(path.join(out,'verification.json'),JSON.stringify(result,null,2));console.log(JSON.stringify(result,null,2));
