import { describe, expect, it } from 'vitest';
import { render, screen } from '@testing-library/react';
import { Switch } from './Switch';

describe('switch', () => {
  it('stays a checkbox with a label and state', () => {
    render(<Switch checked label="Start permissions" onChange={() => undefined} />);
    const box = screen.getByRole('checkbox', { name: 'Start permissions' });
    expect(box).toBeChecked();
  });

  it('without a label uses the accessible name', () => {
    render(<Switch checked={false} ariaLabel="Select all" onChange={() => undefined} />);
    expect(screen.getByRole('checkbox', { name: 'Select all' })).not.toBeChecked();
  });
});
