/** The six approved streaming studies. One style per reply, in this order. */
export const MARK_MOTIONS = ['helix', 'wave', 'orbit', 'infinity', 'relay', 'domino'] as const;
export type MarkMotion = (typeof MARK_MOTIONS)[number];
export interface MarkCell { index: number; x: number; y: number; width: number; height: number; angle: number; depth: number; opacity: number }
const TAU = Math.PI * 2;

/** Original cell identities and colours survive every deformation. Units are the mark's SVG coordinates. */
export function markFrame(motion: MarkMotion, seconds: number, amount = 1): MarkCell[] {
  const phase = seconds * TAU / 2;
  const cells: MarkCell[] = [];
  for (let bar = 0; bar < 3; bar++) for (let row = 0; row < 8; row++) for (let col = 0; col < 2; col++) {
    const ox = 36 * (bar + 1) - 12 * Math.floor(row / 2) + 12 * col + 5 - 65;
    const oy = 12 * row + 5 - 47;
    let x = ox, y = oy, width = 10, height = 10, angle = 0, depth = 1;
    let opacity: number;
    if (motion === 'helix') {
      const theta = phase + row * .82 + bar * TAU / 3;
      depth = (Math.cos(theta) + 1) / 2;
      x = 35 * Math.sin(theta) + (col - .5) * 12 * (.65 + .35 * depth) - oy * .1;
      width = height = 6.5 + 3.5 * depth;
      opacity = .4 + .6 * depth;
    } else if (motion === 'wave') {
      const theta = phase - row * .62 - bar * .62;
      x += 12 * Math.sin(theta); y += 5 * Math.sin(theta + .45);
      opacity = .72 + .28 * (.5 + .5 * Math.cos(theta));
    } else if (motion === 'orbit') {
      const theta = -Math.PI / 2 + bar * TAU / 3 + phase;
      depth = (Math.sin(theta) + 1) / 2;
      const scale = .68 + .32 * depth, localX = ox - 36 * (bar - 1);
      angle = .3 * Math.cos(theta);
      x = 40 * Math.cos(theta) + (localX * Math.cos(angle) - oy * Math.sin(angle)) * .8 * scale;
      y = 19 * Math.sin(theta) + (localX * Math.sin(angle) + oy * Math.cos(angle)) * .8 * scale;
      width = height = 8 * scale; opacity = .48 + .52 * depth;
    } else if (motion === 'infinity') {
      const theta = (bar * 8 + row) * TAU / 24 - phase;
      const dx = 52 * Math.cos(theta), dy = 52 * Math.cos(2 * theta), norm = Math.max(1, Math.hypot(dx, dy));
      x = 52 * Math.sin(theta) - (col - .5) * 9 * dy / norm;
      y = 26 * Math.sin(2 * theta) + (col - .5) * 9 * dx / norm;
      depth = (Math.cos(theta) + 1) / 2;
      width = height = 6.8 + 1.5 * depth; opacity = .55 + .45 * depth; angle = Math.atan2(dy, dx);
    } else if (motion === 'relay') {
      const bump = (.5 + .5 * Math.cos(phase - row * .65 - bar * .85)) ** 4;
      x += 5 * bump; y -= 7 * bump; width = height = 8 + 2 * bump; opacity = .48 + .52 * bump;
    } else {
      const theta = phase - row * .5 - bar * .68;
      x += 3 * Math.sin(theta); y += 2 * Math.sin(theta);
      width = 10 * Math.max(.13, Math.abs(Math.cos(theta))); opacity = .48 + .52 * Math.abs(Math.cos(theta));
    }
    cells.push({ index: bar * 16 + row * 2 + col, x: ox + (x - ox) * amount + 65, y: oy + (y - oy) * amount + 47,
      width: 10 + (width - 10) * amount, height: 10 + (height - 10) * amount,
      angle: amount ? angle * amount : 0, depth: 1 + (depth - 1) * amount, opacity: 1 + (opacity - 1) * amount });
  }
  return cells.sort((a, b) => a.depth - b.depth || a.index - b.index);
}
