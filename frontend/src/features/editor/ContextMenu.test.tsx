import { describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/react';
import { ContextMenu, type MenuItem } from './ContextMenu';

function build() {
  const run = { isolated: vi.fn(), nat: vi.fn(), cloud: vi.fn(), paste: vi.fn() };
  const items: MenuItem[] = [
    { label: 'Add machine', run: vi.fn() },
    { label: 'Add network', sub: [
      { label: 'Isolated', run: run.isolated },
      { label: 'NAT', run: run.nat },
      { label: 'Cloud', run: run.cloud },
    ] },
    { separator: true },
    { label: 'Paste', disabled: false, run: run.paste },
  ];
  const onClose = vi.fn();
  const view = render(<div><span>outside</span><ContextMenu x={120} y={80} items={items} onClose={onClose} /></div>);
  return { ...view, run, onClose };
}

describe('ContextMenu', () => {
  it('lists the top-level actions', () => {
    build();
    expect(screen.getByRole('button', { name: 'Add machine' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /Add network/ })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Paste' })).toBeInTheDocument();
  });

  it('opens the network list on click and on hover, with every available type', () => {
    build();
    fireEvent.click(screen.getByRole('button', { name: /Add network/ }));
    expect(screen.getByRole('menu')).toBeInTheDocument();
    for (const name of ['Isolated', 'NAT', 'Cloud']) expect(screen.getByRole('button', { name })).toBeEnabled();
  });

  it('renders the submenu outside the scrolling list, so overflow cannot clip it', () => {
    const { container } = build();
    fireEvent.mouseEnter(screen.getByRole('button', { name: /Add network/ }));
    const list = container.querySelector('[data-ctx-list]') as HTMLElement;
    const nat = screen.getByRole('button', { name: 'NAT' });
    expect(list).not.toBeNull();
    expect(list.contains(nat)).toBe(false);
    expect(screen.getByRole('menu').parentElement).toHaveAttribute('data-ctx-menu');
  });

  it('runs the chosen network action and closes the menu', () => {
    const { run, onClose } = build();
    fireEvent.click(screen.getByRole('button', { name: /Add network/ }));
    fireEvent.click(screen.getByRole('button', { name: 'NAT' }));
    expect(run.nat).toHaveBeenCalledTimes(1);
    expect(run.isolated).not.toHaveBeenCalled();
    expect(onClose).toHaveBeenCalled();
  });

  it('runs every network option', () => {
    const { run } = build();
    for (const name of ['Isolated', 'Cloud'] as const) {
      fireEvent.click(screen.getByRole('button', { name: /Add network/ }));
      fireEvent.click(screen.getByRole('button', { name }));
      expect(run[name.toLowerCase() as 'isolated' | 'cloud']).toHaveBeenCalledTimes(1);
    }
  });

  it('keeps the menu open when clicking inside the submenu', () => {
    const { onClose } = build();
    fireEvent.click(screen.getByRole('button', { name: /Add network/ }));
    fireEvent.mouseDown(screen.getByRole('button', { name: 'Cloud' }));
    expect(onClose).not.toHaveBeenCalled();
  });

  it('closes on a click outside and on Escape', () => {
    const { onClose } = build();
    fireEvent.mouseDown(screen.getByText('outside'));
    expect(onClose).toHaveBeenCalledTimes(1);
    fireEvent.keyDown(document, { key: 'Escape' });
    expect(onClose).toHaveBeenCalledTimes(2);
  });

  it('does not run disabled items', () => {
    const run = vi.fn();
    render(<ContextMenu x={0} y={0} items={[{ label: 'Stop', disabled: true, run }]} onClose={() => undefined} />);
    fireEvent.click(screen.getByRole('button', { name: 'Stop' }));
    expect(run).not.toHaveBeenCalled();
  });
});
