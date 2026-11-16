import { expect, test } from '@playwright/test';

test.skip(process.env.E2E_LOGS !== '1', 'Requires isolated PostgreSQL and real Go API');

test('real login and operation history, safe filters, details, denial and responsive navigation', async ({ page, request, browser }, testInfo) => {
  expect((await request.get('/api/v1/admin/login-logs')).status()).toBe(401);
  await page.goto('/login');
  await page.getByLabel('用户名').fill(process.env.E2E_ADMIN_USER!);
  await page.getByLabel('密码', { exact: true }).fill('incorrect test password');
  await page.getByRole('button', { name: '登录', exact: true }).click();
  await expect(page).toHaveURL(/\/login/);
  await page.getByLabel('密码', { exact: true }).fill(process.env.E2E_ADMIN_PASSWORD!);
  await page.getByRole('button', { name: '登录', exact: true }).click();
  await expect(page).toHaveURL(/\/admin$/);

  await page.goto('/admin/login-logs');
  await expect(page.getByRole('heading', { name: '登录日志' })).toBeVisible();
  await expect(page.getByRole('navigation', { name: '管理导航' }).getByRole('link', { name: 'Login logs' })).toBeVisible();
  await expect(page.getByRole('region', { name: '登录记录' })).toContainText('success');
  await expect(page.getByRole('region', { name: '登录记录' })).toContainText('failure');
  await page.getByLabel('登录结果').selectOption('failure');
  await page.getByRole('button', { name: '筛选' }).click();
  await expect(page.getByRole('region', { name: '登录记录' })).toContainText('failure');
  await page.getByRole('button', { name: /查看记录 .* 详情/ }).first().click();
  await expect(page.getByRole('region', { name: '日志详情' })).toContainText('failure');
  await page.screenshot({ path: testInfo.outputPath('login-logs.png'), fullPage: true });

  await page.goto('/admin/audit-logs');
  await expect(page.getByRole('heading', { name: '操作审计' })).toBeVisible();
  await page.getByLabel('操作代码').fill('menu_initialized');
  await page.getByRole('button', { name: '筛选' }).click();
  await expect(page.getByRole('region', { name: '操作记录' })).toContainText('menu_initialized');
  const response = await page.request.get('/api/v1/admin/audit-logs?page_size=1000');
  expect(response.status()).toBe(400);
  await page.screenshot({ path: testInfo.outputPath('audit-logs.png'), fullPage: true });

  const suffix = `${Date.now()}_${testInfo.project.name}`.replace(/[^a-z0-9_]/gi, '').toLowerCase();
  const username = `logs_${suffix}`;
  const password = `dedicated logs E2E password ${suffix}`;
  const created = await page.evaluate(async ({ username, password }) => {
    const csrf = await fetch('/api/v1/auth/csrf', { credentials: 'same-origin' }).then(result => result.json());
    const response = await fetch('/api/v1/admin/users', {
      method: 'POST', credentials: 'same-origin',
      headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf.data.csrf_token },
      body: JSON.stringify({ username, email: `${username}@example.test`, password }),
    });
    return response.status;
  }, { username, password });
  expect(created).toBe(200);
  const memberContext = await browser.newContext();
  try {
    const member = await memberContext.newPage();
    await member.goto('/login');
    await member.getByLabel('用户名').fill(username);
    await member.getByLabel('密码', { exact: true }).fill(password);
    await member.getByRole('button', { name: '登录', exact: true }).click();
    await expect(member).toHaveURL(/\/admin$/);
    await member.goto('/admin/login-logs');
    await expect(member.getByText('没有访问此页面的权限')).toBeVisible();
    await expect(member.getByRole('navigation', { name: '管理导航' }).getByRole('link', { name: 'Login logs' })).toHaveCount(0);
    expect((await member.request.get(new URL('/api/v1/admin/login-logs', member.url()).href)).status()).toBe(403);
    expect((await member.request.get(new URL('/api/v1/admin/audit-logs', member.url()).href)).status()).toBe(403);
  } finally { await memberContext.close(); }
});
