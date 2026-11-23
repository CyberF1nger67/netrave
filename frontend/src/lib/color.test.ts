import { describe, expect, it } from 'vitest';
import { withAlpha } from './color';

describe('withAlpha', () => {
  it('converts hex to rgba', () => {
    expect(withAlpha('#6366f1', 0.16)).toBe('rgba(99, 102, 241, 0.16)');
  });
  it('returns a non-hex string unchanged', () => {
    expect(withAlpha('var(--accent)', 0.5)).toBe('var(--accent)');
  });
});
