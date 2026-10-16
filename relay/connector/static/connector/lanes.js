// Connector-owned navigation. Product routes, credentials and authority stay separate.
"use strict";
(() => {
 if (document.getElementById("phone-lanes")) return;
 const team = location.pathname === "/team" || location.pathname.startsWith("/team/");
 const preference = "werkbord.phone.lane";
 const sections = new Set(["control", "schedule", "projects", "mywork", "agents", "analytics", "reviews", "repositories", "runners", "settings", "help"]);
 const narrow = matchMedia("(max-width: 899px)");
 let selected = "individual";
 try { selected = localStorage.getItem(preference) === "team" ? "team" : "individual"; } catch { /* Storage can be disabled. */ }
 const sectionOf = (hash) => {
   const path = hash.replace(/^#\/?/, "").split("?")[0];
   if (sections.has(path)) return path;
   if (path.startsWith("p/")) return path.split("/")[2] === "git" ? "repositories" : "projects";
   return "";
 };
 if (team && sectionOf(location.hash)) {
   selected = "team";
   try { localStorage.setItem(preference, selected); } catch { /* Storage can be disabled. */ }
 }
 const destination = (lane, section) => (lane === "team" ? "/team/" : "/") + "#/" + section;
 const nav = document.createElement("nav");
 nav.id = "phone-lanes"; nav.setAttribute("aria-label", "Workspace scope");
 for (const [lane, label] of [["individual", "Individual"], ["team", "Team"]]) {
   const a = document.createElement("a"); a.textContent = label; a.dataset.lane = lane;
   if ((team ? "team" : "individual") === lane) a.setAttribute("aria-current", "page");
   a.addEventListener("click", () => {
     selected = lane;
     try { localStorage.setItem(preference, lane); } catch { /* Keep the current visit usable. */ }
   });
   nav.append(a);
 }
 document.body.prepend(nav);
 // Team and connection failures have no Individual app navigation of their own.
 if (team || document.body.dataset.state === "problem") {
   const primary = document.createElement("nav"); primary.id = "phone-primary"; primary.setAttribute("aria-label", "Primary");
   for (const [label, href] of [["Control Deck", "/#/deck"], ["Assistant", "/?phone-assistant=1#/deck"]]) {
     const a = document.createElement("a"); a.textContent = label; a.href = href; primary.append(a);
   }
   document.body.append(primary);
 }
 let openingAssistant = new URLSearchParams(location.search).has("phone-assistant");
 function update() {
   const section = sectionOf(location.hash);
   // Home belongs to the deck, regardless of the scope last selected inside a section.
   if (team && !section && narrow.matches) { location.replace("/#/deck"); return; }
   const assistant = document.querySelector('[data-testid="nav-assistant"]');
   if (openingAssistant && assistant) {
     openingAssistant = false;
     const url = new URL(location.href); url.searchParams.delete("phone-assistant");
     history.replaceState(null, "", url);
     assistant.click();
   }
   const visible = narrow.matches && !!section && assistant?.getAttribute("aria-expanded") !== "true";
   nav.hidden = !visible;
   document.documentElement.classList.toggle("phone-lanes", visible);
   for (const a of nav.children) a.href = destination(a.dataset.lane, section || "projects");
 }
 // Deck keys follow the remembered scope. Explicit scope links select their own lane.
 document.addEventListener("click", (event) => {
   if (event.defaultPrevented || event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey || !narrow.matches) return;
   const a = event.target.closest?.("a[href]");
   if (!a || a.closest("#phone-lanes") || a.target || a.hasAttribute("download")) return;
   // Workflow execution belongs to Individual even when section navigation last used Team.
   // This selects a destination only; credentials and backend permissions stay separate.
   if (a.closest('[data-workspace="individual"]')) {
     selected = "individual";
     try { localStorage.setItem(preference, selected); } catch { /* Keep this visit usable. */ }
     update();
     return;
   }
   const url = new URL(a.href, location.href);
   if (url.origin !== location.origin || url.pathname !== "/") return;
   const section = sectionOf(url.hash);
   if (section && selected === "team") {
     event.preventDefault(); event.stopPropagation(); location.assign(destination("team", section));
   }
 }, true);
 window.addEventListener("hashchange", update);
 narrow.addEventListener("change", update);
 // Svelte mounts the primary bar after this connector script and toggles the Assistant in place.
 new MutationObserver(update).observe(document.body, { childList: true, subtree: true, attributes: true, attributeFilter: ["aria-expanded"] });
 update();
})();
