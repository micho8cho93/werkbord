// Apply this device's choice before styles load. Until a choice is made, follow
// the operating system. Team's preference is independent of the individual app.
'use strict';

window.teamTheme = (() => {
  const key = 'werkbord-team.theme';
  const system = matchMedia('(prefers-color-scheme: dark)');
  let choice = '';
  try { const saved = localStorage.getItem(key); if (saved === 'light' || saved === 'dark') choice = saved; } catch (_) {}
  const dark = () => choice ? choice === 'dark' : system.matches;
  function apply() {
    // Resolving the system preference here also keeps the mark and browser chrome
    // in step with CSS, including when the operating system changes while open.
    document.documentElement.dataset.theme = dark() ? 'dark' : 'light';
    const meta = document.querySelector('meta[name="theme-color"]');
    if (meta) meta.content = dark() ? '#0b0b0c' : '#f5f5f2';
    window.dispatchEvent(new Event('teamthemechange'));
  }
  system.addEventListener('change', () => { if (!choice) apply(); });
  window.addEventListener('storage', e => {
    if (e.key !== key && e.key !== null) return;
    choice = e.newValue === 'light' || e.newValue === 'dark' ? e.newValue : '';
    apply();
  });
  apply();
  return {
    get dark() { return dark(); },
    toggle() {
      choice = dark() ? 'light' : 'dark';
      try { localStorage.setItem(key, choice); } catch (_) {}
      apply();
    },
  };
})();
