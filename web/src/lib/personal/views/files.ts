// Files for the files view stay on the device that added them, in this browser's own storage. A workspace saves the entry
// (a name, tags, notes); the file itself is never sent anywhere, so it cannot reach another computer or the controller.

export type PreviewKind = 'pdf' | 'image' | 'video' | 'audio' | 'markdown' | 'text' | 'none';
export const maxFileBytes = 150 * 1024 * 1024;

const textTypes = new Set(['application/json', 'application/xml', 'application/x-yaml', 'application/yaml', 'application/toml', 'application/x-sh']);
const extensionTypes: Record<string, string> = {
  pdf: 'application/pdf', png: 'image/png', jpg: 'image/jpeg', jpeg: 'image/jpeg', gif: 'image/gif', webp: 'image/webp', avif: 'image/avif', svg: 'image/svg+xml',
  mp4: 'video/mp4', m4v: 'video/mp4', webm: 'video/webm', mov: 'video/quicktime', mp3: 'audio/mpeg', m4a: 'audio/mp4', wav: 'audio/wav', ogg: 'audio/ogg',
  md: 'text/markdown', markdown: 'text/markdown', txt: 'text/plain', log: 'text/plain', csv: 'text/csv', tsv: 'text/tab-separated-values', json: 'application/json',
  yaml: 'application/yaml', yml: 'application/yaml', toml: 'application/toml', xml: 'application/xml', html: 'text/plain', css: 'text/plain', js: 'text/plain', ts: 'text/plain',
  go: 'text/plain', py: 'text/plain', rs: 'text/plain', sh: 'text/plain', sql: 'text/plain',
};

/** The type a file is shown as: the browser's own word for it, or one from its name when the browser has none. */
export function typeOf(name: string, declared: string): string {
  const extension = name.includes('.') ? name.slice(name.lastIndexOf('.') + 1).toLowerCase() : '';
  const known = extensionTypes[extension];
  // HTML is shown as the text it is, never run: a document in a frame would be a page with this app's reach.
  if (extension === 'html' || extension === 'htm' || declared === 'text/html') return 'text/plain';
  if (declared && declared !== 'application/octet-stream') return declared;
  return known ?? 'application/octet-stream';
}
export function previewKind(type: string, name = ''): PreviewKind {
  if (type === 'application/pdf') return 'pdf';
  if (type.startsWith('image/')) return 'image';
  if (type.startsWith('video/')) return 'video';
  if (type.startsWith('audio/')) return 'audio';
  if (type === 'text/markdown' || /\.(md|markdown)$/i.test(name)) return 'markdown';
  if (type.startsWith('text/') || textTypes.has(type)) return 'text';
  return 'none';
}
export function sizeLabel(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(bytes < 10240 ? 1 : 0)} KB`;
  if (bytes < 1024 ** 3) return `${(bytes / 1024 / 1024).toFixed(bytes < 10 * 1024 * 1024 ? 1 : 0)} MB`;
  return `${(bytes / 1024 ** 3).toFixed(1)} GB`;
}
export function stem(name: string): string {
  const cut = name.lastIndexOf('.');
  return (cut > 0 ? name.slice(0, cut) : name).replace(/[_]+/g, ' ').trim() || name;
}

export interface StoredFile { key: string; name: string; type: string; size: number; addedAt: number; blob: Blob }
export type StoredMeta = Omit<StoredFile, 'blob'>;
export interface FileStore {
  available(): boolean;
  put(file: StoredFile): Promise<void>;
  get(key: string): Promise<StoredFile | undefined>;
  remove(key: string): Promise<void>;
  /** What is stored under a workspace, without reading any file. */
  list(prefix: string): Promise<StoredMeta[]>;
}

const DB = 'werkbord-workspace-files';
const STORE = 'files';
function open(): Promise<IDBDatabase> {
  return new Promise((resolve, reject) => {
    const request = indexedDB.open(DB, 1);
    request.onupgradeneeded = () => { request.result.createObjectStore(STORE, { keyPath: 'key' }); };
    request.onsuccess = () => resolve(request.result);
    request.onerror = () => reject(request.error ?? new Error('Storage is unavailable.'));
  });
}
function run<T>(mode: IDBTransactionMode, use: (store: IDBObjectStore) => IDBRequest<T>): Promise<T> {
  return open().then((db) => new Promise<T>((resolve, reject) => {
    const tx = db.transaction(STORE, mode);
    const request = use(tx.objectStore(STORE));
    tx.oncomplete = () => { db.close(); resolve(request.result); };
    tx.onerror = tx.onabort = () => { db.close(); reject(tx.error ?? new Error('Storage failed.')); };
  }));
}
export const browserFiles: FileStore = {
  available: () => typeof indexedDB !== 'undefined',
  put: (file) => run('readwrite', (s) => s.put(file)).then(() => undefined),
  get: (key) => run<StoredFile | undefined>('readonly', (s) => s.get(key)),
  remove: (key) => run('readwrite', (s) => s.delete(key)).then(() => undefined),
  async list(prefix) {
    const db = await open();
    return new Promise<StoredMeta[]>((resolve, reject) => {
      const found: StoredMeta[] = [];
      const tx = db.transaction(STORE, 'readonly');
      const request = tx.objectStore(STORE).openCursor(IDBKeyRange.bound(prefix, `${prefix}￿`));
      request.onsuccess = () => {
        const cursor = request.result;
        if (!cursor) return;
        const { key, name, type, size, addedAt } = cursor.value as StoredFile;
        found.push({ key, name, type, size, addedAt });
        cursor.continue();
      };
      tx.oncomplete = () => { db.close(); resolve(found); };
      tx.onerror = tx.onabort = () => { db.close(); reject(tx.error ?? new Error('Storage failed.')); };
    });
  },
};
export const fileKey = (workspaceId: string, recordId: string) => `${workspaceId}/${recordId}`;
