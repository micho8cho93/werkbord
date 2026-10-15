"use strict";
const el = (id) => document.getElementById(id);
let busy = false;
function rows(id, items, render) {
 el(id).replaceChildren();
 for (const item of items || []) { const li = document.createElement("li"); li.textContent = render(item); el(id).append(li); }
 if (!items?.length) { const li = document.createElement("li"); li.textContent = "Nothing here right now."; el(id).append(li); }
}
async function refresh() {
 if (busy) return;
 busy = true; el("refresh").disabled = true;
 el("status").textContent = "Refreshing…";
 // Clear previous data immediately: failed or revoked access must not look current.
 for (const id of ["attention", "work", "schedule"]) el(id).replaceChildren();
 try {
   const res = await fetch("/team/api/summary", { cache: "no-store" });
   if (!res.ok) throw new Error(res.status === 401 ? "This phone was signed out. Pair it again on your member computer." : "The workspace could not be reached. Try again when your computer is online.");
   const data = await res.json();
   el("name").textContent = data.workspace.name;
   if (data.workspace.state !== "ready") { el("status").textContent = data.workspace.detail || "Connecting to your workspace."; return; }
   el("status").textContent = "Updated " + new Date(data.at).toLocaleTimeString();
   rows("attention", data.attention, (x) => x.title + (x.detail ? " — " + x.detail : ""));
   rows("work", data.work, (x) => x.title + " · " + x.project + " · " + ({todo:"Ready", doing:"In progress", review:"In review", done:"Done"}[x.status] || x.status));
   rows("schedule", data.schedule, (x) => x.title + " · " + new Date(x.at).toLocaleString());
 } catch (error) { el("status").textContent = error.message; }
 finally { busy = false; el("refresh").disabled = false; }
}
el("refresh").addEventListener("click", refresh);
refresh();
