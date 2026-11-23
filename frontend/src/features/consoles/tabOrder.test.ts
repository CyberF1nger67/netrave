import { describe, expect, it } from 'vitest';
import { moveEntry } from './tabOrder';

const ids = (list: Array<{ id: number }>) => list.map((item) => item.id);
const sample = () => [{ id: 1 }, { id: 2 }, { id: 3 }, { id: 4 }];

describe('moveEntry', () => {
  it('moves a tab to the right, after the target', () => {
    expect(ids(moveEntry(sample(), 1, 3))).toEqual([2, 3, 1, 4]);
  });

  it('moves a tab to the left, before the target', () => {
    expect(ids(moveEntry(sample(), 4, 2))).toEqual([1, 4, 2, 3]);
  });

  it('keeps the order for the same tab or unknown ids', () => {
    expect(ids(moveEntry(sample(), 2, 2))).toEqual([1, 2, 3, 4]);
    expect(ids(moveEntry(sample(), 9, 2))).toEqual([1, 2, 3, 4]);
    expect(ids(moveEntry(sample(), 1, 9))).toEqual([1, 2, 3, 4]);
  });

  it('does not change the input list', () => {
    const list = sample();
    moveEntry(list, 1, 4);
    expect(ids(list)).toEqual([1, 2, 3, 4]);
  });
});
