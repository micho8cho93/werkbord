// Applies the theme the person chose on this device before the first paint, so a dark
// choice never flashes light. No choice means the operating system decides (in CSS).
(function () {
  try {
    var t = localStorage.getItem('werkbord.theme');
    if (t === 'light' || t === 'dark') document.documentElement.dataset.theme = t;
  } catch {
    // Storage unavailable: follow the operating system.
  }
})();
