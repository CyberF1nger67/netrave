import { describe, expect, it } from 'vitest';
import { clipboardAction } from './clipboard';
import type { KeyPress } from './clipboard';

const press = (overrides: Partial<KeyPress>): KeyPress => ({
  type: 'keydown',
  key: '',
  ctrlKey: false,
  metaKey: false,
  shiftKey: false,
  altKey: false,
  ...overrides,
});

describe('clipboardAction', () => {
  it('copies with Ctrl+Shift+C and Ctrl+Insert', () => {
    expect(clipboardAction(press({ key: 'C', ctrlKey: true, shiftKey: true }))).toBe('copy');
    expect(clipboardAction(press({ key: 'Insert', ctrlKey: true }))).toBe('copy');
  });

  it('pastes with Ctrl+Shift+V, Shift+Insert and Ctrl+Shift+Insert', () => {
    expect(clipboardAction(press({ key: 'v', ctrlKey: true, shiftKey: true }))).toBe('paste');
    expect(clipboardAction(press({ key: 'Insert', shiftKey: true }))).toBe('paste');
    expect(clipboardAction(press({ key: 'Insert', ctrlKey: true, shiftKey: true }))).toBe('paste');
  });

  it('copies and pastes with the Command key on macOS', () => {
    expect(clipboardAction(press({ key: 'c', metaKey: true }))).toBe('copy');
    expect(clipboardAction(press({ key: 'v', metaKey: true }))).toBe('paste');
  });

  it('leaves plain Ctrl+C and Ctrl+V for the guest', () => {
    expect(clipboardAction(press({ key: 'c', ctrlKey: true }))).toBeNull();
    expect(clipboardAction(press({ key: 'v', ctrlKey: true }))).toBeNull();
  });

  it('ignores key releases and Alt combinations', () => {
    expect(clipboardAction(press({ type: 'keyup', key: 'c', ctrlKey: true, shiftKey: true }))).toBeNull();
    expect(clipboardAction(press({ key: 'c', ctrlKey: true, shiftKey: true, altKey: true }))).toBeNull();
  });
});
