import { describe, expect, it } from 'vitest';
import { keysym, usCharacter } from './VNCView';

const key = (init: KeyboardEventInit) => new KeyboardEvent('keydown', init);

describe('keys from other host layouts', () => {
  it('sends the US letter of the physical key for a Cyrillic letter', () => {
    // Russian layout: the key in the S position produces «ы».
    expect(keysym(key({ key: 'ы', code: 'KeyS' }))).toBe('s'.codePointAt(0));
  });

  it('sends the upper-case US letter when Shift is held', () => {
    expect(keysym(key({ key: 'Ы', code: 'KeyS', shiftKey: true }))).toBe('S'.codePointAt(0));
  });

  it('keeps Latin input and special keys unchanged', () => {
    expect(keysym(key({ key: 'a', code: 'KeyA' }))).toBe('a'.codePointAt(0));
    expect(keysym(key({ key: 'Enter', code: 'Enter' }))).toBe(0xff0d);
  });

  it('maps the symbol keys to US characters', () => {
    expect(usCharacter(key({ key: 'ж', code: 'Semicolon' }))).toBe(';');
    expect(usCharacter(key({ key: 'Ж', code: 'Semicolon', shiftKey: true }))).toBe(':');
  });
});
