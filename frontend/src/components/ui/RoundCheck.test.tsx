import { describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/react';
import { RoundCheck } from './RoundCheck';

describe('круглый флажок', () => {
  it('остаётся checkbox и передаёт состояние наружу', () => {
    const onChange = vi.fn();
    render(<RoundCheck checked={false} ariaLabel="Выбрать иконку" onChange={onChange} />);
    const box = screen.getByRole('checkbox', { name: 'Выбрать иконку' });
    expect(box).not.toBeChecked();
    fireEvent.click(box);
    expect(onChange).toHaveBeenCalledWith(true);
  });

  it('показывает промежуточное состояние выбора всех строк', () => {
    render(<RoundCheck checked={false} indeterminate ariaLabel="Выбрать все" onChange={() => undefined} />);
    expect(screen.getByRole('checkbox', { name: 'Выбрать все' })).toHaveAttribute('aria-checked', 'mixed');
  });
});
