// Framework-neutral diagram. Callers supply content and own permissions and data.
export function mountProductMap(root, snapshot, history = []) {
  const node = (tag, text, cls) => { const e = document.createElement(tag); if (text) e.textContent = text; if (cls) e.className = cls; return e; };
  const button = (text, action) => { const b = node('button', text, 'btn small'); b.type = 'button'; b.onclick = action; return b; };
  const shell = node('section', '', 'product-map-viewer');
  const controls = node('div', '', 'pm-controls');
  const search = node('input'); search.type = 'search'; search.placeholder = 'Find a feature'; search.setAttribute('aria-label', 'Find a feature');
  const switcher = node('select'); switcher.setAttribute('aria-label', 'Map presentation');
  for (const mode of ['Canvas', 'List']) { const option = node('option', mode); option.value = mode; switcher.append(option); }
  let zoom = 1;
  const fit = () => { zoom = Math.min(1, Math.max(.2, (viewport.clientWidth - 24) / 960)); canvas.style.zoom = String(zoom); viewport.scrollTo(0, 0); };
  controls.append(search, switcher, button('Zoom in', () => { zoom = Math.min(2, zoom + .2); canvas.style.zoom = String(zoom); }), button('Zoom out', () => { zoom = Math.max(.4, zoom - .2); canvas.style.zoom = String(zoom); }), button('Fit', fit));
  const body = node('div', '', 'pm-body');
  const viewport = node('div', '', 'pm-viewport'); viewport.tabIndex = 0; viewport.setAttribute('aria-label', 'Product map canvas. Scroll to pan, or use the feature list.');
  const canvas = node('div', '', 'pm-canvas');
  const list = node('ul', '', 'pm-list'); list.hidden = true;
  const details = node('section', '', 'pm-details'); details.setAttribute('aria-label', 'Feature details');
  const features = snapshot.content.features;
  const select = (f) => {
    details.replaceChildren(node('h3', f.name), node('p', f.inferred ? 'Inferred behaviour — check the supporting source.' : 'Supported by source references', 'pm-meta'), node('p', f.description));
    details.append(node('h4', 'Scenarios'));
    const scenarios = node('ul'); for (const text of f.scenarios ?? []) scenarios.append(node('li', text)); details.append(scenarios);
    details.append(node('h4', 'Supporting source'));
    for (const e of f.evidence) details.append(node('p', `${e.path}:${e.start}–${e.end}`, 'pm-source'));
    details.append(node('h4', 'Relationships'));
    const relations = (snapshot.content.relationships ?? []).filter(r => r.from === f.id || r.to === f.id);
    if (!relations.length) details.append(node('p', 'No recorded relationships.'));
    for (const r of relations) { const other = features.find(x => x.id === (r.from === f.id ? r.to : r.from)); if (other) { const row = node('div'); row.append(button(other.name, () => select(other)), node('p', r.description)); details.append(row); } }
    details.append(node('h4', 'Change history'));
    let prior = '';
    for (const s of [...history].sort((a,b) => a.version-b.version)) { const feature = s.content.features.find(x => x.id === f.id); const key = JSON.stringify(feature); if (feature && prior !== key) details.append(node('p', `Version ${s.version} · ${new Date(s.createdAt).toLocaleString()} · ${s.commit.slice(0, 8)}`, 'pm-meta')); prior = key; }
    for (const b of canvas.querySelectorAll('button')) b.setAttribute('aria-pressed', String(b.dataset.feature === f.id));
  };
  const draw = () => {
    const query = search.value.toLowerCase(); const visible = features.filter(f => `${f.name} ${f.description}`.toLowerCase().includes(query));
    canvas.replaceChildren(); list.replaceChildren();
    const width = 960; const columns = 3; const rowHeight = 115; const height = Math.max(160, Math.ceil(visible.length / columns) * rowHeight);
    canvas.style.width = `${width}px`; canvas.style.height = `${height}px`;
    const positions = new Map(visible.map((f, i) => [f.id, { x: (i % columns) * 320 + 16, y: Math.floor(i / columns) * rowHeight + 20 }]));
    const svg = document.createElementNS('http://www.w3.org/2000/svg', 'svg'); svg.setAttribute('width', String(width)); svg.setAttribute('height', String(height)); svg.setAttribute('aria-hidden', 'true');
    for (const r of snapshot.content.relationships ?? []) { const a = positions.get(r.from); const b = positions.get(r.to); if (!a || !b) continue; const line = document.createElementNS(svg.namespaceURI, 'path'); line.setAttribute('d', `M${a.x+140},${a.y+38} L${b.x+140},${b.y+38}`); svg.append(line); }
    canvas.append(svg);
    for (const f of visible) {
      const position = positions.get(f.id); const b = button(f.name, () => select(f)); b.className = 'pm-feature'; b.dataset.feature = f.id; b.style.left = `${position.x}px`; b.style.top = `${position.y}px`; b.setAttribute('aria-pressed', 'false'); canvas.append(b);
      const item = node('li'); item.append(button(f.name, () => select(f)), node('p', f.description)); list.append(item);
    }
    if (!visible.length) list.append(node('li', 'No matching features.'));
  };
  search.oninput = draw;
  switcher.onchange = () => { list.hidden = switcher.value !== 'List'; viewport.hidden = switcher.value === 'List'; };
  let drag = null;
  const pointerDown = event => { if (event.target.closest('button') || event.button !== 0) return; drag = { x: event.clientX, y: event.clientY, left: viewport.scrollLeft, top: viewport.scrollTop }; viewport.setPointerCapture(event.pointerId); };
  const pointerMove = event => { if (drag) { viewport.scrollLeft = drag.left - event.clientX + drag.x; viewport.scrollTop = drag.top - event.clientY + drag.y; } };
  viewport.addEventListener('pointerdown', pointerDown); viewport.addEventListener('pointermove', pointerMove); viewport.addEventListener('pointerup', () => { drag = null; }); viewport.addEventListener('pointercancel', () => { drag = null; });
  viewport.append(canvas); body.append(viewport, list, details); shell.append(controls, body);
  if (snapshot.content.limitations?.length) { const limitations = node('details'); limitations.append(node('summary', 'Analysis limitations')); for (const l of snapshot.content.limitations ?? []) limitations.append(node('p', l)); shell.append(limitations); }
  root.replaceChildren(shell); draw(); if (features[0]) select(features[0]);
  if (window.matchMedia('(max-width: 899px)').matches) { switcher.value = 'List'; list.hidden = false; viewport.hidden = true; } else fit();
  return () => root.replaceChildren();
}
