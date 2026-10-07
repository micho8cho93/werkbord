export function createNativeBridge(namespace: string, messages?: { unavailable?: string; timeout?: string }): (method: string, args?: unknown[], timeoutMs?: number) => Promise<unknown>;
