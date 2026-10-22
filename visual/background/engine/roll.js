/* Shared rearward transport pose. The four-slot track is authoritative;
 * every pane receives the same bounded arc, with its local field tilt on top.
 * Velocity-driven pose is continuous when a live state retargets mid-flight.
 * No separate event queue, scene copies, or accumulated rotation.
 */
(function(NS){
 'use strict';
 const params=Object.freeze({peakDegrees:5.2,pivotY:2.2,pivotZ:.31,backArc:.12,riseArc:0});
 function sample(a,enabled=true){
  const peakSpeed=NS.archive.params.rearSteps*.62*NS.archive.params.trackRate/Math.E;
  const speed=enabled?Math.abs(a.railVelocity||0)/peakSpeed:0;
  const weight=1-Math.exp(-2.3*speed*speed),angle=-params.peakDegrees*Math.PI/180*weight;
  return{weight,angle,cos:Math.cos(angle),sin:Math.sin(angle),back:-params.backArc*weight,rise:params.riseArc*weight,pivotY:params.pivotY,pivotZ:params.pivotZ};
 }
 function vector(v,out,g){out[0]=v[0];const y=v[1],z=v[2];out[1]=y*g.cos-z*g.sin;out[2]=y*g.sin+z*g.cos;return out;}
 function point(v,out,g){const x=v[0],y=v[1]-g.pivotY,z=v[2]-g.pivotZ;out[0]=x;out[1]=g.pivotY+y*g.cos-z*g.sin+g.rise;out[2]=g.pivotZ+y*g.sin+z*g.cos+g.back;return out;}
 NS.roll={params,sample,point,vector};
})(window.BG16);
