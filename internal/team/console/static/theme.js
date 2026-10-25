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
  window.teamStyle = (() => {
    const styleKey = 'werkbord-team.style';
    let current = 'paper';
    try { const saved = localStorage.getItem(styleKey); if (saved === 'graphite' || saved === 'classic') current = saved; } catch (_) {}
    const show = () => { if (current === 'paper') delete document.documentElement.dataset.style; else document.documentElement.dataset.style = current; };
    show();
    return {
      get current() { return current; },
      set(next) {
        if (next !== 'paper' && next !== 'graphite' && next !== 'classic') return;
        current = next;
        try { if (next === 'paper') localStorage.removeItem(styleKey); else localStorage.setItem(styleKey, next); } catch (_) {}
        show();
        window.dispatchEvent(new Event('teamstylechange'));
      },
    };
  })();
  return {
    get dark() { return dark(); },
    toggle() {
      this.set(dark() ? 'light' : 'dark');
    },
    set(next) {
      // '' is "follow the device": the window's Settings can hand a workspace back to it.
      choice = next === 'light' || next === 'dark' ? next : '';
      try { if (choice) localStorage.setItem(key, choice); else localStorage.removeItem(key); } catch (_) {}
      apply();
    },
  };
})();
