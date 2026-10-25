// Applies the theme and the style the person chose on this device before the first paint, so a dark
// choice never flashes light and a chosen style never flashes the default. No theme choice means the
// operating system decides (in CSS); no style choice means Paper.
(function () {
  try {
    var t = localStorage.getItem('werkbord.theme');
    if (t === 'light' || t === 'dark') document.documentElement.dataset.theme = t;
    var s = localStorage.getItem('werkbord.style');
    if (s === 'graphite' || s === 'classic') document.documentElement.dataset.style = s;
  } catch {
    // Storage unavailable: follow the operating system, and use Paper.
  }
})();
