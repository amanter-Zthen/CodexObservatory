'use strict';
const fs=require('fs'),path=require('path'),crypto=require('crypto'),assert=require('assert');
const root=path.resolve(__dirname,'..'),hash=p=>crypto.createHash('sha256').update(fs.readFileSync(p)).digest('hex');
const lock=JSON.parse(fs.readFileSync(path.join(root,'layers.lock.json'),'utf8'));
for(const [f,h]of Object.entries(lock.runtimeHashes))assert.equal(hash(path.join(root,'visual',f)),h,f);
assert.equal(hash(path.join(root,'visual/index.html')),lock.previewHtmlSha256);
const native=JSON.parse(fs.readFileSync(path.join(root,'deployment/native-source-manifest.json'),'utf8'));
for(const f of native.files)assert.equal(hash(path.join(root,f.path)),f.sha256,f.path);
for(const n of ['BUILD','INTEGRATION','API','LAYERS','MEDIA','VALIDATION'])for(const lang of ['zh-CN','en'])assert(fs.existsSync(path.join(root,'docs',n+'.'+lang+'.md')),n+' '+lang);
const manifests=fs.readdirSync(path.join(root,'layers')).filter(n=>n.endsWith('.json'));assert.equal(manifests.length,6);
for(const f of manifests){const l=JSON.parse(fs.readFileSync(path.join(root,'layers',f)));assert(fs.existsSync(path.join(root,l.preview)),f);for(const s of l.files)assert.equal(hash(path.join(root,s.path)),s.sha256);}
assert(fs.statSync(path.join(root,'media/preview.gif')).size<10_000_000,'README GIF exceeds free attachment limit');
console.log(JSON.stringify({result:'PASS',runtimeScripts:Object.keys(lock.runtimeHashes).length,nativeFiles:native.files.length,layers:manifests.length,bilingualDocPairs:6,previewBytes:fs.statSync(path.join(root,'media/preview.gif')).size}));
