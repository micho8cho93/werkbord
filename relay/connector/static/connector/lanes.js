// Connector-owned navigation: product applications keep their own routes and credentials.
"use strict";
if (!document.getElementById("phone-lanes")) {
 const nav = document.createElement("nav"); nav.id = "phone-lanes"; nav.setAttribute("aria-label", "Workspace lane");
 const team = location.pathname === "/team" || location.pathname.startsWith("/team/");
 for (const [label, href, current] of [["Individual", "/", !team], ["Team", "/team/", team]]) {
   const a = document.createElement("a"); a.href = href; a.textContent = label;
   if (current) a.setAttribute("aria-current", "page"); nav.append(a);
 }
 document.body.prepend(nav);
 document.documentElement.classList.add("phone-lanes");
}
