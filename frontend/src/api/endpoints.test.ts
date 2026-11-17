import { afterEach, describe, expect, it, vi } from 'vitest';
import { authApi } from './endpoints';

describe('auth api', () => {
  afterEach(() => vi.restoreAllMocks());

  it('sends the new password in the field the server expects', async () => {
    const fetchMock = vi.spyOn(globalThis, 'fetch')
      .mockResolvedValue(new Response(JSON.stringify({ ok: true }), { status: 200 }));

    await authApi.changePassword('old-secret', 'new-secret-123');

    const init = fetchMock.mock.calls[0]?.[1] as RequestInit;
    expect(JSON.parse(String(init.body))).toEqual({ current: 'old-secret', new: 'new-secret-123' });
  });

  it('sends the confirmation to the server when provided', async () => {
    const fetchMock = vi.spyOn(globalThis, 'fetch')
      .mockResolvedValue(new Response(JSON.stringify({ ok: true }), { status: 200 }));

    await authApi.changePassword('old-secret', 'new-secret-123', 'new-secret-123');

    const init = fetchMock.mock.calls[0]?.[1] as RequestInit;
    expect(JSON.parse(String(init.body))).toEqual({
      current: 'old-secret', new: 'new-secret-123', confirm: 'new-secret-123',
    });
  });
});
