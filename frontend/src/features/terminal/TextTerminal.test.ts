import { afterEach, describe, expect, it } from 'vitest';
import { TextTerminal } from './TextTerminal';

function waitFrame(): Promise<void> {
  return new Promise((resolve) => requestAnimationFrame(() => resolve()));
}

describe('TextTerminal', () => {
  const hosts: HTMLElement[] = [];

  afterEach(() => {
    for (const host of hosts) host.remove();
    hosts.length = 0;
  });

  function create(sent: string[] = []): TextTerminal {
    const host = document.createElement('div');
    document.body.append(host);
    hosts.push(host);
    return new TextTerminal(host, (data) => sent.push(data));
  }

  function cursor(term: TextTerminal): HTMLElement {
    return term.root.querySelector('[data-cursor]') as HTMLElement;
  }

  function historyRows(term: TextTerminal): number {
    return (term.root.querySelector('[data-term-out]')?.firstElementChild?.childElementCount ?? 0);
  }

  it('wraps a line at 80 columns and keeps the cursor on the next row', async () => {
    const term = create();
    term.write('a'.repeat(80) + 'b');
    await waitFrame();

    expect(term.screenText()).toBe(`${'a'.repeat(80)}\nb`);
    expect(cursor(term).style.left).toBe('1ch');
  });

  it('treats both CRLF and bare LF as a newline', async () => {
    const term = create();
    term.write('one\r\ntwo\nthree');
    await waitFrame();

    expect(term.screenText()).toBe('one\ntwo\nthree');
  });

  it('places the cursor overlay after the last typed character', async () => {
    const term = create();
    term.write('abc');
    await waitFrame();

    expect(cursor(term).style.left).toBe('3ch');
    expect(cursor(term).textContent).toBe(' ');
  });

  it('erases a line and continues from the start of it', async () => {
    const term = create();
    term.write('hello world');
    term.write('\r\x1b[2Kx');
    await waitFrame();

    expect(term.screenText()).toBe('x');
  });

  it('overwrites at an absolute column (CHA)', async () => {
    const term = create();
    term.write('abcdef\x1b[3Gxyz');
    await waitFrame();

    expect(term.screenText()).toBe('abxyzf');
  });

  it('deletes characters at the cursor (DCH)', async () => {
    const term = create();
    term.write('abcdef\x1b[3G\x1b[2P');
    await waitFrame();

    expect(term.screenText()).toBe('abef');
  });

  it('moves lines that leave the top of the screen into history', async () => {
    const term = create();
    let text = '';
    for (let line = 1; line <= 30; line++) text += `${line}\r\n`;
    term.write(text);
    await waitFrame();

    expect(historyRows(term)).toBe(7);
    expect(term.screenText().split('\n')[0]).toBe('8');
    expect(term.screenText().split('\n').at(-1)).toBe('30');
  });

  it('keeps lines inside a scroll region out of history', async () => {
    const term = create();
    term.write('\x1b[1;2r');
    term.write('a\r\nb\r\nc');
    await waitFrame();

    expect(historyRows(term)).toBe(0);
    expect(term.screenText()).toBe('b\nc');
  });

  it('draws DEC special graphics when the charset is switched', async () => {
    const term = create();
    term.write('\x1b(0lqk\x1b(B');
    await waitFrame();

    expect(term.screenText()).toBe('┌─┐');
  });

  it('gives wide characters two columns', async () => {
    const term = create();
    term.write('漢a');
    await waitFrame();

    expect(term.screenText()).toBe('漢a');
    expect(cursor(term).style.left).toBe('3ch');
  });

  it('restores the main screen when leaving the alternate screen', async () => {
    const term = create();
    term.write('main\x1b[?1049hALT\x1b[?1049l');
    await waitFrame();

    expect(term.screenText()).toBe('main');
  });

  it('answers a cursor position request (DSR 6) with the real position', () => {
    const sent: string[] = [];
    const term = create(sent);
    term.write('ab\x1b[6n');

    expect(sent).toContain('\x1b[1;3R');
  });

  it('sends ANSI sequences for arrow keys', () => {
    const sent: string[] = [];
    const term = create(sent);
    term.root.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowUp', bubbles: true }));

    expect(sent).toEqual(['\x1b[A']);
  });
});
