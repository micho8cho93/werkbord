// Pure structural checks complement the accessible-name set comparison.
function duplicateHeadings(headings) {
  const names = headings.map(h => h.replace(/\s+/g, ' ').trim()).filter(Boolean);
  return [...new Set(names.filter((h, i) => names.indexOf(h) !== i))];
}
function layoutDrift(window, web, tolerance = 2) {
  const gaps = [];
  for (const region of ['sidebar', 'page']) {
    for (const dimension of ['x', 'y', 'width', 'height']) {
      if (!window[region] || !web[region]) { gaps.push(region + ' missing'); break; }
      if (Math.abs(window[region][dimension] - web[region][dimension]) > tolerance) gaps.push(`${region}.${dimension}: window ${window[region][dimension]}, web ${web[region][dimension]}`);
    }
  }
  return gaps;
}
module.exports = { duplicateHeadings, layoutDrift };
