import { callDesktop } from './desktop';

/** What the desktop app reports about this computer's connection (internal/launcher/device_setup.go). */
export interface DeviceJob { running: boolean; action?: 'connect' | 'local' | 'bind'; phase?: 'working' | 'done' | 'failed'; error?: string }
export interface DeviceStatus {
  mode: 'local' | 'remote';
  controller?: string;
  runner: boolean;
  runnerPaired: boolean;
  clientPaired: boolean;
  projects: string[];
  bindings: Record<string, string>;
  job: DeviceJob;
}

export interface DeviceConnectRequest { controller: string; name: string; code: string; runner: boolean; runnerCode: string }

/** The Go side sends null for an empty list; a page must not trip over it. */
export function normalizeDeviceStatus(raw: Partial<DeviceStatus> | null | undefined): DeviceStatus {
  return {
    mode: raw?.mode === 'remote' ? 'remote' : 'local',
    controller: raw?.controller || undefined,
    runner: !!raw?.runner,
    runnerPaired: !!raw?.runnerPaired,
    clientPaired: !!raw?.clientPaired,
    projects: raw?.projects ?? [],
    bindings: raw?.bindings ?? {},
    job: { running: !!raw?.job?.running, action: raw?.job?.action, phase: raw?.job?.phase, error: raw?.job?.error },
  };
}

export const deviceSetup = {
  status: async () => normalizeDeviceStatus(await callDesktop<DeviceStatus>('DeviceStatus', [], 10000)),
  // The app asks the person in its own dialog first, so these wait as long as the person takes.
  connect: (req: DeviceConnectRequest) => callDesktop<DeviceJob>('DeviceConnect', [req], 0),
  useLocal: () => callDesktop<DeviceJob>('DeviceLocal', [], 0),
  bind: (project: string) => callDesktop<DeviceJob>('DeviceBind', [{ project }], 0),
};

/** One sentence on where this computer's board comes from. */
export function connectionSummary(s: DeviceStatus): string {
  if (s.mode !== 'remote') return 'This computer uses its own Werkbord and keeps its own board.';
  const where = s.controller ?? 'another computer';
  return s.runner
    ? `Connected to ${where}. This computer controls that Werkbord and also runs agents for it.`
    : `Connected to ${where}. This computer controls that Werkbord; agents run on its runners.`;
}

/** Whether a pairing code looks complete enough to send; the controller makes the real decision. */
export function desktopCodeReady(code: string): boolean {
  return /^[0-9a-fA-F]{64}$/.test(code.trim());
}

/** Whether the form can be submitted, and what is missing in words if it cannot. */
export function connectProblem(r: DeviceConnectRequest, hasSavedPairing: boolean): string {
  if (!r.controller.trim()) return 'Enter the controller address shown on the other computer.';
  if (!r.code.trim() && !hasSavedPairing) return 'Enter the desktop pairing code from the other computer.';
  if (r.code.trim() && !desktopCodeReady(r.code)) return 'The desktop pairing code is 64 letters and digits. Copy it again from the other computer.';
  if (r.runner && !r.runnerCode.trim()) return 'Enter the separate runner pairing code, or turn off running agents on this computer.';
  return '';
}

/** Shown for a clone that is bound, or that will be prepared from the project's Git remote, or that cannot be used yet. */
export function bindingLabel(path: string | undefined): string {
  return path ? path : 'No local clone chosen. Runs need one, or cloning allowed at both ends.';
}
