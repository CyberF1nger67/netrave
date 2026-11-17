import { describe, expect, it } from 'vitest';
import { KEYSYM_SHIFT_L, latin1Payload, needsShift, typingPlan } from './keyboardText';

// Expands a plan into "down/up" pairs for easy assertions.
const press = (keysym: number) => [{ keysym, down: true }, { keysym, down: false }];
const shifted = (keysym: number) => [
  { keysym: KEYSYM_SHIFT_L, down: true }, { keysym, down: true }, { keysym, down: false }, { keysym: KEYSYM_SHIFT_L, down: false },
];

describe('typingPlan', () => {
  it.each([
    ['_', 0x5f], ['-', 0x2d], ['@', 0x40], ['#', 0x23], ['$', 0x24], ['%', 0x25], ['^', 0x5e], ['&', 0x26],
    ['*', 0x2a], ['(', 0x28], [')', 0x29], ['[', 0x5b], [']', 0x5d], ['{', 0x7b], ['}', 0x7d], ['|', 0x7c],
    ['\\', 0x5c], ['/', 0x2f], [':', 0x3a], [';', 0x3b], ["'", 0x27], ['"', 0x22], ['~', 0x7e], ['!', 0x21],
    ['=', 0x3d], ['+', 0x2b],
  ])('sends the exact keysym for %s', (char, keysym) => {
    const { steps, skipped } = typingPlan(char);
    expect(skipped).toBe(0);
    const expected = needsShift(char) ? shifted(keysym) : press(keysym);
    expect(steps).toEqual(expected);
  });

  it('wraps underscore in Shift so the guest receives underscore, not minus', () => {
    expect(typingPlan('_').steps).toEqual(shifted(0x5f));
    expect(typingPlan('_').steps).not.toEqual(press(0x2d));
  });

  it('uses Shift for uppercase letters and not for lowercase', () => {
    expect(typingPlan('A').steps).toEqual(shifted(0x41));
    expect(typingPlan('a').steps).toEqual(press(0x61));
  });

  it('keeps a command with mixed characters exactly in order', () => {
    const { steps } = typingPlan('export TEST_VAR="hello_world"');
    const typed = steps.filter((step) => step.down && step.keysym !== KEYSYM_SHIFT_L).map((step) => String.fromCharCode(step.keysym));
    expect(typed.join('')).toBe('export TEST_VAR="hello_world"');
  });

  it('maps newlines and tabs to Enter and Tab and drops carriage returns', () => {
    expect(typingPlan('a\r\nb\tc').steps.filter((s) => s.down).map((s) => s.keysym)).toEqual([0x61, 0xff0d, 0x62, 0xff09, 0x63]);
  });

  it('counts characters that a US-layout guest cannot receive as skipped', () => {
    const { steps, skipped } = typingPlan('aПb');
    expect(skipped).toBe(1);
    expect(steps.filter((s) => s.down).map((s) => s.keysym)).toEqual([0x61, 0x62]);
  });
});

describe('latin1Payload', () => {
  it('passes Latin-1 text through unchanged', () => {
    const { bytes, unsupported } = latin1Payload('a_b@c é');
    expect(unsupported).toBe(0);
    expect(Array.from(bytes)).toEqual(Array.from('a_b@c é', (c) => c.charCodeAt(0)));
  });

  it('reports characters outside Latin-1 instead of hiding them', () => {
    const { unsupported } = latin1Payload('привет');
    expect(unsupported).toBe(6);
  });
});
