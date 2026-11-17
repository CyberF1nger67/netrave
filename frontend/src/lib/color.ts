/**
  * `#rrggbb` color with opacity in rgba() format. color-mix() is not used: it is unavailable in Firefox below 113
  * and Safari below 16.2. A string that is not a hex color is returned as is.
 */
export function withAlpha(hex: string, alpha: number): string {
  const match = /^#([0-9a-f]{6})$/i.exec(hex.trim());
  if (!match) return hex;
  const n = Number.parseInt(match[1] ?? '000000', 16);
  return `rgba(${(n >> 16) & 255}, ${(n >> 8) & 255}, ${n & 255}, ${alpha})`;
}
