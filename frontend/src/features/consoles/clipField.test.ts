import { describe, expect, it } from 'vitest';
import { createClipField, replaceClipText } from './clipField';

describe('createClipField', () => {
  it('turns off browser text rewriting and spellcheck', () => {
    const field = createClipField('hint', 'x');
    expect(field.tagName).toBe('TEXTAREA');
    expect(field.getAttribute('autocorrect')).toBe('off');
    expect(field.getAttribute('autocapitalize')).toBe('off');
    expect(field.getAttribute('autocomplete')).toBe('off');
    expect(field.spellcheck).toBe(false);
    expect(field.placeholder).toBe('hint');
  });
});

describe('replaceClipText', () => {
  it('sets the text and puts the caret at its end', () => {
    const field = createClipField('', '');
    document.body.append(field);
    replaceClipText(field, 'ls -la /var/log');
    expect(field.value).toBe('ls -la /var/log');
    expect(field.selectionStart).toBe(15);
    expect(field.selectionEnd).toBe(15);
    field.remove();
  });

  it('clears the field', () => {
    const field = createClipField('', '');
    field.value = 'old';
    replaceClipText(field, '');
    expect(field.value).toBe('');
    expect(field.selectionStart).toBe(0);
  });
});
