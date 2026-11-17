import { describe, expect, it } from 'vitest';
import { folderInputProps } from './desktop';

// The "Upload folder" button must open the directory picker on the first click,
// right after the page is mounted (including after a reload).
describe('выбор папки', () => {
  it('директива каталога задаётся прямо в свойствах поля', () => {
    expect(folderInputProps['webkitdirectory']).toBe('');
    expect(folderInputProps['directory']).toBe('');
  });
});
