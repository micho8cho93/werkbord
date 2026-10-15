// The page a QR code opens (/connect/#wbr1.…). It keeps the pairing, installs the service worker, checks the whole way
// to your computer, and opens the app. Everything it knows is in the address after the "#", which a browser never sends
// to a server, and the pairing is removed from the address as soon as it is read.
import { LinkError, decodePairing } from "./link.js";
import { kvSet } from "./store.js";

const $ = (id) => document.getElementById(id);
const WORDS = {
  offline: ["Your computer is offline", "Werkbord cannot reach your computer. Wake it, check that Werkbord is running, then try again."],
  unavailable: ["Temporarily unavailable", "The connection service is busy or cannot be reached right now. Wait a few minutes, then try again."],
  removed: ["This code has run out", "A code works once, for five minutes. On your computer, open Werkbord, then Settings, then Phones, and scan a new one."],
  invalid: ["This link is not complete", "On your computer, open Werkbord, then Settings, then Phones, and scan a new code."],
  unsupported: ["This browser cannot do it", "Open the link in Safari on iPhone or Chrome on Android."],
};

function show(reason, retry) {
  const [title, text] = WORDS[reason] || WORDS.unavailable;
  document.body.dataset.state = "problem";
  $("title").textContent = title;
  $("text").textContent = text;
  $("retry").hidden = !retry;
}

async function ask(worker, message) {
  return new Promise((resolve) => {
    const ch = new MessageChannel();
    const timer = setTimeout(() => resolve({ ok: false, reason: "unavailable" }), 20_000);
    ch.port1.onmessage = (ev) => {
      clearTimeout(timer);
      resolve(ev.data);
    };
    worker.postMessage(message, [ch.port2]);
  });
}

async function connect(lane) {
  document.body.dataset.state = "working";
  $("title").textContent = "Connecting to your computer";
  $("text").textContent = "This takes a few seconds.";
  const reg = await navigator.serviceWorker.register("/sw.js", { scope: "/" });
  await navigator.serviceWorker.ready;
  return ask(reg.active || (await navigator.serviceWorker.ready).active, { type: "reset", lane });
}

async function main() {
  if (!("serviceWorker" in navigator) || !window.indexedDB || !window.crypto?.subtle) return show("unsupported", false);
  const raw = location.hash;
  let pairing;
  try {
    pairing = decodePairing(raw);
  } catch (e) {
    return show(e instanceof LinkError ? e.reason : "invalid", false);
  }
  history.replaceState(null, "", location.pathname); // the pairing is a secret: not in the address bar, history or a screenshot
  try {
    await kvSet(pairing.lane === "team" ? "pairing:team" : "pairing", raw.replace(/^#/, ""));
  } catch {
    return show("unsupported", false);
  }
  const attempt = async () => {
    let result;
    try {
      result = await connect(pairing.lane);
    } catch {
      return show("unsupported", false);
    }
    if (!result.ok) return show(result.reason, result.reason === "offline" || result.reason === "unavailable");
    location.replace(pairing.lane === "team" ? "/team/#/mywork" : pairing.code ? "/#enroll=" + encodeURIComponent(pairing.code) : "/");
  };
  $("retry").addEventListener("click", attempt);
  await attempt();
}

main();
