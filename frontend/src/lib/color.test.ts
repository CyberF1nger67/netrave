import { describe, expect, it } from 'vitest';
import { withAlpha } from './color';

describe('withAlpha', () => {
  it('переводит hex в rgba', () => {
    expect(withAlpha('#6366f1', 0.16)).toBe('rgba(99, 102, 241, 0.16)');
  });
  it('возвращает нехексовую строку без изменений', () => {
    expect(withAlpha('var(--accent)', 0.5)).toBe('var(--accent)');
  });
});
