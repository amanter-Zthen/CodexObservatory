// G02: 0.4.8 motion.go's tuned deformation kernel, adapted to the glow ribbon field.
// Position, material, twist/depth and stage masks remain those of the glow design.
(function(NS){
  'use strict';
  const P=Object.freeze({counter:.32,slow:.20,warp:.19,drift:.24,twistK:.38,twistRange:.16,twistTime:.40});
  const f=n=>n.toFixed(6),TAU=Math.PI*2;
  function ribbonY(R,i,x,W){
    const o=i*4,A=R.A,B=R.B,u=(B[o+2]>0?x:W-x)/W,ph=A[o+3],sd=B[o+3],k=A[o+2];
    const env=Math.sin(Math.PI*u),warp=P.warp*Math.sin(u*4.1+ph*.39+sd),drift=R.D[i];
    const carrier=Math.sin(u*k-ph*.67+warp+drift);
    const counter=P.counter*Math.sin(u*10.3+ph*.41+sd*1.7);
    const slow=P.slow*Math.sin(u*3.7-ph*.23+sd*.8);
    const packet=.75+.25*Math.sin(u*6.7-ph*.52+sd);
    return A[o]+A[o+1]*(carrier+counter+slow)*packet*(.65+.35*env)
      +(u-.5)*R.H*.10*Math.cos(sd*1.1+ph*.09);
  }
  const ribbonGLSL=[
    'float ribY(int i, float x, out float tw){',
    ' vec4 A=uRibA[i],B=uRibB[i];',
    ' float u=(B.z>0.?x:uPanel.x-x)/uPanel.x,ph=A.w,sd=B.w,k=A.z;',
    ' float env=sin(PI*u),warp='+f(P.warp)+'*sin(u*4.1+ph*.39+sd);',
    ' float carrier=sin(u*k-ph*.67+warp+uRibD[i]);',
    ' float counter='+f(P.counter)+'*sin(u*10.3+ph*.41+sd*1.7);',
    ' float slow='+f(P.slow)+'*sin(u*3.7-ph*.23+sd*.8);',
    ' float packet=.75+.25*sin(u*6.7-ph*.52+sd);',
    ' float y=A.x+A.y*(carrier+counter+slow)*packet*(.65+.35*env)+(u-.5)*uPanel.y*.10*cos(sd*1.1+ph*.09);',
    ' tw=sin(u*('+f(P.twistK)+'+'+f(P.twistRange)+'*h11(sd))*TAU-ph*'+f(P.twistTime)+'+sd*2.3);',
    ' return y;',
    '}'
  ].join('\n');
  const seaGLSL=[
    'float seaY(float x,float k,float lv){',
    ' float u=x/uPanel.x,base=mix(uPanel.y-4.,34.,lv)+k*4.5;',
    ' return base+(2.+1.2*k)*(sin(u*PI*2.1-uTime*.39+k*1.9)+.33*sin(u*PI*5.+uTime*.28+k*.7));',
    '}'
  ].join('\n');
  NS.Flow={P,ribbonY,ribbonGLSL,seaGLSL};
})(window.RA);
