import { describe, expect, it } from 'vitest';
import { fmtBytes, fmtDisk, fmtPct, fmtUptime, levelOf } from './format';

describe('format', () => {
  it('formats bytes', () => {
    expect(fmtBytes(0)).toBe('0');
    expect(fmtBytes(1024)).toBe('1.0 KB');
    expect(fmtBytes(1024 * 1024 * 1.5)).toBe('1.5 MB');
  });
  it('formats disk and percentages', () => {
    expect(fmtDisk(1024 * 1024)).toBe('1 MB');
    expect(fmtDisk(2 * 1024 * 1024 * 1024)).toBe('2.00 GB');
    expect(fmtPct(12.34)).toBe('12,3%');
  });
  it('formats uptime', () => {
    expect(fmtUptime(0)).toBe('0h 0m');
    expect(fmtUptime(90061)).toBe('1d 1h 1m');
  });
  it('classifies load levels', () => {
    expect(levelOf(10)).toBe('ok');
    expect(levelOf(70)).toBe('warn');
    expect(levelOf(90)).toBe('bad');
  });
});
