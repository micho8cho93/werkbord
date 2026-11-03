// Whether the sidebar is folded to a rail of icons. It is the person's choice for this window and is kept for the next one.

const KEY = 'werkbord.shell.sidebar';

function read(): boolean {
  try { return localStorage.getItem(KEY) === 'collapsed'; } catch { return false; }
}

class SidebarState {
  collapsed = $state(read());

  set(collapsed: boolean): void {
    this.collapsed = collapsed;
    try {
      if (collapsed) localStorage.setItem(KEY, 'collapsed');
      else localStorage.removeItem(KEY);
    } catch { /* The choice holds for this window even when it cannot be kept. */ }
  }

  toggle(): void { this.set(!this.collapsed); }
}

export const sidebar = new SidebarState();
