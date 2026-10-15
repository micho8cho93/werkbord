// What the connector keeps on the phone: the pairing, in IndexedDB because a service worker cannot read localStorage.
const DB = "werkbord-connector";
const STORE = "kv";

function db() {
  return new Promise((resolve, reject) => {
    const req = indexedDB.open(DB, 1);
    req.onupgradeneeded = () => req.result.createObjectStore(STORE);
    req.onsuccess = () => resolve(req.result);
    req.onerror = () => reject(req.error);
  });
}

async function run(mode, fn) {
  const d = await db();
  try {
    return await new Promise((resolve, reject) => {
      const tx = d.transaction(STORE, mode);
      const req = fn(tx.objectStore(STORE));
      tx.oncomplete = () => resolve(req.result);
      tx.onerror = () => reject(tx.error);
    });
  } finally {
    d.close();
  }
}

export const kvGet = (key) => run("readonly", (s) => s.get(key));
export const kvSet = (key, value) => run("readwrite", (s) => s.put(value, key));
export const kvDel = (key) => run("readwrite", (s) => s.delete(key));
