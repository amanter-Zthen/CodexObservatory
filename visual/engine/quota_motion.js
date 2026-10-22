// G02 quota choreography, driven only by displayed-row presence.
// Center -> dock left -> draw clockwise; erase ring -> return to center.
// Retargeting keeps spring position/velocity, including bursty row changes.
(function(NS){
  'use strict';
  const {Spring,clamp01}=NS.core;
  class QuotaMotion{
    constructor(){
      this.dock=new Spring(0,1.45,1);
      this.head=0;this.tail=0;this.erasing=false;this.phase='center';
    }
    step(hasRows,rowsGone,canDraw,hidden,dt){
      if(hidden){this.head=this.tail=0;this.erasing=false;}
      if(!hasRows&&this.head>0)this.erasing=true;
      if(this.erasing){
        this.tail=Math.min(this.head,this.tail+dt/.55);
        if(this.tail>=this.head){this.head=this.tail=0;this.erasing=false;}
      }
      if(hasRows)this.dock.set(1);
      else if(this.head===0&&rowsGone)this.dock.set(0);
      else if(this.head>0)this.dock.set(1);
      const dock=clamp01(this.dock.step(dt));
      const arrived=dock>=.998&&Math.abs(this.dock.v)<.025;
      // A new task during erasure finishes that erasure before a fresh draw.
      if(hasRows&&arrived&&canDraw&&!this.erasing&&!hidden){
        this.head=Math.min(1,this.head+dt/.90);this.tail=0;
      }
      if(this.erasing)this.phase='erasing';
      else if(hasRows)this.phase=!arrived?'docking':this.head<1?'drawing':'left';
      else this.phase=this.head>0?'erasing':dock>.002?'centering':'center';
      return{dock,arcEnd:this.head,arcStart:this.tail,phase:this.phase};
    }
  }
  NS.QuotaMotion=QuotaMotion;
})(window.RA);

