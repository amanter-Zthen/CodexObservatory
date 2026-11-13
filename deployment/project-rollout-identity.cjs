'use strict';
// Apply the identity repair to the retained 0.4.8 baseline at build time.
const fs=require('fs'),path=require('path');
module.exports=function({native,generated,overlay}) {
 const edit=(name,changes)=>{
  let text=fs.readFileSync(path.join(native,name),'utf8');
  for(const [from,to] of changes){
   if(text.split(from).length!==2)throw Error('Rollout identity projection anchor changed: '+name+' / '+from);
   text=text.replace(from,to);
  }
  const dest=path.join(generated,name);fs.writeFileSync(dest,text);overlay.Replace[path.join(native,name)]=dest;
 };
 edit('types.go', [['type FileState struct {','type FileState struct {\n\tIdentitySchema int `json:"rollout_identity_schema,omitempty"`']]);
 edit('parser.go', [['case "session_meta":','case "session_meta":\n\t\tif !ownRolloutMetadata(f, p) { return nil }\n\t\tf.IdentitySchema = 1']]);
 edit('store.go', [['\t\t\tif f.TierSchema < 1 {','\t\t\tif f.IdentitySchema < 1 {\n\t\t\t\tif err := a.backfillRolloutIdentity(&f); err != nil {\n\t\t\t\t\tlog.Printf("rollout identity checkpoint recovery: %v", err)\n\t\t\t\t} else if err := saveJSON(p, &f); err != nil {\n\t\t\t\t\tlog.Printf("rollout identity checkpoint save: %v", err)\n\t\t\t\t}\n\t\t\t}\n\t\t\tif f.TierSchema < 1 {']]);
 // A fork is a separate file. Keep the existing service-tier isolation test,
 // with an actual separate FileState rather than changing one file's owner.
 edit('service_tier_test.go', [['\tparseLine(f, rec("session_meta", Obj{"id": "fork"}), 4)','\tf = newState("home", "fork-file", "fork-key")\n\tparseLine(f, rec("session_meta", Obj{"id": "fork"}), 4)']]);
 for(const name of ['rollout_identity_windows.go','rollout_identity_windows_test.go','rollout_identity_migration_windows_test.go','rollout_identity_observed_windows_test.go']) {
  overlay.Replace[path.join(native,'zz_'+name)]=path.join(__dirname,'overlay',name);
 }
};
