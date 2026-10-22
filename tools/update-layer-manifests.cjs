'use strict';
const fs=require('fs'),path=require('path');
const root=path.resolve(__dirname,'..'),lock=JSON.parse(fs.readFileSync(path.join(root,'layers.lock.json'),'utf8'));
const keys=['background','curves','particles','frame','info','glow'];
const names=['Glass background','Flowing ribbons','Floating particles','Frame guides','Information text','Foreground bloom'];
fs.mkdirSync(path.join(root,'layers'),{recursive:true});
lock.layers.forEach((layer,i)=>{
 const value={version:lock.version,id:layer.id,key:keys[i],name:{'zh-CN':layer.name,en:names[i]},preview:'media/layer-'+keys[i]+'.png',files:layer.files.map(file=>({path:'visual/'+file,sha256:lock.runtimeHashes[file]})),sharedRuntime:'layers.lock.json'};
 if(value.files.some(f=>!f.sha256))throw Error('Layer source is not pinned');
 fs.writeFileSync(path.join(root,'layers',layer.id+'-'+keys[i]+'.json'),JSON.stringify(value,null,2)+'\n');
});
console.log('Updated six per-layer manifests from the existing source lock.');
