import { expect, test } from '@playwright/test';
import { request as httpRequest } from 'node:http';
import { setTimeout as delay } from 'node:timers/promises';

test.skip(process.env.E2E_MEDIA_HTTP !== '1', 'Requires isolated Go, Next, PostgreSQL and delay proxy');
test.setTimeout(120_000);

test('real Media HTTP streaming, first byte, idle timeout and cancellation', async ({ page }, testInfo) => {
  await page.goto('/login');
  await page.getByLabel('用户名').fill(process.env.E2E_ADMIN_USER!);
  await page.getByLabel('密码', { exact: true }).fill(process.env.E2E_ADMIN_PASSWORD!);
  await page.getByRole('button', { name: '登录', exact: true }).click();
  await expect(page).toHaveURL(/\/admin$/);
  const origin = new URL(page.url()).origin;
  const csrf = (await (await page.request.get('/api/v1/auth/csrf')).json()).data.csrf_token as string;
  const cookie = (await page.context().cookies()).filter(c => c.name.startsWith('goadmin_')).map(c => `${c.name}=${c.value}`).join('; ');
  const filename = `transport-${testInfo.project.name}.txt`;
  const boundary = 'media-reliability-boundary';
  const prefix = `--${boundary}\r\nContent-Disposition: form-data; name="file"; filename="${filename}"\r\nContent-Type: text/plain\r\n\r\n`;
  const url = new URL('/api/v1/admin/media', origin);
  const response = await new Promise<{ status: number; body: string }>((resolve, reject) => {
    const req = httpRequest(url, { method: 'POST', headers: { Cookie: cookie, Origin: origin, 'X-CSRF-Token': csrf, 'Content-Type': `multipart/form-data; boundary=${boundary}` } }, res => {
      const chunks: Buffer[] = [];
      res.on('data', c => chunks.push(Buffer.from(c)));
      res.on('end', () => resolve({ status: res.statusCode ?? 0, body: Buffer.concat(chunks).toString() }));
    });
    req.on('error', reject);
    (async () => {
      req.write(prefix);
      await delay(150);
      req.write('slow ');
      await delay(150);
      req.end(`upload\n\r\n--${boundary}--\r\n`);
    })().catch(reject);
  });
  expect(response.status, response.body).toBe(200);
  const id = JSON.parse(response.body).data.id as string;
  const beforeAbort = (await (await page.request.get('/api/v1/admin/media')).json()).data.total as number;
  await new Promise<void>((resolve) => {
    const req = httpRequest(url, { method: 'POST', headers: { Cookie: cookie, Origin: origin, 'X-CSRF-Token': csrf, 'Content-Type': `multipart/form-data; boundary=${boundary}` } });
    req.on('error', () => resolve());
    req.write(prefix.replace(filename, 'interrupted.txt') + 'partial');
    setTimeout(() => { req.destroy(); resolve(); }, 200);
  });
  await delay(350);
  expect((await (await page.request.get('/api/v1/admin/media')).json()).data.total).toBe(beforeAbort);

  const contentURL = `${origin}/api/v1/admin/media/${id}/content`;
  const start = Date.now();
  const delayed = await fetch(contentURL, { headers: { Cookie: cookie, Accept: 'application/x-media-test-head' }, signal: AbortSignal.timeout(50000) });
  expect(delayed.status).toBe(200);
  expect(Date.now() - start).toBeGreaterThanOrEqual(16000);
  expect(Buffer.from(await delayed.arrayBuffer()).toString()).toBe('slow upload\n');

  const idleStart = Date.now();
  let failed = false;
  try {
    const idle = await fetch(contentURL, { headers: { Cookie: cookie, Accept: 'application/x-media-test-idle' }, signal: AbortSignal.timeout(40000) });
    expect(idle.status).toBe(200);
    await idle.arrayBuffer();
  } catch {
    failed = true;
  }
  expect(failed, 'idle download must not silently complete').toBe(true);
  expect(Date.now() - idleStart).toBeGreaterThanOrEqual(14000);
  expect((await page.request.get(`/api/v1/admin/media/${id}/content`)).status()).toBe(200);
});

test('real Media absolute 45s, 60s, 90s deadlines', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'desktop', 'One full-clock deadline run is sufficient');
  test.setTimeout(255_000);
  await page.goto('/login');
  await page.getByLabel('用户名').fill(process.env.E2E_ADMIN_USER!);
  await page.getByLabel('密码', { exact: true }).fill(process.env.E2E_ADMIN_PASSWORD!);
  await page.getByRole('button', { name: '登录', exact: true }).click();
  await expect(page).toHaveURL(/\/admin$/);
  const origin = new URL(page.url()).origin;
  const csrf = (await (await page.request.get('/api/v1/auth/csrf')).json()).data.csrf_token as string;
  const cookie = (await page.context().cookies()).filter(c => c.name.startsWith('goadmin_')).map(c => `${c.name}=${c.value}`).join('; ');
  const boundary = 'media-absolute-timeout';
  const multipart = `--${boundary}\r\nContent-Disposition: form-data; name="file"; filename="timeout.txt"\r\nContent-Type: text/plain\r\n\r\nlong enough payload\n\r\n--${boundary}--\r\n`;
  const upload = await fetch(`${origin}/api/v1/admin/media`, { method: 'POST', headers: { Cookie: cookie, Origin: origin, 'X-CSRF-Token': csrf, 'Content-Type': `multipart/form-data; boundary=${boundary}` }, body: multipart });
  expect(upload.status).toBe(200);
  const id = (await upload.json()).data.id as string;
  const contentURL = `${origin}/api/v1/admin/media/${id}/content`;

  const firstStart = Date.now();
  const head = await fetch(contentURL, { headers: { Cookie: cookie, Accept: 'application/x-media-test-head-timeout' }, signal: AbortSignal.timeout(55000) });
  expect(head.status).toBe(503);
  expect(Date.now() - firstStart).toBeGreaterThanOrEqual(44000);
  expect(Date.now() - firstStart).toBeLessThan(55000);

  const before = (await (await page.request.get('/api/v1/admin/media')).json()).data.total as number;
  const uploadStart = Date.now();
  await new Promise<void>((resolve, reject) => {
    const req = httpRequest(new URL('/api/v1/admin/media', origin), { method: 'POST', headers: { Cookie: cookie, Origin: origin, 'X-CSRF-Token': csrf, 'Content-Type': `multipart/form-data; boundary=${boundary}` } }, res => {
      expect(res.statusCode).toBe(503);
      req.destroy(); resolve();
    });
    req.on('error', () => resolve());
    req.write(multipart.slice(0, multipart.indexOf('long enough')) + 'unfinished');
    setTimeout(() => { req.destroy(); reject(new Error('upload exceeded 65 seconds without timeout')); }, 65000).unref();
  });
  expect(Date.now() - uploadStart).toBeGreaterThanOrEqual(59000);
  expect((await (await page.request.get('/api/v1/admin/media')).json()).data.total).toBe(before);

  const totalStart = Date.now();
  let truncated = false;
  try {
    const total = await fetch(contentURL, { headers: { Cookie: cookie, Accept: 'application/x-media-test-total' }, signal: AbortSignal.timeout(100000) });
    expect(total.status).toBe(200);
    await total.arrayBuffer();
  } catch {
    truncated = true;
  }
  expect(truncated, '90s total must abort rather than complete the body').toBe(true);
  expect(Date.now() - totalStart).toBeGreaterThanOrEqual(89000);
  expect(Date.now() - totalStart).toBeLessThan(100000);
});
