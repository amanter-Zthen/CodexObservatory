/* G02-BG08: two persistent currents, visible tiers at 2/4 sessions, warm hierarchy.
 * The reference palette is sampled from warm regions, not guessed material IDs.
 */
(function(NS){
 'use strict';
 const P=Object.freeze({
  coreCount:2,detailThresholds:[2,2,4,4],maxCurves:6,maxFlowSources:32,
  mainHalfWidth:13.5,secondHalfWidth:8,secondOpacity:.62,
  detailHalfWidths:[4.0,2.8,3.6,2.5],detailOpacities:[.44,.34,.40,.30],
  detailTracks:[.28,.80,.17,.68],
  palette:[[181,160,128],[203,182,155],[224,204,183],[244,226,205],[255,246,228]],
  emissionGain:.85
 });
 const vec=a=>'vec3('+a.map(x=>(x/255).toFixed(6)).join(',')+')';
 const paletteGLSL=`
vec3 ribbonTone(float t){
 t=clamp(t,0.,1.)*4.;
 if(t<1.)return mix(${vec(P.palette[0])},${vec(P.palette[1])},t);
 if(t<2.)return mix(${vec(P.palette[1])},${vec(P.palette[2])},t-1.);
 if(t<3.)return mix(${vec(P.palette[2])},${vec(P.palette[3])},t-2.);
 return mix(${vec(P.palette[3])},${vec(P.palette[4])},t-3.);
}
float ribbonShoulder(float energy){return 1.-exp(-max(energy,0.));}
`;
 NS.ribbonArt={P,paletteGLSL};
})(window.RA);
