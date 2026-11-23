import { afterEach, describe, expect, it } from 'vitest';
import { EN_KEYS, setLanguage, t, translateMessage } from './index';

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

  it('translates a message with its parameters', () => {
    setLanguage('en');
    expect(translateMessage('Machine {id}: {detail}', { id: '7', detail: 'boom' })).toBe('Machine 7: boom');
    setLanguage('ru');
    expect(translateMessage('Machine {id}: {detail}', { id: '7', detail: 'boom' })).toBe('Машина 7: boom');
  });

  it('translates nested keys with the same parameters, without recursing', () => {
    setLanguage('en');
    expect(translateMessage('No permission: {permission_key}', { permission_key: 'Edit machines' })).toBe('No permission: Edit machines');
    expect(translateMessage('{name}: {detail_key}', { name: 'lab', detail_key: 'Folder {id} not found', id: '3' })).toBe('lab: Folder 3 not found');
    setLanguage('ru');
    expect(translateMessage('{name}: {detail_key}', { name: 'lab', detail_key: 'Folder {id} not found', id: '3' })).toBe('lab: Папка 3 не найдена');
  });
});
