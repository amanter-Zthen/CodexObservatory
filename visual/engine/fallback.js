// A readable, continuously updated display when GPU setup or drawing fails.
(function(NS){
 'use strict';
 class CanvasFallback{
  constructor(canvas,reason){
   const replacement=document.createElement('canvas');
   replacement.id=canvas.id;replacement.style.cssText=canvas.style.cssText;
   replacement.width=canvas.width;replacement.height=canvas.height;
   canvas.replaceWith(replacement);this.canvas=replacement;
   this.ctx=replacement.getContext('2d');this.fallback=true;this.reason=String(reason);
   this.fmt={hdr:false};this.skipped=this.submitted=0;
  }
  ready(){return true;}
  uploadAtlas(atlas){atlas.dirty.length=0;}
  render(s,labels,parts,o){
   const c=this.ctx,z=o.scale,m=NS.MARGIN;c.setTransform(z,0,0,z,m*z,m*z);
   c.clearRect(-m,-m,this.canvas.width/z,this.canvas.height/z);
   if(s.stage.hidden)return;
   c.fillStyle='#f3ecde';c.beginPath();c.roundRect(0,0,s.W,s.H,14);c.fill();
   c.strokeStyle='#b7ae9c';c.stroke();
   c.fillStyle='#3a3a36';c.font='bold 9px "Segoe UI"';c.fillText('CODEX OBSERVATORY',18,17);
   c.font='9px "Microsoft YaHei UI"';c.fillStyle='#81745d';c.fillText('简化显示 · 数据继续更新',18,s.H-10);
   c.font='bold 24px "Segoe UI"';c.fillText(s.quota.text,27,s.H*.5+8);
   for(const r of s.rows){
    c.globalAlpha=Math.max(0,r.reveal*(1-r.dissolve));
    c.fillStyle=r.color==='red'?'#a83035':r.color==='green'?'#226e54':'#99701e';
    const l=NS.rowLayout(labels.atlas,r,s.W);
    c.beginPath();c.arc(l.orbX,r.y,2.5,0,Math.PI*2);c.fill();
    c.font='bold '+l.name.size+'px "Segoe UI"';c.fillText(l.name.text,l.modelX,l.baseline);
    if(r.rateOp>.5){c.font='bold '+l.rateSize+'px "Segoe UI"';c.textAlign='right';c.fillText(r.rateText+'/s',l.rateRight,l.baseline);c.textAlign='left';}
   }
   c.globalAlpha=1;this.submitted++;
  }
 }
 NS.CanvasFallback=CanvasFallback;
})(window.RA);
