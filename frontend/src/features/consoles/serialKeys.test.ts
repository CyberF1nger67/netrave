import { describe, expect, it } from 'vitest';
import { serialSequence } from './serialKeys';

describe('serialSequence', () => {
  it('sends the same sequences as a physical keyboard', () => {
    expect(serialSequence('Enter')).toBe('\r');
    expect(serialSequence('Tab')).toBe('\t');
    expect(serialSequence('Esc')).toBe('\x1b');
    expect(serialSequence('Bksp')).toBe('\x7f');
    expect(serialSequence('ArrowUp')).toBe('');
    expect(serialSequence('↑')).toBe('\x1b[A');
    expect(serialSequence('→')).toBe('\x1b[C');
    expect(serialSequence('PgUp')).toBe('\x1b[5~');
    expect(serialSequence('F5')).toBe('\x1b[15~');
  });

  it('applies Ctrl, Alt and Shift to the next key', () => {
    expect(serialSequence('C', { ctrl: true })).toBe('\x03');
    expect(serialSequence('d', { ctrl: true })).toBe('\x04');
    expect(serialSequence('x', { alt: true })).toBe('\x1bx');
    expect(serialSequence('D', { ctrl: true, alt: true })).toBe('\x1b\x04');
    expect(serialSequence('a', { shift: true })).toBe('A');
    expect(serialSequence('Tab', { shift: true })).toBe('\x1b[Z');
    expect(serialSequence('Space', { ctrl: true })).toBe('\x00');
    expect(serialSequence('[', { ctrl: true })).toBe('\x1b');
  });

  it('ignores unsupported keys instead of sending garbage', () => {
    expect(serialSequence('Control')).toBe('');
    expect(serialSequence('F13')).toBe('');
  });
});
