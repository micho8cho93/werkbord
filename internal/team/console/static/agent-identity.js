// The agent mark, avatar and assignment group for the Team console, drawn to the same markup as AgentMark.svelte, Avatar.svelte and
// AssigneeChip.svelte so identity.css styles both alike (identity-parity.test.ts holds them to it). The state comes from
// identity.js; a peer's freshness is the server's word (Board.agents[].fresh), never worked out here from the clock.
//
// Read only: nothing here opens a runner or acts for anyone. Avatars carry a role or an assignment, never whether a person is around.
'use strict';
(function (root) {
  const SVG = 'http://www.w3.org/2000/svg';
  function el(tag, attrs, children, ns) {
    const node = ns ? document.createElementNS(ns, tag) : document.createElement(tag);
    for (const [k, v] of Object.entries(attrs || {})) if (v !== null && v !== undefined && v !== false) node.setAttribute(k, v === true ? '' : String(v));
    for (const c of children || []) node.append(c);
    return node;
  }

  // ref is one entry of Board.agents (or a summary AgentRef): { id, state, observedAt, fresh }. Without one it is the bare mark of id.
  function agentMark(ref, opts) {
    const o = opts || {};
    const marks = root.WerkbordAgentMarks;
    const id = ref && ref.id || o.agentId;
    const name = marks.agentName(id, o.name);
    const mark = id ? marks.MARKS[id] : undefined;
    const view = ref && ref.state !== undefined ? root.WerkbordIdentity.resolveAgentState({ state: ref.state, fresh: ref.fresh, observedAt: ref.observedAt, now: o.now === undefined ? Date.now() : o.now, agentName: name }) : null;
    const tile = el('span', { class: mark ? 'wb-agent-tile' : 'wb-agent-tile letter' }, [mark
      ? el('svg', { viewBox: '0 0 24 24', 'aria-hidden': 'true' }, [el('path', { d: mark.path, fill: mark.colour }, [], SVG)], SVG)
      : document.createTextNode(name.slice(0, 1).toUpperCase())]);
    return el('span', { class: 'wb-agent', 'data-size': o.size || 'sm', 'data-ring': view && view.ring, role: o.decorative ? null : 'img', 'aria-label': o.decorative ? null : (view ? view.label : name), 'aria-hidden': o.decorative ? 'true' : null }, [tile]);
  }

  function avatar(id, name, self, size) {
    return el('span', { class: 'wb-avatar', 'data-size': size || 'md', 'data-tone': root.WerkbordIdentity.avatarTone(id), 'data-self': self ? true : null, role: 'img', 'aria-label': self ? name + ' (you)' : name }, [document.createTextNode(root.WerkbordIdentity.initials(name))]);
  }

  // The agent first, then the person who holds the work. holder is { id, name, self }; role is "Held by", "Assigned to" or "Review".
  function assignment(ref, holder, role, opts) {
    const o = opts || {};
    if (!ref && !holder) return null;
    const marks = root.WerkbordAgentMarks;
    const groups = [];
    if (ref) {
      const name = marks.agentName(ref.id, o.name);
      const view = ref.state !== undefined ? root.WerkbordIdentity.resolveAgentState({ state: ref.state, fresh: ref.fresh, observedAt: ref.observedAt, now: o.now === undefined ? Date.now() : o.now, agentName: name }) : null;
      groups.push(el('span', { class: 'wb-assign-group' }, [agentMark(ref, { name: o.name, now: o.now, decorative: true }), el('span', {}, [document.createTextNode(name + (view ? ' ' + view.text : ''))])]));
    }
    if (holder) groups.push(el('span', { class: 'wb-assign-group' }, [avatar(holder.id, holder.name, holder.self, 'sm'), el('span', {}, [document.createTextNode((role || 'Held by') + ' ' + (holder.self ? 'you' : holder.name))])]));
    return el('span', { class: 'wb-assign' }, groups);
  }

  root.WerkbordTeamIdentity = { agentMark, avatar, assignment };
})(globalThis);
