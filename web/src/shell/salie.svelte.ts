import { defaultSaliePlacements, everywhere, isSaliePlacements, unifiedPlacement, type SaliePlacements } from '../lib/salie-placement';

const KEY = 'werkbord.shell.salie-placement';
function read(): SaliePlacements {
  try {
    const saved: unknown = JSON.parse(localStorage.getItem(KEY) ?? 'null');
    if (isSaliePlacements(saved)) return everywhere(unifiedPlacement(saved));
  } catch { /* Use the defaults when storage is unavailable. */ }
  return defaultSaliePlacements();
}

export const salie = new class {
  placements = $state<SaliePlacements>(read());
  set(placements: SaliePlacements): void {
    if (!isSaliePlacements(placements)) return;
    this.placements = everywhere(unifiedPlacement(placements));
    try { localStorage.setItem(KEY, JSON.stringify(this.placements)); } catch { /* The current window keeps the choice. */ }
  }
}();
