import { describe, expect, it } from 'vitest';
import { render, screen } from '@testing-library/react';
import { Switch } from './Switch';

describe('переключатель', () => {
  it('остаётся checkbox с подписью и состоянием', () => {
    render(<Switch checked label="Права на запуск" onChange={() => undefined} />);
    const box = screen.getByRole('checkbox', { name: 'Права на запуск' });
    expect(box).toBeChecked();
  });

  it('без подписи использует доступное имя', () => {
    render(<Switch checked={false} ariaLabel="Выбрать все" onChange={() => undefined} />);
    expect(screen.getByRole('checkbox', { name: 'Выбрать все' })).not.toBeChecked();
  });
});
