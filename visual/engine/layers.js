/* Coarse art groups for this composition preview. Hiding a group leaves its
 * animation/data state intact so comparison does not restart the motion. */
(function(NS){
 'use strict';
 const keys=['background','curves','particles','frame','info','glow'];
 const enabled=Object.fromEntries(keys.map(k=>[k,true]));let soloKey='';
 function paint(){for(const key of keys){document.querySelector(`[data-layer="${key}"]`).checked=enabled[key];document.querySelector(`[data-solo="${key}"]`).setAttribute('aria-pressed',String(soloKey===key));}NS.debug?.redraw();}
 function all(){soloKey='';for(const key of keys)enabled[key]=true;paint();}
 function solo(key){if(!keys.includes(key))return;if(soloKey===key){all();return;}soloKey=key;for(const k of keys)enabled[k]=k===key;paint();}
 for(const key of keys){
  document.querySelector(`[data-layer="${key}"]`).onchange=e=>{soloKey='';enabled[key]=e.target.checked;paint();};
  document.querySelector(`[data-solo="${key}"]`).onclick=()=>solo(key);
 }
 document.getElementById('layers-all').onclick=all;paint();
 NS.layers={keys,enabled,all,solo,get soloKey(){return soloKey;}};
})(window.RA);
