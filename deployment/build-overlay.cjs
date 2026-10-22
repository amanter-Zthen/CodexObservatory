'use strict';
// Offline build-only projection. Source files stay unchanged; generated files stay in deployment/.
const fs=require('fs'),path=require('path'),crypto=require('crypto'),{spawnSync}=require('child_process');
const release=require('../tools/release-common.cjs');
if(process.argv.includes('--help')){
 console.log('Usage: node deployment/build-overlay.cjs\nWindows AMD64 build + isolated overlay tests. Set OBSERVATORY_GO to a Go 1.25+ executable, or use go from PATH. OBSERVATORY_GO_CACHE optionally overrides the normal Go cache. All module dependencies come from native/vendor. No application or installer is launched.');
 process.exit(0);
}
if(process.argv.length>2)throw Error('Unknown argument; use --help');
const project=path.resolve(__dirname,'..'),native=path.join(project,'native');
const build=path.join(__dirname,'build'),generated=path.join(build,'overlay');
const go=process.env.OBSERVATORY_GO||'go';
const cache=process.env.OBSERVATORY_GO_CACHE?path.resolve(process.env.OBSERVATORY_GO_CACHE):null;
const binary=path.join(build,'CodexObservatory.exe');
const hash=data=>crypto.createHash('sha256').update(data).digest('hex');
const fileHash=p=>hash(fs.readFileSync(p));
function treeHashes(){return release.hashFiles(project,release.walk(project,'native'));}
const overlayNames=['display_host_windows.go','display_host_windows_test.go','display_controls_windows.go','display_controls_windows_test.go','route_pulse_windows.go','route_pulse_windows_test.go','startup_visibility_windows_test.go'];
const sourceInputs=['deployment/build-overlay.cjs','deployment/native-source-manifest.json','tools/release-common.cjs',...overlayNames.map(file=>'deployment/overlay/'+file)];
const relative=p=>path.relative(project,p).replaceAll('\\','/');
const report={formatVersion:1,kind:'codex-observatory-backend-build',started:new Date().toISOString(),binary:relative(binary),nativeBaseline:'0.4.8-nacre',
 parameters:{goSelection:process.env.OBSERVATORY_GO?'OBSERVATORY_GO':'PATH',cacheSelection:cache?'OBSERVATORY_GO_CACHE':'Go default or inherited GOCACHE',goos:'windows',goarch:'amd64',moduleMode:'vendor',toolchain:'local',cgo:false,productionMutation:'NONE'},
 configSchema:{file:'display-host.json beside the backend executable',reload:'every new process launch or reopen; showing an existing child keeps its selection',fields:{enabled:'boolean; true selects the candidate',executable:'canonical absolute local EXE path, <=2048 UTF16 units',state:'canonical absolute local directory path, <=2048 UTF16 units',sha256:'64 hexadecimal characters; validated on every launch'},maxBytes:16384,maxExecutableBytes:134217728,
  childArguments:['--instance','<backend data>/instance.json','--state','<state>','--owner-pid','<backend PID>']},
 controlsEndpoint:{path:'/api/display-controls',method:'GET',authentication:'existing /api middleware',fields:['version','route_sync'],sideEffects:'NONE'},
 pulseEndpoint:{path:'/api/routing/pulse',method:'POST',body:{},authentication:'existing /api middleware',responseFields:['accepted'],queueCapacity:1,worker:'existing liveFeedLoop',cursor:'preserved',historyReplay:false,previewAndDemo:false},
 startupDashboard:{automatic:false,explicitOpen:'existing tray/menu actions or --no-open=false',watcherUnchanged:true},
 commands:[],testsPassed:false,nativeUnchanged:false,sha256:null,overlayHash:null,artifacts:{},result:'FAIL'};
