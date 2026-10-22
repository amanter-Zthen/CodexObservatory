// One deterministic timeline for the two opposing identity captions and bars.
// Glyph motion is applied to the existing instance data; atlas and GPU storage
// remain shared with the other labels.
(function(NS){
 'use strict';
 const {clamp01,smoother}=NS.core;
 const P=Object.freeze({
  headerStart:.65,footerStart:1.03,
  glyphEnter:.50,glyphEnterStagger:.016,glyphEnterDistance:20,
  glyphExit:.62,glyphExitStagger:.018,glyphExitDistance:34,
  headerBarStart:.49,headerBarDuration:.36,
  headerLineStart:.70,headerLineDuration:.86,
  footerBarStart:.90,footerBarDuration:.56,
  barExitStart:.22,barExitDuration:.48,
  headerBarExitStart:.58,headerBarExitDuration:.28
 });
 const settle=u=>{u=clamp01(u);return smoother(u+.32*u*(1-u));};
 const accelerate=u=>Math.pow(clamp01(u),3);
 const pop=u=>{u=clamp01(u);const x=u-1;return 1+1.65*x*x*x+.65*x*x;};
 const times=T=>({enter:T.enter*NS.STAGE.ENTER_DUR,exit:T.exit*NS.STAGE.EXIT_DUR});
 function glyph(side,index,count,T){
  const t=times(T),header=side==='header',order=header?index:count-1-index,dir=header?1:-1;
  const entering=clamp01((t.enter-(header?P.headerStart:P.footerStart)-order*P.glyphEnterStagger)/P.glyphEnter);
  const leaving=clamp01((t.exit-order*P.glyphExitStagger)/P.glyphExit);
  return{x:dir*(P.glyphEnterDistance*(1-settle(entering))-P.glyphExitDistance*accelerate(leaving)),
   alpha:smoother(entering/.18)*(1-smoother((leaving-.32)/.68)),entering,leaving};
 }
 function bars(T){
  const t=times(T),exit=accelerate((t.exit-P.barExitStart)/P.barExitDuration);
  return{
   headerRise:settle((t.enter-P.headerBarStart)/P.headerBarDuration)*(1-accelerate((t.exit-P.headerBarExitStart)/P.headerBarExitDuration)),
   headerReach:pop((t.enter-P.headerLineStart)/P.headerLineDuration)*(1-exit),
   footerReach:pop((t.enter-P.footerBarStart)/P.footerBarDuration)*(1-exit)
  };
 }
 NS.hudMotion={P,glyph,bars,settle,accelerate,pop};
})(window.RA);
