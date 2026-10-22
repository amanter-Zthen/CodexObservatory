// Warm champagne background: preserve each status ink's hue, then separate
// body and light in OKLCH. Core: L + .035, C * 1.04. Halo: L + .115, C * 1.10.
// Values are converted to sRGB once and retained here as stable art controls.
(function(NS){
 'use strict';
 const swatch=(ink,core,glow)=>Object.freeze({
  ink:Object.freeze(ink),core:Object.freeze(core),glow:Object.freeze(glow)
 });
 NS.statusPalette=Object.freeze({
  green:swatch([.12,.43,.33],[.15205,.47258,.36657],[.23634,.57063,.45387]),
  red:swatch([.66,.19,.21],[.71652,.22249,.23995],[.84078,.30714,.31673]),
  yellow:swatch([.60,.42,.10],[.64837,.45850,.12879],[.75700,.55037,.21059])
 });
})(window.RA);
