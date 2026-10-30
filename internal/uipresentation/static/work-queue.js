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
  root.WerkbordWork = Object.assign(root.WerkbordWork || {}, { group });
})(typeof globalThis !== 'undefined' ? globalThis : window);
