import { describe, expect, it } from 'vitest';
import { folderInputProps } from './desktop';

// The "Upload folder" button must open the directory picker on the first click,
// right after the page is mounted (including after a reload).
describe('folder picker', () => {
  it('the directory attribute is set directly on the input properties', () => {
    expect(folderInputProps['webkitdirectory']).toBe('');
    expect(folderInputProps['directory']).toBe('');
  });
});
