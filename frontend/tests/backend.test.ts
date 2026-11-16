import { expect, it, vi } from 'vitest';
vi.mock('server-only', () => ({}));
import { backendResponse } from '@/lib/backend';
it('uses a server-only runtime URL and preserves upstream status', async () => {
  vi.stubEnv('API_INTERNAL_URL', 'http://api:8080');
  const fetch = vi.fn().mockResolvedValue(Response.json({ code: 50301, data: null }, { status: 503 }));
  vi.stubGlobal('fetch', fetch);
  const response = await backendResponse('/api/v1/health');
  expect(response.status).toBe(503);
  expect(fetch.mock.calls[0][0].toString()).toBe('http://api:8080/api/v1/health');
  expect(response.headers.get('cache-control')).toBe('no-store');
});
it('returns a sanitized unavailable response on failure', async () => {
  vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new Error('private details')));
  const response = await backendResponse('/api/v1/health');
  expect(response.status).toBe(503);
  expect(await response.json()).toEqual({ code: 50302, message: 'API unavailable', data: null });
});
