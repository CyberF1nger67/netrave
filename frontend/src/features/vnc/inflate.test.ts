import { constants, createDeflate, deflateSync } from 'node:zlib';
import { describe, expect, it } from 'vitest';
import { Inflater } from './inflate';

function pattern(length: number): Uint8Array {
  const data = new Uint8Array(length);
  for (let i = 0; i < length; i++) data[i] = (i * 37 + (i >> 3) * 11) & 0xff;
  for (let i = 100; i < Math.min(length, 400); i += 7) data[i] = data[i - 1] ?? 0;
  return data;
}

function syncChunk(deflater: ReturnType<typeof createDeflate>, data: Uint8Array): Promise<Uint8Array> {
  return new Promise((resolve, reject) => {
    const chunks: Buffer[] = [];
    const onData = (chunk: Buffer) => chunks.push(chunk);
    deflater.on('data', onData);
    deflater.once('error', reject);
    deflater.write(data);
    deflater.flush(constants.Z_SYNC_FLUSH, () => {
      deflater.off('data', onData);
      deflater.off('error', reject);
      resolve(new Uint8Array(Buffer.concat(chunks)));
    });
  });
}

describe('Inflater', () => {
  it('decodes a zlib stream', () => {
    const input = pattern(120000);
    const compressed = new Uint8Array(deflateSync(input));
    const output = new Inflater().inflate(compressed, input.length);
    expect(output).toEqual(input);
  });

  it('keeps the deflate state between sync-flushed rectangles', async () => {
    const first = pattern(9000);
    const second = pattern(5000);
    const deflater = createDeflate();
    const one = await syncChunk(deflater, first);
    const two = await syncChunk(deflater, second);
    deflater.close();
    const inflater = new Inflater();
    expect(inflater.inflate(one, first.length)).toEqual(first);
    expect(inflater.inflate(two, second.length)).toEqual(second);
  });

  it('decodes stored blocks', () => {
    const input = pattern(5000);
    const compressed = new Uint8Array(deflateSync(input, { level: 0 }));
    expect(new Inflater().inflate(compressed, input.length)).toEqual(input);
  });

  it('starts a fresh zlib stream after reset', () => {
    const input = pattern(500);
    const inflater = new Inflater();
    expect(inflater.inflate(new Uint8Array(deflateSync(input)), input.length)).toEqual(input);
    inflater.reset();
    expect(inflater.inflate(new Uint8Array(deflateSync(input)), input.length)).toEqual(input);
  });
});
