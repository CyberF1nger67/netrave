import { describe, expect, it } from 'vitest';
import { validateIcon } from './validate';

function pngFile(width: number, height: number): File {
  const head = new Uint8Array(24);
  head.set([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]);
  const view = new DataView(head.buffer);
  view.setUint32(16, width);
  view.setUint32(20, height);
  return new File([head], 'icon.png', { type: 'image/png' });
}

function svgFile(text: string): File {
  return new File([text], 'icon.svg', { type: 'image/svg+xml' });
}

describe('validateIcon', () => {
  it('reads PNG dimensions from the header', async () => {
    await expect(validateIcon(pngFile(64, 48))).resolves.toEqual({ width: 64, height: 48 });
  });

  it('rejects PNG outside the allowed size', async () => {
    await expect(validateIcon(pngFile(8, 64))).rejects.toThrow('8×64');
    await expect(validateIcon(pngFile(513, 64))).rejects.toThrow('513×64');
  });

  it('accepts JPEG and WEBP signatures', async () => {
    const jpeg = new File([new Uint8Array([0xff, 0xd8, 0xff, 0xe0, 0, 0])], 'a.jpg', { type: 'image/jpeg' });
    const webp = new File([new TextEncoder().encode('RIFF1234WEBP')], 'a.webp', { type: 'image/webp' });
    await expect(validateIcon(jpeg)).resolves.toEqual({ width: 0, height: 0 });
    await expect(validateIcon(webp)).resolves.toEqual({ width: 0, height: 0 });
  });

  it('reads SVG dimensions from width/height or viewBox', async () => {
    await expect(validateIcon(svgFile('<svg xmlns="http://www.w3.org/2000/svg" width="64" height="48"><rect width="64" height="48"/></svg>')))
      .resolves.toEqual({ width: 64, height: 48 });
    await expect(validateIcon(svgFile('<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 40 30"><path d="M0 0h40v30z"/></svg>')))
      .resolves.toEqual({ width: 40, height: 30 });
  });

  it('rejects unsafe SVG content and missing sizes', async () => {
    await expect(validateIcon(svgFile('<svg xmlns="http://www.w3.org/2000/svg" width="32" height="32"><script>alert(1)</script></svg>')))
      .rejects.toThrow('SVG contains unsafe content');
    await expect(validateIcon(svgFile('<svg xmlns="http://www.w3.org/2000/svg" width="32" height="32" onload="x()"><rect width="32" height="32"/></svg>')))
      .rejects.toThrow('SVG contains unsafe content');
    await expect(validateIcon(svgFile('<svg xmlns="http://www.w3.org/2000/svg"><rect width="32" height="32"/></svg>')))
      .rejects.toThrow('SVG has no size');
  });

  it('rejects unsupported and empty files', async () => {
    await expect(validateIcon(new File([new Uint8Array([1, 2, 3])], 'a.gif', { type: 'image/gif' })))
      .rejects.toThrow('PNG, JPEG, WEBP or SVG required');
    await expect(validateIcon(new File([], 'empty.png', { type: 'image/png' }))).rejects.toThrow('File is empty');
  });
});
