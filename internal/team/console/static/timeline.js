// Team supplies its own status vocabulary and wire shape; layout comes from the shared presentation library.
'use strict';
const WerkbordTimeline = {
  ...globalThis.WerkbordPlanning,
  groupTasks(tasks, labels, by) {
    return globalThis.WerkbordPlanning.groupTasks(tasks, labels, by, {
      statusOf: (t) => t.status || t.state,
      statusTitles: { backlog: 'Backlog', available: 'Available', in_progress: 'In progress', review: 'In review', done: 'Done' },
      statusOrder: ['backlog', 'available', 'in_progress', 'review', 'done'],
      positionOf: (t) => t.number || t.position || 0,
    });
  },
  dependencyMap(tasks) {
    const out = new Map();
    for (const t of tasks) {
      const deps = t.dependencies || (t.orchestration && t.orchestration.dependencies) || [];
      if (deps.length) out.set(t.id, deps);
    }
    return out;
  },
};
