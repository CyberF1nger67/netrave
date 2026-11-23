import { describe, expect, it } from 'vitest';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';

// React rule: a component must not call hooks after an early `return`.
// That made the journal page crash with "Minified React error #310" when loading data.
function componentSource(file: string, name: string): string {
  const text = readFileSync(resolve(__dirname, '..', '..', file), 'utf8');
  const start = text.indexOf(`export function ${name}(`);
  expect(start, `${name} not found in ${file}`).toBeGreaterThanOrEqual(0);
  const end = text.indexOf('\n}\n', start);
  return text.slice(start, end === -1 ? undefined : end);
}

function firstEarlyReturn(code: string): number {
  const match = /\n {2}if \([^)]*\) return /.exec(code);
  return match ? match.index : -1;
}

function lastHook(code: string): number {
  const hooks = /use[A-Z]\w*\(/g;
  let last = -1;
  let match: RegExpExecArray | null;
  while ((match = hooks.exec(code))) last = match.index;
  return last;
}

describe('logs', () => {
  it('calls all hooks before the first early return', () => {
    const code = componentSource('features/logs/LogsPage.tsx', 'LogsPage');
    const early = firstEarlyReturn(code);
    if (early === -1) return;
    expect(lastHook(code), 'hook after early return').toBeLessThan(early);
  });
});
