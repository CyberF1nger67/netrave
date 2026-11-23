import { afterEach, describe, expect, it, vi } from 'vitest';
import { ApiError, request, setCsrfToken } from './client';

describe('api client', () => {
  afterEach(() => vi.restoreAllMocks());

  it('adds csrf header for mutations', async () => {
    setCsrfToken('token');
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(JSON.stringify({ ok: true }), { status: 200 }));
    await request('POST', '/api/test', { value: 1 });
    const init = fetchMock.mock.calls[0]?.[1] as RequestInit;
    expect((init.headers as Record<string, string>)['X-CSRF-Token']).toBe('token');
    expect(init.credentials).toBe('same-origin');
  });

  it('throws ApiError with server message', async () => {
    vi.spyOn(globalThis, 'fetch').mockImplementation(() => Promise.resolve(new Response(JSON.stringify({ error: 'no permission' }), { status: 403 })));
    await expect(request('GET', '/api/test')).rejects.toMatchObject({ message: 'no permission', status: 403 });
    await expect(request('GET', '/api/test')).rejects.toBeInstanceOf(ApiError);
  });
});
