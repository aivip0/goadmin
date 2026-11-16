import { describe, expect, it, vi } from 'vitest';
import { getHealth } from '@/services/health';
const health = { status: 'ok', database: 'connected', migration: 'current', checked_at: '2026-11-12T03:00:00Z' };
describe('health client', () => {
  it('accepts a confirmed health response', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(Response.json({ code: 0, data: health })));
    expect(await getHealth()).toEqual(health);
  });
  it.each([
    { status: 503, body: { code: 50301, data: null } },
    { status: 200, body: { code: 0, data: null } },
    { status: 200, body: { code: 0, data: { ...health, database: 'disconnected' } } },
    { status: 200, body: { code: 0, data: { ...health, checked_at: 'invalid' } } },
  ])('rejects unconfirmed health %#', async ({ status, body }) => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(Response.json(body, { status })));
    await expect(getHealth()).rejects.toThrow();
  });
  it('does not mask a network failure', async () => {
    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new Error('offline')));
    await expect(getHealth()).rejects.toThrow('offline');
  });
});