let before;
function run(args,label,timeout=120000){
 const env={...process.env,GOTOOLCHAIN:'local',GOPROXY:'off',GOSUMDB:'off',GOENV:'off',GOFLAGS:'',GOOS:'windows',GOARCH:'amd64',CGO_ENABLED:'0',TMP:path.join(build,'tmp'),TEMP:path.join(build,'tmp'),GOTMPDIR:path.join(build,'tmp')};
 if(cache)env.GOCACHE=cache;
 const r=spawnSync(go,args,{cwd:native,env,encoding:'utf8',windowsHide:true,timeout,maxBuffer:16*1024*1024});
 fs.writeFileSync(path.join(build,label+'.log'),(r.stdout||'')+(r.stderr||''));
 report.commands.push({label,arguments:args.map(arg=>arg.startsWith(project+path.sep)?relative(arg):arg),exitCode:r.status,error:r.error?.code});
 if(r.status!==0)throw Error(label+' failed: '+(r.stderr||r.stdout||r.error?.message||'').slice(-5000));
 return r.stdout;
}
function renameOnce(text,from,to){
 if(text.split(from).length!==2)throw Error('Expected exactly one original declaration/reference: '+from);
 return text.replace(from,to);
}
try{
 if(process.platform!=='win32')throw Error('Run this entry on Windows: its isolated tests exercise native Windows lifecycle and message contracts.');
 for(const p of [build,generated,path.join(build,'tmp')]){
  release.projectPath(project,relative(p),true);
  fs.mkdirSync(p,{recursive:true});
 }
 for(const filename of ['CodexObservatory.exe','CodexObservedLauncher.exe','CodexRoutingBridge.exe'])release.projectPath(project,relative(path.join(build,filename)),true);
 const baseline=release.verifyNative(project);
 before=treeHashes();
 report.nativeFiles=baseline.files;report.nativeBaselineVerifiedFiles=baseline.files;
 report.nativeManifestSHA256=baseline.manifestSHA256;
 report.nativeTreeSHA256=hash(JSON.stringify(before));
 report.sourceHashes=release.hashFiles(project,sourceInputs);
 report.goVersion=run(['version'],'go-version').trim();
 const overlay={Replace:{}};
 let main=fs.readFileSync(path.join(native,'main.go'),'utf8');
 main=renameOnce(main,'noOpen := flag.Bool("no-open", false, "Do not open the local dashboard")','noOpen := flag.Bool("no-open", true, "Do not open the local dashboard")');
 const mainProjected=path.join(generated,'main.go');
 fs.writeFileSync(mainProjected,main);overlay.Replace[path.join(native,'main.go')]=mainProjected;
 let original=fs.readFileSync(path.join(native,'floating_native_windows.go'),'utf8');
 original=renameOnce(original,'func (a *App) startFloating() func() {','func (a *App) startLegacyFloating() func() {');
 original=renameOnce(original,'return a.startLegacyFloating()','return a.startMinimalFloating()');
 const nativeProjected=path.join(generated,'floating_native_windows.go');
 fs.writeFileSync(nativeProjected,original);overlay.Replace[path.join(native,'floating_native_windows.go')]=nativeProjected;
 let simple=fs.readFileSync(path.join(native,'floating_windows.go'),'utf8');
 simple=renameOnce(simple,'func showFloating(data string) {','func showLegacyFloating(data string) {');
 simple=renameOnce(simple,'func (a *App) startLegacyFloating() func() {','func (a *App) startMinimalFloating() func() {');
 const simpleProjected=path.join(generated,'floating_windows.go');
 fs.writeFileSync(simpleProjected,simple);overlay.Replace[path.join(native,'floating_windows.go')]=simpleProjected;
 let server=fs.readFileSync(path.join(native,'server.go'),'utf8');
 server=renameOnce(server,'mux.HandleFunc("/api/state", a.apiState)','mux.HandleFunc("/api/state", a.apiState)\n\tmux.HandleFunc("/api/display-controls", a.apiDisplayControls)\n\tmux.HandleFunc("/api/routing/pulse", a.apiRoutePulse)');
 const serverProjected=path.join(generated,'server.go');
 fs.writeFileSync(serverProjected,server);overlay.Replace[path.join(native,'server.go')]=serverProjected;
 const adapter=fs.readFileSync(path.join(native,'floating_adapter.go'),'utf8').replace(/\r\n/g,'\n');
 const loopStart=adapter.indexOf('func (a *App) liveFeedLoop() {');
 if(loopStart<0)throw Error('Original live feed loop was not found');
 let loop=adapter.slice(loopStart);
 loop=renameOnce(loop,'func (a *App) liveFeedLoop() {',
  'func (a *App) liveFeedLoop() {\n\ta.liveFeedLoopWithPulse(routePulseRuntime{})\n}\n\nfunc (a *App) liveFeedLoopWithPulse(pulseRuntime routePulseRuntime) {\n\tpulse := a.registerRoutePulse()\n\tif pulse == nil {\n\t\treturn\n\t}\n\tdefer a.releaseRoutePulse(pulse)\n\tpulseRuntime = a.routePulseRuntimeDefaults(pulseRuntime)');
 loop=renameOnce(loop,'wake := a.routeSyncChannel()',
  'wake := a.routeSyncChannel()\n\tticks := ticker.C\n\tif pulseRuntime.ticks != nil {\n\t\tticks = pulseRuntime.ticks\n\t}');
 loop=renameOnce(loop,'case <-ticker.C:\n\t\tcase <-wake:',
  'case <-ticks:\n\t\tcase <-pulse.wake:\n\t\tcase <-wake:');
 const cycleStart=loop.indexOf('\t\tif !syncDeadline.IsZero() && time.Now().After(syncDeadline) {');
 const cycleEnd=loop.lastIndexOf('\n\t}\n}');
 if(cycleStart<0||cycleEnd<=cycleStart||loop.slice(cycleEnd).trim()!=='}\n}')throw Error('Original feed-cycle boundaries changed');
 let cycle=loop.slice(cycleStart,cycleEnd);
 if(cycle.split('\t\t\tcontinue\n').length!==3)throw Error('Expected the two original feed-cycle early exits');
 cycle=cycle.replaceAll('\t\t\tcontinue\n','\t\t\treturn\n');
 cycle=renameOnce(cycle,'var next routefeed.Description\n\t\t\tif loadJSON(filepath.Join(a.data, routefeed.DiscoveryFile), &next) == nil && routefeed.ValidDescription(next) {',
  'next, err := pulseRuntime.discover()\n\t\t\tif err == nil && routefeed.ValidDescription(next) {');
 cycle=renameOnce(cycle,'routefeed.Fetch(ctx, client, description, epoch, sequence)','pulseRuntime.fetch(ctx, client, description, epoch, sequence)');
 cycle=renameOnce(cycle,'a.acceptFloatingRouteReplay(item.Record, !syncDeadline.IsZero())','pulseRuntime.accept(item.Record, !syncDeadline.IsZero())');
 cycle=renameOnce(cycle,'a.publishFloating()','pulseRuntime.publish()');
 cycle=renameOnce(cycle,'a.refreshFloating()','pulseRuntime.refresh()');
 cycle=renameOnce(cycle,'a.completeRouteSync()','pulseRuntime.complete()');
 cycle=renameOnce(cycle,
  '\t\t\tif sink := a.routeAuditSink(); sink != nil {\n\t\t\t\trecords := make([]routetrace.Record, 0, len(batch.Items))\n\t\t\t\tfor _, item := range batch.Items {\n\t\t\t\t\trecords = append(records, item.Record)\n\t\t\t\t}\n\t\t\t\tsink.Publish(records)\n\t\t\t}',
  '\t\t\trecords := make([]routetrace.Record, 0, len(batch.Items))\n\t\t\tfor _, item := range batch.Items {\n\t\t\t\trecords = append(records, item.Record)\n\t\t\t}\n\t\t\tpulseRuntime.audit(records)');
 loop=loop.slice(0,cycleStart)+'\t\tpulse.begin()\n\t\tfunc() {\n\t\t\tdefer pulse.end()\n'+cycle.split('\n').map(line=>'\t'+line).join('\n')+'\n\t\t}()'+loop.slice(cycleEnd);
 const adapterProjected=path.join(generated,'floating_adapter.go');
 fs.writeFileSync(adapterProjected,adapter.slice(0,loopStart)+loop);overlay.Replace[path.join(native,'floating_adapter.go')]=adapterProjected;
 for(const file of overlayNames){
  overlay.Replace[path.join(native,'zz_'+file)]=path.join(__dirname,'overlay',file);
 }
 const overlayPath=path.join(build,'overlay.json');
 fs.writeFileSync(overlayPath,JSON.stringify(overlay,null,2));
 report.overlay=relative(overlayPath);
 report.overlayFiles=Object.fromEntries(Object.entries(overlay.Replace).map(([logical,physical])=>[relative(logical),{source:relative(physical),sha256:fileHash(physical)}]));
 report.overlayHash=hash(JSON.stringify(report.overlayFiles));
 const args=['test','-mod=vendor','-overlay',overlayPath,'-count=1','-timeout=60s','-json','-run','^(TestDisplayOverlay|TestRouteSync)','.'];
 const tests=run(args,'display-overlay-tests');
 const events=tests.split(/\r?\n/).flatMap(line=>{try{return[JSON.parse(line)];}catch{return[];}});
 const passes=events.filter(e=>e.Action==='pass'&&e.Test),failures=events.filter(e=>e.Action==='fail');
 report.testSummary={topLevelPasses:passes.filter(e=>!e.Test.includes('/')).length,passEventsIncludingSubtests:passes.length,failures:failures.length,names:passes.filter(e=>!e.Test.includes('/')).map(e=>e.Test)};
 if(report.testSummary.topLevelPasses<26||failures.length)throw Error('Overlay and original manual-resync suites did not exercise all expected checks');
 report.testsPassed=true;
 run(['build','-mod=vendor','-overlay',overlayPath,'-buildvcs=false','-trimpath','-ldflags=-H=windowsgui','-o',binary,'.'],'backend-build',180000);
 report.artifacts.backend={path:relative(binary),...release.inspectPE(binary,2)};
 report.sha256=report.artifacts.backend.sha256;report.bytes=report.artifacts.backend.bytes;report.windowsSubsystem=2;
 const watcher=path.join(build,'CodexObservedLauncher.exe'),bridge=path.join(build,'CodexRoutingBridge.exe');
 run(['build','-mod=vendor','-buildvcs=false','-trimpath','-ldflags=-H=windowsgui','-o',watcher,'./cmd/codex-observed-launcher'],'watcher-build',180000);
 report.artifacts.watcher={path:relative(watcher),...release.inspectPE(watcher,2)};
 // The bridge is a stdio compatibility helper, so retain the console PE subsystem.
 run(['build','-mod=vendor','-buildvcs=false','-trimpath','-o',bridge,'./cmd/codex-routing-bridge'],'bridge-build',180000);
 report.artifacts.bridge={path:relative(bridge),...release.inspectPE(bridge,3)};
 release.checkHashes(project,report.sourceHashes,'Build input');
 report.nativeUnchanged=JSON.stringify(treeHashes())===JSON.stringify(before);
 if(!report.nativeUnchanged)throw Error('Frozen native source changed during the build');
 report.result='PASS';
}catch(error){report.error=String(error.stack||error);console.error(report.error);process.exitCode=1;}
finally{
 if(before){
  try{report.nativeUnchanged=JSON.stringify(treeHashes())===JSON.stringify(before);}
  catch(error){report.nativeUnchanged=false;report.error=String(error.message||error);}
  if(!report.nativeUnchanged){report.result='FAIL';process.exitCode=1;}
 }
 report.finished=new Date().toISOString();
 fs.mkdirSync(__dirname,{recursive:true});fs.writeFileSync(path.join(__dirname,'build-report.json'),JSON.stringify(report,null,2));
 console.log(JSON.stringify({result:report.result,binary:report.binary,sha256:report.sha256,overlayHash:report.overlayHash,nativeUnchanged:report.nativeUnchanged,testsPassed:report.testsPassed,testSummary:report.testSummary}));
}
