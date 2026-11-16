import { expect, request, test } from '@playwright/test';

test.skip(process.env.E2E_STARTER !== '1', 'Requires fresh isolated PostgreSQL, Go API and Next.js');

test('real notifications, API keys and job queue through desktop/mobile browser', async ({ page, browser }, info) => {
  await page.goto('/login');
  await page.getByLabel('用户名').fill(process.env.E2E_ADMIN_USER!);
  await page.getByLabel('密码', { exact: true }).fill(process.env.E2E_ADMIN_PASSWORD!);
  await page.getByRole('button', { name: '登录', exact: true }).click();
  await expect(page).toHaveURL(/\/admin$/);
  const me = await page.request.get('/api/v1/auth/me').then(r => r.json());
  const userID = me.data.user.id as string;
  if (info.project.name === 'mobile') await page.getByRole('button', { name: '打开导航' }).click();
  const nav = page.getByRole('navigation', { name: '管理导航' });
  for (const name of ['Notifications', 'API Keys', 'Jobs']) await expect(nav.getByRole('link', { name })).toBeVisible();

  await page.goto('/admin/notifications');
  await page.getByPlaceholder('管理员 UUID').fill(userID);
  const noticeTitle = `Isolated E2E notice ${info.project.name} ${Date.now()}-${info.workerIndex}`;
  await page.getByLabel('标题').fill(noticeTitle);
  await page.getByLabel('正文').fill('Real PostgreSQL notification');
  await page.getByRole('button', { name: '发送' }).click();
  await expect(page.getByText(noticeTitle, { exact: true })).toBeVisible();
  const inbox = await page.request.get('/api/v1/admin/notifications').then(r => r.json());
  expect(inbox.data.items.find((item: {title:string}) => item.title===noticeTitle)?.read_at).toBeNull();
  const ownNotice = page.getByRole('listitem').filter({hasText:noticeTitle});
  await ownNotice.getByRole('button', { name: '标记已读' }).click();
  await expect(ownNotice.getByRole('button', { name: '标记已读' })).toHaveCount(0);

  await page.goto('/admin/api-keys');
  const keyName = `Browser E2E key ${info.project.name} ${Date.now()}-${info.workerIndex}`;
  await page.getByLabel('密钥名称').fill(keyName);
  await page.getByRole('button', { name: '创建' }).click();
  const raw = await page.getByRole('status').locator('code').textContent();
  expect(raw).toMatch(/^gak_[A-Za-z0-9_-]{43}$/);
  const apiBase = `http://127.0.0.1:${process.env.API_PORT}`;
  const keyHeaders = { Authorization: `Bearer ${raw}` };
  const direct = await request.newContext();
  try {
    expect((await direct.get(`${apiBase}/api/v1/key/dashboard/summary`, { headers: keyHeaders })).status()).toBe(200);
    expect((await direct.get(`${apiBase}/api/v1/admin/dashboard/summary`, { headers: keyHeaders })).status()).toBe(401);
    expect((await direct.get(`${apiBase}/api/v1/admin/jobs`, { headers: keyHeaders })).status()).toBe(401);
    // Unsafe methods are stopped by the global CSRF boundary before route matching.
    expect((await direct.post(`${apiBase}/api/v1/key/dashboard/summary`, { headers: keyHeaders })).status()).toBe(403);
    expect((await page.request.get('/api/v1/key/dashboard/summary', { headers: keyHeaders })).status()).toBe(404);
    await page.getByRole('button', { name: '已安全保存，关闭' }).click();
    page.once('dialog', dialog => dialog.accept());
    const ownKey = page.getByRole('listitem').filter({hasText:keyName});
    await ownKey.getByRole('button', { name: '撤销' }).click();
    await expect(ownKey.getByText(/已撤销/)).toBeVisible();
    expect((await direct.get(`${apiBase}/api/v1/key/dashboard/summary`, { headers: keyHeaders })).status()).toBe(401);
  } finally { await direct.dispose(); }

  await page.goto('/admin/jobs');
  await page.getByRole('button', { name: '提交任务' }).click();
  await expect(page.getByText('session_cleanup', { exact: true }).first()).toBeVisible();
  await page.waitForTimeout(2500);
  await page.getByRole('button', { name: '刷新任务' }).click();
  await expect(page.getByText(/已完成 · 第 1 次尝试/).first()).toBeVisible({ timeout: 12000 });
  await page.getByRole('button', { name: '详情' }).first().click();
  await expect(page.getByText(/第 1 次 · 已完成/)).toBeVisible();

  const csrf = await page.request.get('/api/v1/auth/csrf').then(r => r.json());
  const deniedCSRF = await page.request.post('/api/v1/admin/jobs', { headers: { Origin: new URL(page.url()).origin, 'X-CSRF-Token': 'wrong' }, data: { kind: 'session_cleanup' } });
  expect(deniedCSRF.status()).toBe(403);
  const headers = { Origin: new URL(page.url()).origin, 'X-CSRF-Token': csrf.data.csrf_token, 'Content-Type': 'application/json' };
  const viewerName = `starter_viewer_${info.project.name.toLowerCase()}_${Date.now()}_${info.workerIndex}`;
  const viewer = await page.request.post('/api/v1/admin/users', { headers, data: { username: viewerName, email: `${viewerName}@example.test`, display_name: 'Viewer', password: 'isolated starter viewer password' } });
  expect(viewer.status()).toBe(200);
  const context = await browser.newContext();
  try {
    const member = await context.newPage();
    await member.goto('/login');
    await member.getByLabel('用户名').fill(viewerName);
    await member.getByLabel('密码', { exact: true }).fill('isolated starter viewer password');
    await member.getByRole('button', { name: '登录', exact: true }).click();
    await expect(member).toHaveURL(/\/admin$/);
    for (const path of ['notifications', 'api-keys', 'jobs']) {
      expect((await member.request.get(`/api/v1/admin/${path}`)).status()).toBe(403);
    }
    await member.goto('/admin/jobs');
    await expect(member.getByText('没有访问此页面的权限')).toBeVisible();
  } finally { await context.close(); }
  await page.screenshot({ path: info.outputPath(`starter-${info.project.name}.png`), fullPage: true });
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth + 1)).toBe(true);
});
