/* Fixed-capacity 2.5D slab pool. 6 banks x 48 rows, reused across changes.
 * Four settled columns, including one additional foreground column. Incoming
 * and outgoing banks overlap during travel and are reused beyond visibility.
 * A moving finite visibility window follows the shared tracks; slot coordinates
 * rebase every 16 steps. No meshes are appended to the scene during navigation.
 */
(function(NS){
 'use strict';
 const ROW_SLOTS=NS.archive.params.rowSlots,COLUMN_SLOTS=NS.archive.params.columnSlots;
 const SCALE=82,ORIGIN=[836,685],yaw=59*Math.PI/180,elev=19*Math.PI/180,MAX_PANELS=ROW_SLOTS*COLUMN_SLOTS;
 const X=[Math.cos(yaw),-Math.sin(elev)*Math.sin(yaw),-Math.cos(elev)*Math.sin(yaw)];
 const Y=[0,-Math.cos(elev),Math.sin(elev)];
 const Z=[Math.sin(yaw),Math.sin(elev)*Math.cos(yaw),Math.cos(elev)*Math.cos(yaw)];
 function project(p){return[ORIGIN[0]+SCALE*(X[0]*p[0]+Y[0]*p[1]+Z[0]*p[2]),ORIGIN[1]+SCALE*(X[1]*p[0]+Y[1]*p[1]+Z[1]*p[2])];}
 const smooth=t=>{t=Math.max(0,Math.min(1,t));return t*t*t*(t*(t*6-15)+10);};
 const wrap=n=>((n%1024)+1024)%1024;
 const definitions=[
  {key:'front',type:0,origin:[0,3.7,.22],du:[5,0,0],dv:[0,-3.7,0],normal:Z,tu:X,tv:Y.map(x=>-x),wn:[0,0,1],wu:[1,0,0],wv:[0,-1,0]},
  {key:'end',type:1,origin:[0,3.7,0],du:[0,0,.22],dv:[0,-3.7,0],normal:X.map(x=>-x),tu:Z,tv:Y.map(x=>-x),wn:[-1,0,0],wu:[0,0,1],wv:[0,-1,0]},
  {key:'top',type:2,origin:[0,3.7,0],du:[5,0,0],dv:[0,0,.22],normal:Y,tu:X,tv:Z,wn:[0,1,0],wu:[1,0,0],wv:[0,0,1]}
 ];
 function slot(){
  return{parts:definitions.map(d=>{
   const q0=project(d.origin),q1=project(d.origin.map((x,i)=>x+d.du[i])),q3=project(d.origin.map((x,i)=>x+d.dv[i]));
   return{key:d.key,type:d.type,localOrigin:d.origin,localDU:d.du,localDV:d.dv,normal:[...d.normal],tu:[...d.tu],tv:[...d.tv],worldOrigin:[0,0,0],worldDU:[0,0,0],worldDV:[0,0,0],
    span:[Math.hypot(q1[0]-q0[0],q1[1]-q0[1]),Math.hypot(q3[0]-q0[0],q3[1]-q0[1])],p:Array.from({length:4},()=>[0,0])};
  }),baseA:[0,0],baseB:[0,0],worldCenter:[0,0,0],length:5,height:3.7,depth:.22,motion:{idle:0,pulse:0,lift:0,crest:0},appearance:{}};
 }
 const pool=Array.from({length:MAX_PANELS},slot),panels=[],scene={W:1672,H:941,t:0,panels,hero:null,archive:null};
 const columnPool=Array.from({length:COLUMN_SLOTS},(_,slot)=>({slot,key:null,opacity:0,phase:'hidden',rebuilds:0}));
 const columnStats={rebuilds:0,visibleRebinds:0};let lastTime=-Infinity,lastPrevious=null;
 const mod=(x,n)=>((x%n)+n)%n;
 function columnOpacity(relative,threeColumns=false){
  const front=threeColumns?-1:-2;
  if(relative>=front&&relative<=1)return 1;
  // Arrival belongs to the same transport as the roll: become visible near
  // its beginning and reach full density at 60% travel. A separate hidden
  // bank permits overlap while the far-side column clears.
  return relative>1?1-smooth((relative-1)/.74):smooth((relative-front+1-.02)/.58);
 }
 function rowOpacity(relative){return smooth((relative+23)/6)*(1-smooth((relative-17)/6));}
 const defaultState=new NS.archive.Timeline();
 function layout(t=0,o={}){
  const a=o.archive||defaultState.seek(t),centerLane=1+a.trackX/5.2,centerRow=7-a.rail/.62;
  const threeColumns=o.movementStyle==='previous',field=NS.switchField,roll=NS.roll;
  const group=roll.sample(a,o.groupRoll!==0);scene.group=group;
  const fieldState=field.state?field.state(a):null;
  const rowCount=ROW_SLOTS,firstLane=Math.floor(centerLane)-3,firstRow=Math.floor(centerRow)-24;
  scene.depthHalf=32;scene.compact=false;
  const seekJump=t<lastTime||t-lastTime>.20||threeColumns!==lastPrevious;lastTime=t;lastPrevious=threeColumns;
  for(let lane=firstLane;lane<firstLane+COLUMN_SLOTS;lane++){
   const rel=lane-centerLane,col=columnPool[mod(lane+(a.bankLane||0),COLUMN_SLOTS)],key=wrap(lane+a.originLane),opacity=columnOpacity(rel,threeColumns);
   if(col.key!==key){if(col.key!==null&&col.opacity>.001&&opacity>.001&&!seekJump)columnStats.visibleRebinds++;col.key=key;col.rebuilds++;columnStats.rebuilds++;}
   col.opacity=opacity;col.lane=lane;col.phase=opacity<.001?'hidden':rel>1?'exiting':rel<(threeColumns?-1:-2)?'entering':'stable';
  }scene.columns=columnPool;scene.columnStats=columnStats;
  panels.length=0;scene.hero=null;scene.t=t;scene.archive=a;
  for(let row=firstRow;row<firstRow+rowCount;row++)for(let lane=firstLane;lane<firstLane+COLUMN_SLOTS;lane++){
   const b=pool[mod(lane+(a.bankLane||0),COLUMN_SLOTS)*ROW_SLOTS+mod(row+(a.bankRow||0),ROW_SLOTS)],relLane=lane-centerLane,relRow=row-centerRow;
   b.opacity=columnOpacity(relLane,threeColumns)*rowOpacity(relRow);
   if(b.opacity<.001)continue;
   const materialRow=wrap(row+a.originRow),materialLane=wrap(lane+a.originLane);
   b.id=(materialRow<14&&materialLane<3)?materialRow*3+materialLane+1:100+materialRow*1024+materialLane;
   b.key=lane+':'+row;b.row=row;b.lane=lane;b.sourceRow=row+5;b.sourceLane=lane+1;
   b.x=(lane-1)*5.2-2.5-a.trackX;b.z=(row-6.5)*.62+a.rail;
   b.profileBase=field.profile(row,lane,a,fieldState);
   const screenX=project([b.x+2.5,b.profileBase+1.85,b.z+.11])[0],screenQ=Math.max(0,Math.min(1,(screenX-340.8599077206659)/(1346.6033903840002-340.8599077206659)));
   const m=NS.motion.sample(materialRow,materialLane,t,o,screenQ);Object.assign(b.motion,m);
   const r=a.records.find(r=>r.row===row&&r.lane===lane);
   b.archiveState=r?NS.archive.states[r.state].index:-1;b.archiveOpacity=r?smooth(r.lift/.32):0;
   b.archivePrevious=r?NS.archive.states[r.previous||r.state].index:-1;b.archiveMix=r?(r.inkMix??1):1;
   b.selectedLift=r?r.lift:0;
   const quality=smooth(b.selectedLift/.4),f=field.sample(row,lane,t,a,o,screenQ,quality,fieldState);
   const restDepth=X[2]*(b.x+2.5)+Y[2]*(b.profileBase+1.85)+Z[2]*(b.z+.11);
   const appearance=NS.appearance.sample(o.glassAppearance,restDepth,b.appearance);
   b.opacity*=appearance.visibility;
   if(b.opacity<.001)continue;
   b.base=b.profileBase+(f.base-b.profileBase)*appearance.ambient+b.selectedLift+appearance.offset;
   b.lift=f.base-b.profileBase;b.pitch=f.pitch+appearance.pitch;b.eventWave=f.selection;
   b.kind=lane===a.cell.lane&&row===a.cell.row?'hero':r?'outgoing':'array';b.energy=.45+.55*Math.exp(-Math.pow((row-a.shoulder)/4,2));
   let xMin=Infinity,xMax=-Infinity,yMin=Infinity,yMax=-Infinity;
   b.totalPitch=b.pitch+group.angle;
   const cs=Math.cos(b.pitch),sn=Math.sin(b.pitch),totalCS=Math.cos(b.totalPitch),totalSN=Math.sin(b.totalPitch);
   b.worldCenter[0]=b.x+2.5;b.worldCenter[1]=b.base+1.85*cs-.11*sn;b.worldCenter[2]=b.z+1.85*sn+.11*cs;NS.roll.point(b.worldCenter,b.worldCenter,group);
   for(const part of b.parts){const d=definitions[part.type];
    const vectors=[[d.origin,part.worldOrigin],[d.du,part.worldDU],[d.dv,part.worldDV]];
    for(const [src,dst]of vectors){dst[0]=src[0];dst[1]=src[1]*cs-src[2]*sn;dst[2]=src[1]*sn+src[2]*cs;}
    part.worldOrigin[0]+=b.x;part.worldOrigin[1]+=b.base;part.worldOrigin[2]+=b.z;
    NS.roll.point(part.worldOrigin,part.worldOrigin,group);NS.roll.vector(part.worldDU,part.worldDU,group);NS.roll.vector(part.worldDV,part.worldDV,group);
    for(const [src,dst]of [[d.wn,part.normal],[d.wu,part.tu],[d.wv,part.tv]]){
     const wx=src[0],wy=src[1]*totalCS-src[2]*totalSN,wz=src[1]*totalSN+src[2]*totalCS;
     for(let axis=0;axis<3;axis++)dst[axis]=X[axis]*wx+Y[axis]*wy+Z[axis]*wz;
    }
    for(let k=0;k<4;k++){
    const u=k===1||k===2?1:0,v=k===2||k===3?1:0;
    const wx=part.worldOrigin[0]+u*part.worldDU[0]+v*part.worldDV[0],wy=part.worldOrigin[1]+u*part.worldDU[1]+v*part.worldDV[1],wz=part.worldOrigin[2]+u*part.worldDU[2]+v*part.worldDV[2];
    const px=ORIGIN[0]+SCALE*(X[0]*wx+Y[0]*wy+Z[0]*wz),py=ORIGIN[1]+SCALE*(X[1]*wx+Y[1]*wy+Z[1]*wz);
    part.p[k][0]=px;part.p[k][1]=py;xMin=Math.min(xMin,px);xMax=Math.max(xMax,px);yMin=Math.min(yMin,py);yMax=Math.max(yMax,py);
   }part.span[0]=Math.hypot(part.p[1][0]-part.p[0][0],part.p[1][1]-part.p[0][1]);part.span[1]=Math.hypot(part.p[3][0]-part.p[0][0],part.p[3][1]-part.p[0][1]);b[part.key]=part.p;}
   b.baseA[0]=b.front[3][0];b.baseA[1]=b.front[3][1];b.baseB[0]=b.front[2][0];b.baseB[1]=b.front[2][1];
   b.xMin=b.x;b.xMax=b.x+5;b.zMin=b.z;b.zMax=b.z+.22;
   if(xMax< -70||xMin>1742||yMax< -70||yMin>1011)continue;
   if(b.kind==='hero')scene.hero=b;panels.push(b);
  }
  panels.sort((a,b)=>a.z-b.z||b.x-a.x);return scene;
 }
 NS.geometry={layout,project,X,Y,Z,SCALE,ORIGIN,ROW_SLOTS,COLUMN_SLOTS,MAX_PANELS,pool,columnPool,columnStats,columnOpacity,rowOpacity};
})(window.BG16);
