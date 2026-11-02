// Presentation only: these groups are derived, never persisted as task state.
(function(root) {
  function group(item, now = Date.now()) {
    if (item.done) return 'done';
    if (item.active || item.attention || item.priority === 'high') return 'now';
    if (item.blocked) return 'later';
    const start = item.scheduledAt || item.start;
    if (start && new Date(/^\d{4}-\d{2}-\d{2}$/.test(start) ? start+'T00:00:00' : start).getTime() > now) return 'later';
    if (item.priority === 'low') return 'later';
    return 'next';
  }
  // A Team plan from one member's side: whose each step is, what it waits for, and which of the member's own steps they can start
  // now. Guidance only. Team starts nothing: each start still goes through the member's own preview, approval and runner.
  // Dependencies block (they must be done); the order of the plan only informs ("comes after step 2"), as Team enforces dependencies.
  function planSteps(steps, me, nameOf) {
    const live = steps.filter(k => !k.archived);
    const byId = new Map(live.map(k => [k.id, k]));
    const who = k => !k.assigneeId ? 'nobody yet' : k.assigneeId === me ? 'you' : (nameOf(k.assigneeId) || 'a former member');
    let prevOpen = null, next = null;
    const out = live.map((k, n) => {
      const open = k.status !== 'done';
      const waitsFor = (k.dependencies || []).map(id => byId.get(id)).filter(d => d && d.status !== 'done').map(d => ({ id: d.id, number: live.indexOf(d) + 1, title: d.title, holder: who(d) }));
      const follows = open && prevOpen && !waitsFor.some(w => w.id === prevOpen.id) ? { id: prevOpen.id, number: live.indexOf(prevOpen) + 1, title: prevOpen.title, holder: who(prevOpen) } : null;
      const mine = k.assigneeId === me, agentWork = (k.workMode || 'agent') !== 'human';
      let state;
      if (!open) state = 'done';
      else if (k.status === 'review') state = 'review';
      else if (!k.assigneeId) state = 'unclaimed';
      else if (!mine) state = 'theirs';
      else if (!agentWork) state = 'yours-human';
      else if (k.running) state = 'running';
      else if (waitsFor.length) state = 'waiting';
      else state = 'ready';
      if (open) prevOpen = k;
      const step = { id: k.id, number: n + 1, title: k.title, mine, holder: who(k), state, waitsFor, follows, canStart: state === 'ready' };
      if (step.canStart && !next) next = step.id;
      return step;
    });
    const mineOpen = out.filter(x => x.mine && x.state !== 'done');
    return { steps: out, next, mineOpen: mineOpen.length, mineWaiting: out.filter(x => x.mine && x.state === 'waiting').length, unclaimed: out.filter(x => x.state === 'unclaimed').length };
  }
  root.WerkbordWork = Object.assign(root.WerkbordWork || {}, { group, planSteps });
})(typeof globalThis !== 'undefined' ? globalThis : window);
