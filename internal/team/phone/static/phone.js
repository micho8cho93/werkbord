"use strict";
const el = (id) => document.getElementById(id);
let busy = false;
let snapshot = null;
const sections = {
 control: ["Needs you", "Workspace attention reported by your member computer."],
 projects: ["Projects", "Projects in this workspace."],
 mywork: ["My Work", "Your tickets and their reported status."],
 schedule: ["Calendar", "Planned work and scheduled starts reported by your workspace."],
 reviews: ["Reviews", "Tickets waiting for your review."],
 repositories: ["Git", "Reported repository conflicts. Full branches, commits and Git actions are available on your computer."],
 agents: ["Agents", "Reported execution for your tickets. Team does not run agents."],
 runners: ["Runners", "Runner details are not included in Team phone access. Open Individual to see your runners."],
 analytics: ["Analytics", "Analytics are not included in Team phone access. Open the workspace on your computer."],
 settings: ["Settings", "Manage Team settings and phone access on your member computer."],
 help: ["Help", "Team phone access shows a read-only workspace summary. Use Individual for your local work and the Assistant."],
};
const status = (x) => ({todo:"Ready", doing:"In progress", review:"In review", done:"Done"}[x] || x);
const work = (x) => x.title + " · " + x.project + " · " + status(x.status);
const attention = (x) => x.title + (x.detail ? " — " + x.detail : "");
function render() {
 const section = location.hash.replace(/^#\/?/, "").split("?")[0] || "mywork";
 const [title, detail] = sections[section] || ["Section unavailable", "Return to the Control Deck to choose a section."];
 el("section-title").textContent = title; document.title = title + " · Team · Werkbord";
 el("detail").textContent = detail;
 el("items").replaceChildren();
 if (!snapshot) return;
 let items = []; let describe = (x) => x.title;
 switch (section) {
   case "control": items = snapshot.attention || []; describe = attention; break;
   case "projects": items = snapshot.projects || []; describe = (x) => x.name; break;
   case "mywork": items = snapshot.work || []; describe = work; break;
   case "schedule":
     items = [...(snapshot.planned || []).map((x) => ({title: x.title + " · " + x.project + " · " + x.start + " – " + x.end})),
       ...(snapshot.schedule || []).map((x) => ({title: x.title + " · " + x.project + " · " + new Date(x.at).toLocaleString()}))]; break;
   case "reviews": items = (snapshot.attention || []).filter((x) => x.kind === "review"); describe = attention; break;
   case "repositories": items = (snapshot.attention || []).filter((x) => x.kind === "conflict"); describe = attention; break;
   case "agents": items = (snapshot.work || []).filter((x) => x.execution); describe = (x) => work(x) + " · Reported: " + x.execution.replaceAll("_", " ") + (x.report?.stale ? " · Stale report" : ""); break;
   default: return;
 }
 for (const item of items) {
   const li = document.createElement("li"); li.textContent = describe(item); el("items").append(li);
 }
 if (!items.length) { const li = document.createElement("li"); li.textContent = "No items in this summary."; el("items").append(li); }
}
async function refresh() {
 if (busy) return;
 busy = true; el("refresh").disabled = true;
 el("status").textContent = "Refreshing…";
 // Failed or revoked access must not leave an old snapshot looking current.
 snapshot = null; render();
 try {
   const res = await fetch("/team/api/summary", { cache: "no-store" });
   if (!res.ok) throw new Error(res.status === 401 ? "This phone was signed out. Pair it again on your member computer." : "The workspace could not be reached. Try again when your computer is online.");
   const data = await res.json();
   el("name").textContent = data.workspace.name;
   if (data.workspace.state !== "ready") { el("status").textContent = data.workspace.detail || "Connecting to your workspace."; return; }
   snapshot = data;
   el("status").textContent = "Updated " + new Date(data.at).toLocaleTimeString();
   render();
 } catch (error) { el("status").textContent = error.message; }
 finally { busy = false; el("refresh").disabled = false; }
}
el("refresh").addEventListener("click", refresh);
window.addEventListener("hashchange", render);
render();
refresh();
