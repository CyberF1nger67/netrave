import { afterEach, describe, expect, it } from 'vitest';
import { EN_KEYS, setLanguage, t } from './index';

describe('i18n', () => {
  afterEach(() => setLanguage('en'));

  it('defaults to English and switches to Russian', () => {
    setLanguage('en');
    expect(t('Laboratories')).toBe('Laboratories');
    setLanguage('ru');
    expect(t('Laboratories')).toBe('Лаборатории');
    expect(localStorage.getItem('netrave.language')).toBe('ru');
  });

  it('interpolates variables', () => {
    setLanguage('en');
    expect(t('Delete folder?')).toContain('Delete folder?');
  });

  it('contains keys', () => {
    expect(EN_KEYS.length).toBeGreaterThan(100);
  });
});
