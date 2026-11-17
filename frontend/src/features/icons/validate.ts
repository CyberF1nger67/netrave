import { t } from '../../i18n';

const LIMIT = 512 * 1024;
const MIN = 16;
const MAX = 512;
const SVG_MIN = 1;
const SVG_MAX = 8192;
const HEAD_BYTES = 64 * 1024;
const PNG_SIGNATURE = [0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a];
// IHDR follows the signature immediately: 4 bytes of length, 4 bytes of type, then width and height.
const PNG_HEADER_BYTES = 24;

interface Size { width: number; height: number }

/** Reads the beginning of the file via FileReader: Blob.arrayBuffer is not available in all browsers/environments. */
function readHead(file: File, limit: number): Promise<Uint8Array> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => resolve(new Uint8Array(reader.result as ArrayBuffer));
    reader.onerror = () => reject(reader.error ?? new Error(t('File is empty')));
    reader.readAsArrayBuffer(file.slice(0, Math.min(file.size, limit)));
  });
}

function isPng(head: Uint8Array): boolean {
  return head.length >= PNG_HEADER_BYTES && PNG_SIGNATURE.every((byte, index) => head[index] === byte);
}

function pngSize(head: Uint8Array): Size {
  const view = new DataView(head.buffer, head.byteOffset, head.byteLength);
  return { width: view.getUint32(16), height: view.getUint32(20) };
}

function isJpeg(head: Uint8Array): boolean {
  return head.length >= 3 && head[0] === 0xff && head[1] === 0xd8 && head[2] === 0xff;
}

function isWebp(head: Uint8Array): boolean {
  return head.length >= 12
    && String.fromCharCode(...head.slice(0, 4)) === 'RIFF'
    && String.fromCharCode(...head.slice(8, 12)) === 'WEBP';
}

function svgSize(text: string): Size | null {
  const root = /<svg\b[^>]*>/i.exec(text)?.[0];
  if (!root) return null;
  const attr = (name: string): string | undefined => {
    const match = new RegExp(`\\b${name}\\s*=\\s*["']([^"']*)["']`, 'i').exec(root);
    return match?.[1];
  };
  const length = (value: string | undefined): number | undefined => {
    const match = value ? /^\s*([0-9]+(?:\.[0-9]+)?)(?:px|pt|pc|mm|cm|in)?\s*$/i.exec(value) : null;
    if (!match) return undefined;
    const number = Number(match[1]);
    return Number.isFinite(number) && number > 0 ? number : undefined;
  };
  const viewBox = (attr('viewBox') ?? '').trim().split(/[\s,]+/).map(Number);
  const boxWidth = viewBox.length === 4 && viewBox[2]! > 0 ? viewBox[2]! : undefined;
  const boxHeight = viewBox.length === 4 && viewBox[3]! > 0 ? viewBox[3]! : undefined;
  let width = length(attr('width'));
  let height = length(attr('height'));
  if (width === undefined && height === undefined) {
    width = boxWidth;
    height = boxHeight;
  } else if (width !== undefined && height === undefined) {
    height = boxWidth && boxHeight ? (width * boxHeight) / boxWidth : width;
  } else if (height !== undefined && width === undefined) {
    width = boxWidth && boxHeight ? (height * boxWidth) / boxHeight : height;
  }
  if (!width || !height) return null;
  return { width: Math.round(width), height: Math.round(height) };
}

/**
  * A quick image check before upload. The server repeats and deepens it:
  * raster formats are rebuilt as PNG, and SVG is cleaned against a whitelist.
  * The client checks file size, signature and dimensions so the user immediately
  * sees a clear error without waiting for the server response.
 */
export async function validateIcon(file: File): Promise<Size> {
  if (!file || file.size === 0) throw new Error(t('File is empty'));
  if (file.size > LIMIT) throw new Error(t('File is larger than 512 KB'));

  const head = await readHead(file, HEAD_BYTES);
  if (isPng(head)) {
    const size = pngSize(head);
    if (!size.width || !size.height) throw new Error(t('PNG could not be opened'));
    if (size.width < MIN || size.width > MAX || size.height < MIN || size.height > MAX) {
      throw new Error(`${size.width}×${size.height}: ${MIN}–${MAX} px`);
    }
    return size;
  }
  // JPEG and WEBP are decoded by the server: dimensions from their headers are not needed by the UI.
  if (isJpeg(head) || isWebp(head)) return { width: 0, height: 0 };

  let text = '';
  try {
    text = new TextDecoder('utf-8', { fatal: false }).decode(head).replace(/^\uFEFF/, '').trimStart();
  } catch {
    text = '';
  }
  if (!/^(?:<\?xml\b|<svg\b)/i.test(text) || !/<svg\b/i.test(text)) {
    throw new Error(t('PNG, JPEG, WEBP or SVG required'));
  }
  if (/<\s*(?:script|foreignObject|iframe|image|use|style|animate|set|feImage)\b/i.test(text)
    || /\son[a-z]+\s*=/i.test(text)
    || /(?:javascript|vbscript|data):/i.test(text)) {
    throw new Error(t('SVG contains unsafe content'));
  }
  const size = svgSize(text);
  if (!size) throw new Error(t('SVG has no size'));
  if (size.width < SVG_MIN || size.width > SVG_MAX || size.height < SVG_MIN || size.height > SVG_MAX) {
    throw new Error(`${size.width}×${size.height}: ${SVG_MIN}–${SVG_MAX} px`);
  }
  return size;
}
