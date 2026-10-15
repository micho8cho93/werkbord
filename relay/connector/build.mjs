// Builds dist/: the connector as plain static files for Cloudflare Pages. No dependencies and no bundler to trust: the
// two scripts are classic scripts (a service worker the app also registers must be one) made by inlining each module
// into its own function scope, which is all this code needs.
//
//   node build.mjs                      -> dist/
//   RELAY_URL=https://… node build.mjs  -> the same, with the Content-Security-Policy limited to that relay
import { cpSync, mkdirSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const dist = resolve(here, "dist");

function bundle(entry) {
  const order = [];
  const seen = new Map();
  const visit = (file) => {
    if (seen.has(file)) return seen.get(file);
    const src = readFileSync(file, "utf8");
    const mod = { file, name: "m" + seen.size, imports: [], exports: [], body: "" };
    seen.set(file, mod);
    const body = src.replace(/^import\s+\{([^}]*)\}\s+from\s+"([^"]+)";?\s*$/gm, (_, names, from) => {
      const dep = visit(resolve(dirname(file), from));
      mod.imports.push(`const {${names.trim()}} = ${dep.name};`);
      return "";
    });
    mod.body = body.replace(/^export\s+((?:async\s+)?function\*?|class|const|let)\s+([A-Za-z_$][\w$]*)/gm, (_, kind, name) => {
      mod.exports.push(name);
      return `${kind} ${name}`;
    });
    if (/^\s*import\s/m.test(mod.body) || /^\s*export\s/m.test(mod.body)) throw new Error(`${file}: unsupported import/export form`);
    order.push(mod);
    return mod;
  };
  const main = visit(resolve(here, entry));
  const parts = order.map((m) => (m === main
    ? `(() => {\n${m.imports.join("\n")}\n${m.body}\n})();`
    : `const ${m.name} = (() => {\n${m.imports.join("\n")}\n${m.body}\nreturn { ${m.exports.join(", ")} };\n})();`));
  return `"use strict";\n${parts.join("\n")}\n`;
}

rmSync(dist, { recursive: true, force: true });
mkdirSync(dist, { recursive: true });
cpSync(resolve(here, "static"), dist, { recursive: true });
writeFileSync(resolve(dist, "sw.js"), bundle("src/sw.js"));
writeFileSync(resolve(dist, "connect/connect.js"), bundle("src/connect.js"));

// Headers. The relay is the only place the worker connects to. Without RELAY_URL any https relay is allowed, so a
// page built once keeps working if the relay moves; set it to pin the policy.
let connect = "https: wss:";
if (process.env.RELAY_URL) {
  const u = new URL(process.env.RELAY_URL);
  connect = `${u.origin} ${u.origin.replace(/^http/, "ws")}`;
}
writeFileSync(
  resolve(dist, "_headers"),
  `/*
  X-Content-Type-Options: nosniff
  Referrer-Policy: no-referrer
  X-Frame-Options: DENY

/sw.js
  Cache-Control: no-cache
  Service-Worker-Allowed: /
  Content-Security-Policy: default-src 'none'; connect-src ${connect}

/connect/*
  Cache-Control: no-store
  Content-Security-Policy: default-src 'none'; script-src 'self'; style-src 'self'; connect-src 'self'; img-src 'self'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'
`,
);
console.log("built", dist);
