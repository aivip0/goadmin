import { expect, test } from '@playwright/test';
import { crc32, deflateSync } from 'node:zlib';

function pngChunk(type: string, data: Buffer): Buffer {
  const length = Buffer.alloc(4);
  length.writeUInt32BE(data.length);
  const content = Buffer.concat([Buffer.from(type), data]);
  const checksum = Buffer.alloc(4);
  checksum.writeUInt32BE(crc32(content));
  return Buffer.concat([length, content, checksum]);
}

function validPNG(): Buffer {
  const ihdr = Buffer.alloc(13);
  ihdr.writeUInt32BE(1, 0);
  ihdr.writeUInt32BE(1, 4);
  ihdr[8] = 8;
  ihdr[9] = 2;
  return Buffer.concat([
    Buffer.from('89504e470d0a1a0a', 'hex'),
    pngChunk('IHDR', ihdr),
    pngChunk('IDAT', deflateSync(Buffer.from([0, 255, 0, 0]))),
    pngChunk('IEND', Buffer.alloc(0)),
  ]);
}

test.skip(process.env.E2E_MEDIA !== '1', 'Requires isolated PostgreSQL, Go API and private media root');

test('private media upload, preview, download, CSRF, and deletion on desktop and mobile', async ({ page, browser }, testInfo) => {
  const anonymous = await browser.newContext({ baseURL: process.env.E2E_BASE_URL });
  const unauthenticated = await anonymous.request.get('/api/v1/admin/media');
  expect(unauthenticated.status()).toBe(401);
  await anonymous.close();
  await page.goto('/login');
  await page.getByLabel('用户名').fill(process.env.E2E_ADMIN_USER!);
  await page.getByLabel('密码', { exact: true }).fill(process.env.E2E_ADMIN_PASSWORD!);
  await page.getByRole('button', { name: '登录', exact: true }).click();
  await expect(page).toHaveURL(/\/admin$/);
  await page.goto('/admin/media');
  await expect(page.getByRole('heading', { name: '媒体库' })).toBeVisible();
  await expect(page.getByRole('navigation', { name: '管理导航' }).getByRole('link', { name: 'Media' })).toBeVisible();
  const filename = `media-${testInfo.project.name}.txt`;
  const content = Buffer.from(`private media for ${testInfo.project.name}\n`);
  await page.getByLabel('选择文件').setInputFiles({ name: filename, mimeType: 'text/plain', buffer: content });
  await page.getByRole('button', { name: '上传', exact: true }).click();
  await expect(page.getByRole('status').filter({ hasText: '文件已保存。' })).toBeVisible();
  await expect(page.getByRole('heading', { name: filename })).toBeVisible();
  const list = await page.request.get('/api/v1/admin/media?type=document&page=1&page_size=25');
  expect(list.status()).toBe(200);
  const uploaded = (await list.json()).data.items.find((item: { original_name: string }) => item.original_name === filename);
  expect(uploaded.id).toMatch(/^[0-9a-f-]{36}$/);
  const stream = await page.request.get(`/api/v1/admin/media/${uploaded.id}/content?download=1`);
  expect(stream.status()).toBe(200);
  expect(stream.headers()['content-disposition']).toContain('attachment');
  expect(stream.headers()['x-content-type-options']).toBe('nosniff');
  expect(await stream.body()).toEqual(content);
  const range = await page.request.get(`/api/v1/admin/media/${uploaded.id}/content`, { headers: { Range: 'bytes=0-1' } });
  expect(range.status()).toBe(416);
  const origin = new URL(page.url()).origin;
  const noCSRF = await page.request.delete(`/api/v1/admin/media/${uploaded.id}`, { headers: { origin } });
  expect(noCSRF.status()).toBe(403);
  const csrfInvalid = (await (await page.request.get('/api/v1/auth/csrf')).json()).data.csrf_token as string;
  const invalid = await page.request.post('/api/v1/admin/media', { multipart: { file: { name: 'unsafe.svg', mimeType: 'image/svg+xml', buffer: Buffer.from('<svg></svg>') } }, headers: { origin, 'X-CSRF-Token': csrfInvalid } });
  expect(invalid.status()).toBe(415);
  const png = validPNG();
  await page.getByLabel('选择文件').setInputFiles({ name: 'sample.png', mimeType: 'image/png', buffer: png });
  await page.getByRole('button', { name: '上传', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'sample.png' })).toBeVisible();
  await page.getByRole('button', { name: '预览' }).click();
  await expect(page.getByRole('dialog', { name: '预览 sample.png' })).toBeVisible();
  await page.getByRole('button', { name: '关闭预览' }).click();
  const image = (await (await page.request.get('/api/v1/admin/media?type=image')).json()).data.items[0];
  const inline = await page.request.get(`/api/v1/admin/media/${image.id}/content`);
  expect(inline.status()).toBe(200);
  expect(inline.headers()['content-disposition']).toContain('inline');
  expect(await inline.body()).toEqual(png);
  await page.goto('/admin/settings');
  await page.getByLabel('从媒体库选择图片').selectOption(`/api/v1/admin/media/${image.id}/content`);
  await page.getByRole('button', { name: '保存更改', exact: true }).click();
  await expect(page.getByRole('status').filter({ hasText: '系统设置已保存。' })).toBeVisible();
  const csrf = (await (await page.request.get('/api/v1/auth/csrf')).json()).data.csrf_token as string;
  const referenced = await page.request.delete(`/api/v1/admin/media/${image.id}`, { headers: { origin, 'X-CSRF-Token': csrf } });
  expect(referenced.status()).toBe(409);
  await page.getByLabel('Logo 同源路径').fill('');
  await page.getByRole('button', { name: '保存更改', exact: true }).click();
  await page.goto('/admin/media');
  page.once('dialog', dialog => dialog.accept());
  await page.getByRole('article').filter({ hasText: 'sample.png' }).getByRole('button', { name: '删除' }).click();
  await expect(page.getByRole('heading', { name: 'sample.png' })).toHaveCount(0);
  page.once('dialog', dialog => dialog.accept());
  await page.getByRole('article').filter({ hasText: filename }).getByRole('button', { name: '删除' }).click();
  await expect(page.getByRole('status').filter({ hasText: '文件已删除。' })).toBeVisible();
  expect((await page.request.get(`/api/v1/admin/media/${uploaded.id}/content`)).status()).toBe(404);
  await page.screenshot({ path: testInfo.outputPath('media.png'), fullPage: true });
});
