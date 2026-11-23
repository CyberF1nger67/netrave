// Minimal streaming inflate for RFB Tight encoding. The browser's
// DecompressionStream is asynchronous, while RFB parsing in VNCView is synchronous, so
// only deflate/zlib is implemented here, without external dependencies.
// Stored, fixed and dynamic Huffman blocks are supported, 32 KiB dictionary.

const LEN_BASE = [3, 4, 5, 6, 7, 8, 9, 10, 11, 13, 15, 17, 19, 23, 27, 31, 35, 43, 51, 59, 67, 83, 99, 115, 131, 163, 195, 227, 258];
const LEN_EXTRA = [0, 0, 0, 0, 0, 0, 0, 0, 1, 1, 1, 1, 2, 2, 2, 2, 3, 3, 3, 3, 4, 4, 4, 4, 5, 5, 5, 5, 0];
const DIST_BASE = [1, 2, 3, 4, 5, 7, 9, 13, 17, 25, 33, 49, 65, 97, 129, 193, 257, 385, 513, 769, 1025, 1537, 2049, 3073, 4097, 6145, 8193, 12289, 16385, 24577];
const DIST_EXTRA = [0, 0, 0, 0, 1, 1, 2, 2, 3, 3, 4, 4, 5, 5, 6, 6, 7, 7, 8, 8, 9, 9, 10, 10, 11, 11, 12, 12, 13, 13];
const CL_ORDER = [16, 17, 18, 0, 8, 7, 9, 6, 10, 5, 11, 4, 12, 3, 13, 2, 14, 1, 15];

interface Huffman {
  counts: Uint16Array;
  symbols: Uint16Array;
}

function buildTree(lengths: Uint8Array): Huffman {
  const counts = new Uint16Array(16);
  for (const length of lengths) counts[length] = (counts[length] ?? 0) + 1;
  counts[0] = 0;
  const next = new Uint16Array(16);
  let total = 0;
  for (let length = 1; length < 16; length++) {
    next[length] = total;
    total += counts[length] ?? 0;
  }
  const symbols = new Uint16Array(total);
  for (let symbol = 0; symbol < lengths.length; symbol++) {
    const length = lengths[symbol] ?? 0;
    if (length) symbols[next[length]!++] = symbol;
  }
  return { counts, symbols };
}

let fixedLit: Huffman | null = null;
let fixedDist: Huffman | null = null;
function fixedTrees(): [Huffman, Huffman] {
  if (!fixedLit || !fixedDist) {
    const lit = new Uint8Array(288);
    for (let i = 0; i < 144; i++) lit[i] = 8;
    for (let i = 144; i < 256; i++) lit[i] = 9;
    for (let i = 256; i < 280; i++) lit[i] = 7;
    for (let i = 280; i < 288; i++) lit[i] = 8;
    const dist = new Uint8Array(32).fill(5);
    fixedLit = buildTree(lit);
    fixedDist = buildTree(dist);
  }
  return [fixedLit, fixedDist];
}

export class Inflater {
  private pending = new Uint8Array(0);
  private pos = 0;
  private bitbuf = 0;
  private bitcnt = 0;
  private needHeader = true;
  private mode: 'header' | 'stored' | 'huff' | 'done' = 'header';
  private final = false;
  private storedLeft = 0;
  private lit: Huffman | null = null;
  private dist: Huffman | null = null;
  private matchLen = 0;
  private matchDist = 0;
  private readonly history = new Uint8Array(32768);
  private historyPos = 0;

  reset(): void {
    this.pending = new Uint8Array(0);
    this.pos = 0;
    this.bitbuf = 0;
    this.bitcnt = 0;
    this.needHeader = true;
    this.mode = 'header';
    this.final = false;
    this.storedLeft = 0;
    this.lit = null;
    this.dist = null;
    this.matchLen = 0;
    this.matchDist = 0;
    this.historyPos = 0;
  }

