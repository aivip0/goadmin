import { expect, test } from '@playwright/test';

test.skip(process.env.E2E_SECURITY !== '1', 'Requires isolated Phase 2.1 API and migrated test database');

test('real browser obtains HttpOnly CSRF cookie and passes the security boundary', async ({ page, context }) => {
  await page.goto('/');
  const issued = await page.evaluate(async () => {
    const response = await fetch('/api/v1/auth/csrf', { credentials: 'same-origin' });
    const result = await response.json();
    return { status: response.status, token: result.data.csrf_token, id: response.headers.get('x-request-id') };
  });
  expect(issued.status).toBe(200); expect(issued.id).toMatch(/^[0-9a-f]{32}$/);
  const cookie = (await context.cookies()).find((cookie) => cookie.name === 'goadmin_csrf');
  expect(cookie?.httpOnly).toBe(true); expect(cookie?.sameSite).toBe('Lax'); expect(cookie?.path).toBe('/');
  expect(await page.evaluate(() => document.cookie)).not.toContain('goadmin_csrf');
  const results = await page.evaluate(async (token) => {
    const codes: number[] = [];
    for (const value of ['', 'incorrect', token]) {
      const response = await fetch('/api/v1/auth/login', { method: 'POST', credentials: 'same-origin', headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': value }, body: '{}' });
      codes.push(response.status);
    }
    return codes;
  }, issued.token);
  // The real login handler rejects malformed JSON after valid CSRF.
  expect(results).toEqual([403, 403, 400]);
  const forbidden = await page.request.post('/api/v1/auth/login', { headers: { Origin: 'https://attacker.invalid', 'X-CSRF-Token': issued.token } });
  expect(forbidden.status()).toBe(403);
  const crossSite = await page.request.get('/api/v1/auth/csrf', { headers: { 'Sec-Fetch-Site': 'cross-site' } });
  expect(crossSite.status()).toBe(403);
  const illegal = await page.request.get('/api/v1/internal/config'); expect(illegal.status()).toBe(404);
});
