// Product-neutral pointer/keyboard transport. Each board owns drop permissions and actions.
let gesture = null;
let moved = false;
function clear() {
  document.querySelectorAll('[data-board-column].pointer-over').forEach(el => el.classList.remove('pointer-over'));
  gesture?.card.classList.remove('pointer-dragging');
  gesture = null;
}
function columnAt(x, y) { return document.elementFromPoint(x, y)?.closest('[data-board-column]'); }
function dispatch(card, column) {
  if (!column || column.closest('[data-board]') !== card.closest('[data-board]')) return;
  column.dispatchEvent(new CustomEvent('werkbord-board-drop', { bubbles: true, detail: { id: card.dataset.boardItem, state: column.dataset.boardColumn } }));
}
document.addEventListener('pointerdown', event => {
  const handle = event.target.closest('[data-drag-handle]');
  const card = handle?.closest('[data-board-item]');
  if (!card || card.getAttribute('aria-busy') === 'true' || event.button !== 0) return;
  event.preventDefault();
  moved = false;
  gesture = { card, handle, pointer: event.pointerId, x: event.clientX, y: event.clientY };
  handle.setPointerCapture(event.pointerId);
});
document.addEventListener('pointermove', event => {
  if (!gesture || event.pointerId !== gesture.pointer) return;
  if (Math.hypot(event.clientX - gesture.x, event.clientY - gesture.y) < 5 && !moved) return;
  moved = true;
  gesture.card.classList.add('pointer-dragging');
  document.querySelectorAll('[data-board-column].pointer-over').forEach(el => el.classList.remove('pointer-over'));
  const column = columnAt(event.clientX, event.clientY);
  if (column?.closest('[data-board]') === gesture.card.closest('[data-board]')) column.classList.add('pointer-over');
  const board = gesture.card.closest('[data-board]');
  const rect = board.getBoundingClientRect();
  if (event.clientX > rect.right - 40) board.scrollLeft += 16;
  if (event.clientX < rect.left + 40) board.scrollLeft -= 16;
});
document.addEventListener('pointerup', event => {
  if (!gesture || event.pointerId !== gesture.pointer) return;
  const card = gesture.card, column = columnAt(event.clientX, event.clientY);
  const drop = moved;
  clear();
  if (drop) dispatch(card, column);
});
document.addEventListener('pointercancel', clear);
document.addEventListener('keydown', event => {
  const handle = event.target.closest('[data-drag-handle]');
  if (!handle || !['ArrowLeft', 'ArrowRight', 'Escape'].includes(event.key)) return;
  if (event.key === 'Escape') { clear(); return; }
  const card = handle.closest('[data-board-item]');
  if (!card || card.getAttribute('aria-busy') === 'true') return;
  event.preventDefault();
  const columns = [...card.closest('[data-board]').querySelectorAll('[data-board-column]')];
  const at = columns.indexOf(card.closest('[data-board-column]'));
  dispatch(card, columns[at + (event.key === 'ArrowRight' ? 1 : -1)]);
});
document.addEventListener('click', event => {
  if (event.target.closest('[data-drag-handle]')) { event.preventDefault(); event.stopPropagation(); }
}, true);