  // inflate returns exactly expected decompressed bytes. The undecompressed
  // tail of the zlib stream (sync marker) is kept until the next call.
  inflate(data: Uint8Array, expected: number): Uint8Array {
    const rest = this.pending.subarray(this.pos);
    const input = new Uint8Array(rest.length + data.length);
    input.set(rest, 0);
    input.set(data, rest.length);
    this.pending = input;
    this.pos = 0;

    if (this.needHeader) {
      if (input.length < 2) throw new Error('Tight: truncated zlib header');
      const cmf = input[0] ?? 0;
      const flg = input[1] ?? 0;
      if ((cmf & 0x0f) !== 8 || ((cmf << 8) | flg) % 31 !== 0) throw new Error('Tight: invalid zlib header');
      this.pos = 2;
      this.needHeader = false;
    }

    const out = new Uint8Array(expected);
    let written = 0;
    while (written < expected) {
      if (this.matchLen > 0) {
        const count = Math.min(this.matchLen, expected - written);
        for (let i = 0; i < count; i++) {
          const byte = this.history[(this.historyPos - this.matchDist + 32768) & 32767] ?? 0;
          out[written++] = byte;
          this.history[this.historyPos] = byte;
          this.historyPos = (this.historyPos + 1) & 32767;
        }
        this.matchLen -= count;
        continue;
      }
      if (this.mode === 'header') {
        this.final = this.readBits(1) === 1;
        const type = this.readBits(2);
        if (type === 0) {
          this.alignByte();
          const length = this.readByte() | (this.readByte() << 8);
          const inverse = this.readByte() | (this.readByte() << 8);
          if ((length ^ 0xffff) !== inverse) throw new Error('Tight: stored block is corrupted');
          this.storedLeft = length;
          this.mode = 'stored';
        } else if (type === 1) {
          [this.lit, this.dist] = fixedTrees();
          this.mode = 'huff';
        } else if (type === 2) {
          this.readDynamic();
          this.mode = 'huff';
        } else {
          throw new Error('Tight: invalid deflate block type');
        }
      } else if (this.mode === 'stored') {
        if (this.storedLeft === 0) {
          this.mode = this.final ? 'done' : 'header';
          continue;
        }
        const byte = this.readByte();
        out[written++] = byte;
        this.history[this.historyPos] = byte;
        this.historyPos = (this.historyPos + 1) & 32767;
        this.storedLeft--;
      } else if (this.mode === 'huff') {
        const symbol = this.decode(this.lit!);
        if (symbol < 256) {
          out[written++] = symbol;
          this.history[this.historyPos] = symbol;
          this.historyPos = (this.historyPos + 1) & 32767;
        } else if (symbol === 256) {
          this.mode = this.final ? 'done' : 'header';
        } else {
          const lengthIndex = symbol - 257;
          if (lengthIndex >= LEN_BASE.length) throw new Error('Tight: invalid length code');
          const length = (LEN_BASE[lengthIndex] ?? 0) + this.readBits(LEN_EXTRA[lengthIndex] ?? 0);
          const distanceSymbol = this.decode(this.dist!);
          if (distanceSymbol >= DIST_BASE.length) throw new Error('Tight: invalid distance code');
          this.matchLen = length;
          this.matchDist = (DIST_BASE[distanceSymbol] ?? 0) + this.readBits(DIST_EXTRA[distanceSymbol] ?? 0);
        }
      } else {
        throw new Error('Tight: zlib stream ended early');
      }
    }
    return out;
  }

  private readDynamic(): void {
    const literalCount = this.readBits(5) + 257;
    const distanceCount = this.readBits(5) + 1;
    const codeCount = this.readBits(4) + 4;
    const codeLengths = new Uint8Array(19);
    for (let i = 0; i < codeCount; i++) codeLengths[CL_ORDER[i] ?? 0] = this.readBits(3);
    const codeTree = buildTree(codeLengths);
    const lengths = new Uint8Array(literalCount + distanceCount);
    let index = 0;
    while (index < lengths.length) {
      const symbol = this.decode(codeTree);
      if (symbol < 16) {
        lengths[index++] = symbol;
      } else if (symbol === 16) {
        if (index === 0) throw new Error('Tight: repeat without a previous length');
        const previous = lengths[index - 1] ?? 0;
        const repeat = 3 + this.readBits(2);
        for (let i = 0; i < repeat && index < lengths.length; i++) lengths[index++] = previous;
      } else if (symbol === 17) {
        const repeat = 3 + this.readBits(3);
        index += repeat;
      } else if (symbol === 18) {
        const repeat = 11 + this.readBits(7);
        index += repeat;
      } else {
        throw new Error('Tight: invalid length code');
      }
    }
    this.lit = buildTree(lengths.subarray(0, literalCount));
    this.dist = buildTree(lengths.subarray(literalCount));
  }

  private decode(tree: Huffman): number {
    let code = 0;
    let first = 0;
    let index = 0;
    for (let length = 1; length <= 15; length++) {
      code |= this.readBit();
      const count = tree.counts[length] ?? 0;
      if (code < first + count && code >= first) return tree.symbols[index + code - first] ?? 0;
      index += count;
      first = (first + count) << 1;
      code <<= 1;
    }
    throw new Error('Tight: invalid Huffman code');
  }

  private readBit(): number {
    if (this.bitcnt === 0) {
      if (this.pos >= this.pending.length) throw new Error('Tight: truncated zlib stream');
      this.bitbuf = this.pending[this.pos++] ?? 0;
      this.bitcnt = 8;
    }
    const bit = this.bitbuf & 1;
    this.bitbuf >>>= 1;
    this.bitcnt--;
    return bit;
  }

  private readBits(count: number): number {
    let value = 0;
    for (let i = 0; i < count; i++) value |= this.readBit() << i;
    return value;
  }

  private readByte(): number {
    if (this.bitcnt >= 8) {
      const byte = this.bitbuf & 0xff;
      this.bitbuf >>>= 8;
      this.bitcnt -= 8;
      return byte;
    }
    if (this.pos >= this.pending.length) throw new Error('Tight: truncated zlib stream');
    return this.pending[this.pos++] ?? 0;
  }

  private alignByte(): void {
    const drop = this.bitcnt & 7;
    this.bitbuf >>>= drop;
    this.bitcnt -= drop;
  }
}
