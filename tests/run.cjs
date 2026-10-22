'use strict';
const fs = require('fs'), path = require('path'), {spawnSync} = require('child_process');
fs.mkdirSync(path.join(__dirname,'../verification'),{recursive:true});
const files = ['verify.cjs','verify-choreography.cjs','verify-glass-regression.cjs',
  'verify-timing.cjs','verify-identity.cjs','verify-ribbon-tiers.cjs','verify-package.cjs'];
const started = new Date().toISOString(), tests = [];
for (const file of files) {
  const run = spawnSync(process.execPath, [path.join(__dirname,file)], {encoding:'utf8', windowsHide:true, timeout:180000});
  fs.writeFileSync(path.join(__dirname,file+'.log'), (run.stdout||'')+(run.stderr||''));
  const record = {file,exitCode:run.status,pass:run.status===0,error:run.error?.message};
  tests.push(record);
  console.log(`${record.pass?'PASS':'FAIL'} ${file}`);
  if (!record.pass) console.error((run.stderr||run.stdout||run.error?.message||'').slice(-3500));
}
const result = {version:'0.4.9',started,finished:new Date().toISOString(),result:tests.every(t=>t.pass)?'PASS':'FAIL',tests};
fs.writeFileSync(path.join(__dirname,'../verification/visual-suite.json'),JSON.stringify(result,null,2));
if(result.result!=='PASS') process.exitCode=1;
