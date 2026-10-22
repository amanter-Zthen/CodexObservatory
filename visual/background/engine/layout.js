/* Reference distribution adapted from LBEILC/RhineLabUI (MIT).
 * Source snapshot: ee5779741c6c0c916e416705fa634c7abf905c73 src/motion.ts.
 * Archive field retains settlingWave(distance,26.56) * columnStrength.
 * Copyright (c) 2026 LBEILC. License: ../references/LICENSE.RhineLabUI.txt
 * We crop source rows 5..18 and lanes 1..3, without resampling the profile.
 */
(function(NS){
 'use strict';
 const smooth=t=>{t=Math.max(0,Math.min(1,t));return t*t*t*(10+t*(-15+6*t));};
 function settlingWave(distance,time){
  const age=time-25.05-Math.abs(distance)*.065;
  const envelope=Math.max(-.42,2.15-.17*(Math.sqrt(distance*distance+1)-1));
  const ring=age>0?Math.sin(age*5.1)*Math.exp(-age*1.3):0;
  return envelope*(smooth(age/.62)+.18*ring*smooth(age/.16));
 }
 function columnStrength(lane,focus){return .25+.75*Math.exp(-.5*Math.pow((lane-focus)/.55,2));}
 function height(row,lane){return settlingWave(row+5-12,26.56)*columnStrength(lane+1,2);}
 NS.layoutProfile={height,settlingWave,columnStrength,sourceTime:26.56,sourceCenter:12,sourceFocus:2,
  rowFirst:5,rowLast:18,laneFirst:1,laneLast:3,selectedRow:7,selectedLane:1,selectedLift:.4};
})(window.BG16=window.BG16||{});
