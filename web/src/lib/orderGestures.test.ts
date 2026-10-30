import { describe, expect, it } from 'vitest';
import { positionBetween, reorderedIds, spreadPositions } from './orderGestures';

describe('Work order moves', () => {
  it('puts a plan task next to the neighbour it landed beside, even when the list shown is filtered', () => {
    expect(reorderedIds(['a', 'b', 'c', 'd'], { id: 'c', before: '', after: 'a' })).toEqual(['c', 'a', 'b', 'd']);
    expect(reorderedIds(['a', 'b', 'c', 'd'], { id: 'a', before: 'c', after: 'd' })).toEqual(['b', 'c', 'a', 'd']);
    expect(reorderedIds(['a', 'b', 'c', 'd'], { id: 'a', before: 'd', after: '' })).toEqual(['b', 'c', 'd', 'a']);
    // 'b' is hidden by a filter: dropped directly below 'a', 'c' goes right after 'a', the neighbour the person saw.
    expect(reorderedIds(['a', 'b', 'c'], { id: 'c', before: 'a', after: '' })).toEqual(['a', 'c', 'b']);
    expect(reorderedIds(['a', 'b', 'c'], { id: 'a', before: 'missing', after: 'gone' })).toEqual(['b', 'c', 'a']);
  });
  it('exposes the shared position maths', () => {
    expect(positionBetween(2, 4)).toBe(3);
    expect(positionBetween(2, 2)).toBeNull();
    expect(spreadPositions(2)).toEqual([1, 2]);
  });
});
