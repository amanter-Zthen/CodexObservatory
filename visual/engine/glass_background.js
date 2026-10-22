/* BG16 is a bounded offscreen background. G02 retains foreground motion,
 * information, frame and lifecycle. Automatic framing follows the quota dock.
 */
(function(NS){
 'use strict';
 const B=window.BG16,V=NS.GLASS_VIEWS;
 class GlassBackground{
  constructor(gl,copy=false){
   this.gl=gl;this.canvas=document.createElement('canvas');this.canvas.width=V.canvas.width;this.canvas.height=V.canvas.height;
   this.copy=copy;this.painter=new B.Optical(this.canvas,copy?null:gl);this.timeline=null;
   this.profile='running';this.lastTime=-Infinity;this.draws=0;this.uploads=0;this.lastStats=null;
   this.texture=gl.createTexture();gl.bindTexture(gl.TEXTURE_2D,this.texture);
   // Immutable full mip chain: allocated once, refreshed in place. This gives
   // continuous low-pass levels without per-frame render-target allocation.
   this.mipLevels=1+Math.floor(Math.log2(Math.max(this.canvas.width,this.canvas.height)));
   gl.texStorage2D(gl.TEXTURE_2D,this.mipLevels,gl.RGBA8,this.canvas.width,this.canvas.height);
   gl.texParameteri(gl.TEXTURE_2D,gl.TEXTURE_MIN_FILTER,gl.LINEAR_MIPMAP_LINEAR);
   gl.texParameteri(gl.TEXTURE_2D,gl.TEXTURE_MAG_FILTER,gl.LINEAR);
   for(const p of[gl.TEXTURE_WRAP_S,gl.TEXTURE_WRAP_T])gl.texParameteri(gl.TEXTURE_2D,p,gl.CLAMP_TO_EDGE);
   if(!copy){
    this.framebuffer=gl.createFramebuffer();gl.bindFramebuffer(gl.FRAMEBUFFER,this.framebuffer);
    gl.framebufferTexture2D(gl.FRAMEBUFFER,gl.COLOR_ATTACHMENT0,gl.TEXTURE_2D,this.texture,0);
    if(gl.checkFramebufferStatus(gl.FRAMEBUFFER)!==gl.FRAMEBUFFER_COMPLETE)throw Error('Glass framebuffer unavailable');
    gl.bindFramebuffer(gl.FRAMEBUFFER,null);
   }
  }
  crop(profile,width,height,dock=0){
   const blend=profile==='auto'?NS.core.clamp01(dock):profile==='idle'?0:1;
   const cx=V.idle.center.x+(V.running.center.x-V.idle.center.x)*blend;
   const cy=V.idle.center.y+(V.running.center.y-V.idle.center.y)*blend;
   const idleWidth=V.idle.rect.width/NS.presentation.glassIdleZoom;
   const designScale=(idleWidth+(V.running.rect.width-idleWidth)*blend)/388;
   const scale=Math.min(designScale,2*Math.min(cx,this.canvas.width-cx)/width,2*Math.min(cy,this.canvas.height-cy)/height);
   return{x:cx-width*scale/2,y:cy-height*scale/2,width:width*scale,height:height*scale,center:[cx,cy],scale,blend};
  }
  update(time,profile,dynamics,frameInterval=1/30){
   if(profile==='legacy')return;
   const changed=profile!==this.profile||time<this.lastTime;
   const appearance=dynamics.appearance;
   const command=dynamics.id!==this.dynamicsId||dynamics.generation!==this.dynamicsGeneration||appearance.generation!==this.appearanceGeneration;
   if(changed){this.profile=profile;this.lastTime=-Infinity;}
   if(command)this.lastTime=-Infinity;
   // Live display keeps its bounded 30 Hz background. Offline export explicitly
   // supplies zero to render every simulated frame without changing live pacing.
   const interval=Number.isFinite(frameInterval)?Math.max(0,Math.min(.1,frameInterval)):1/30;
   if(!changed&&time-this.lastTime<interval-.00025)return;
   this.timeline=dynamics.archive;
   const state=dynamics.view(),o={archive:state.archive,glow:1,bloom:1,material:1,dynamicLight:1,cleanSurface:1,glassAppearance:appearance,interactionGain:state.interactionGain};
   this.lastStats=this.painter.render(B.geometry.layout(state.geometryTime,o),{...o,lightTime:state.lightTime,framebuffer:this.framebuffer});
   const gl=this.gl;gl.activeTexture(gl.TEXTURE3);gl.bindTexture(gl.TEXTURE_2D,this.texture);
   if(this.copy){
    gl.pixelStorei(gl.UNPACK_FLIP_Y_WEBGL,true);gl.pixelStorei(gl.UNPACK_COLORSPACE_CONVERSION_WEBGL,gl.NONE);
    gl.texSubImage2D(gl.TEXTURE_2D,0,0,0,gl.RGBA,gl.UNSIGNED_BYTE,this.canvas);this.uploads++;
    gl.pixelStorei(gl.UNPACK_FLIP_Y_WEBGL,false);gl.pixelStorei(gl.UNPACK_COLORSPACE_CONVERSION_WEBGL,gl.BROWSER_DEFAULT_WEBGL);
   }
   gl.generateMipmap(gl.TEXTURE_2D);
   gl.activeTexture(gl.TEXTURE0);this.lastTime=time;this.draws++;
   this.dynamicsId=dynamics.id;this.dynamicsGeneration=dynamics.generation;this.appearanceGeneration=appearance.generation;
  }
 }
 NS.GlassBackground=GlassBackground;
})(window.RA);
