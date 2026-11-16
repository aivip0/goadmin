import { expect, test } from '@playwright/test';

test.skip(process.env.E2E_AUTH !== '1', 'Requires actual Go API and isolated PostgreSQL database with one-time initialized admin');

test('real administrator login, cookie, refresh, revoke and old-token rejection', async ({ page, context }, testInfo) => {
  await page.goto('/admin');
  await expect(page).toHaveURL(/\/login$/);
  await expect(page.getByRole('heading', { name: '登录管理后台' })).toBeVisible();
  await page.screenshot({ path: `/private/tmp/goadmin-phase22-login-${testInfo.project.name}.png` });

  await page.getByLabel('用户名').fill(process.env.E2E_ADMIN_USER!);
  await page.getByLabel('密码', { exact: true }).fill('definitely-not-the-password');
  await page.getByRole('button', { name: '登录', exact: true }).click();
  await expect(page.getByText('用户名或密码不正确。')).toBeVisible();

  const issued = await page.evaluate(async () => (await (await fetch('/api/v1/auth/csrf')).json()).data.csrf_token as string);
  const wrongCSRF = await page.evaluate(async () => (await fetch('/api/v1/auth/login', { method: 'POST', headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': 'wrong' }, body: '{}' })).status);
  expect(wrongCSRF).toBe(403);
  const invalidOrigin = await page.request.post('/api/v1/auth/login', { headers: { Origin: 'https://invalid.example.test', 'X-CSRF-Token': issued }, data: '{}' });
  expect(invalidOrigin.status()).toBe(403);

  await page.getByLabel('密码', { exact: true }).fill(process.env.E2E_ADMIN_PASSWORD!);
  await page.getByRole('button', { name: '登录', exact: true }).click();
  await expect(page).toHaveURL(/\/admin$/);
  await expect(page.getByText('已连接管理员账号')).toBeVisible();
  const cookie = (await context.cookies()).find((item) => item.name === 'goadmin_session');
  expect(cookie?.httpOnly).toBe(true);
  expect(cookie?.value).toHaveLength(43);
  expect(await page.evaluate(() => document.cookie)).not.toContain('goadmin_session');
  const me = await page.request.get('/api/v1/auth/me');
  expect(me.ok()).toBe(true);
  const payload = await me.json();
  expect(payload.data.user.username).toBe(process.env.E2E_ADMIN_USER);
  expect(JSON.stringify(payload)).not.toContain(cookie!.value);
  await page.reload();
  await expect(page.getByText('已连接管理员账号')).toBeVisible();
  await page.screenshot({ path: `/private/tmp/goadmin-phase22-admin-${testInfo.project.name}.png` });

  await page.getByRole('button', { name: '退出登录' }).click();
  await expect(page).toHaveURL(/\/login$/);
  const replay = await page.request.get('/api/v1/auth/me', { headers: { Cookie: `goadmin_session=${cookie!.value}` } });
  expect(replay.status()).toBe(401);
  await page.goto('/admin');
  await expect(page).toHaveURL(/\/login$/);

  // A second session verifies logout-all invalidates every device, not only its cookie.
  await page.getByLabel('用户名').fill(process.env.E2E_ADMIN_USER!);
  await page.getByLabel('密码', { exact: true }).fill(process.env.E2E_ADMIN_PASSWORD!);
  await page.getByRole('button', { name: '登录', exact: true }).click();
  await expect(page).toHaveURL(/\/admin$/);
  const second = (await context.cookies()).find((item) => item.name === 'goadmin_session')!.value;
  const other = await context.browser()!.newContext();
  try {
    await other.addCookies([{ name: 'goadmin_session', value: second, url: new URL(page.url()).origin, httpOnly: true }]);
    await page.getByRole('button', { name: '退出所有设备' }).click();
    await expect(page).toHaveURL(/\/login$/);
    expect((await other.request.get(new URL('/api/v1/auth/me', page.url()).href)).status()).toBe(401);
  } finally { await other.close(); }
});

test('real RBAC administration, immediate revocation, disabled sessions and super-admin invariant', async ({ page, browser }, testInfo) => {
  test.skip(process.env.E2E_RBAC !== '1', 'Requires an isolated PostgreSQL database and real Go API');
  const suffix = `${Date.now()}_${testInfo.project.name}`.replace(/[^a-z0-9_]/gi, '').toLowerCase();
  const username = `rbac_${suffix}`;
  const email = `${username}@example.test`;
  const password = 'dedicated real RBAC test password';
  await page.goto('/login');
  await page.getByLabel('用户名').fill(process.env.E2E_ADMIN_USER!);
  await page.getByLabel('密码', { exact: true }).fill(process.env.E2E_ADMIN_PASSWORD!);
  await page.getByRole('button', { name: '登录', exact: true }).click();
  await expect(page).toHaveURL(/\/admin$/);

  await page.goto('/admin/users');
  await page.getByRole('button', { name: '新建用户' }).click();
  const createPanel = page.getByRole('region', { name: '新建用户' });
  await createPanel.getByLabel('用户名').fill(username);
  await createPanel.getByLabel('邮箱').fill(email);
  await createPanel.getByLabel('姓名').fill('RBAC E2E');
  await createPanel.getByLabel(/初始密码/).fill(password);
  await createPanel.getByRole('button', { name: '创建' }).click();
  await expect(page.getByRole('heading', { name: `${username} · 用户详情` })).toBeVisible();

  await page.goto('/admin/roles');
  await page.getByRole('button', { name: '新建角色' }).click();
  const rolePanel = page.locator('section.admin-panel').first();
  await rolePanel.getByLabel('代码').fill(`e2e_${suffix}`);
  const roleName = `E2E Users Viewer ${testInfo.project.name}`;
  await rolePanel.getByLabel('名称').fill(roleName);
  await rolePanel.getByRole('button', { name: '创建' }).click();
  await expect(page.getByRole('heading', { name: `${roleName} · 角色详情` })).toBeVisible();
  await page.getByLabel('users:view').check();
  page.once('dialog', dialog => dialog.accept());
  await page.getByRole('button', { name: '保存权限' }).click();
  await expect(page.getByText('角色权限已更新。')).toBeVisible();

  await page.goto('/admin/users');
  const row = page.getByRole('row').filter({ hasText: username });
  await row.getByRole('button', { name: '详情' }).click();
  await page.getByRole('region', { name: '用户详情' }).getByRole('checkbox', { name: roleName, exact: true }).check();
  page.once('dialog', dialog => dialog.accept());
  await page.getByRole('button', { name: '保存角色' }).click();
  await expect(page.getByText('用户角色已更新。')).toBeVisible();

  const restricted = await browser.newContext();
  const member = await restricted.newPage();
  try {
    await member.goto('/login');
    await member.getByLabel('用户名').fill(username);
    await member.getByLabel('密码', { exact: true }).fill(password);
    await member.getByRole('button', { name: '登录', exact: true }).click();
    await expect(member).toHaveURL(/\/admin$/);
    await member.goto('/admin/users');
    await expect(member.getByRole('heading', { name: '管理员用户' })).toBeVisible();
    await member.goto('/admin/roles');
    await expect(member.getByText('没有访问此页面的权限。')).toBeVisible();
    expect((await member.request.get(new URL('/api/v1/admin/roles', member.url()).href)).status()).toBe(403);

    const superContext = await browser.newContext();
    try {
      const admin = await superContext.newPage();
      await admin.goto('/login');
      await admin.getByLabel('用户名').fill(process.env.E2E_ADMIN_USER!);
      await admin.getByLabel('密码', { exact: true }).fill(process.env.E2E_ADMIN_PASSWORD!);
      await admin.getByRole('button', { name: '登录', exact: true }).click();
      await expect(admin).toHaveURL(/\/admin$/);
      await admin.goto('/admin/roles');
      const roleRow = admin.getByRole('row').filter({ hasText: `e2e_${suffix}` });
      await roleRow.getByRole('button', { name: '详情' }).click();
      await admin.getByLabel('users:view').uncheck();
      admin.once('dialog', dialog => dialog.accept());
      await admin.getByRole('button', { name: '保存权限' }).click();
      await expect(admin.getByText('角色权限已更新。')).toBeVisible();
      expect((await member.request.get(new URL('/api/v1/admin/users', member.url()).href)).status()).toBe(403);
      await admin.goto('/admin/users');
      const memberRow = admin.getByRole('row').filter({ hasText: username });
      await memberRow.getByRole('button', { name: '详情' }).click();
      admin.once('dialog', dialog => dialog.accept());
      await admin.getByRole('button', { name: '停用' }).click();
      await expect(admin.getByRole('combobox', { name: '状态' })).toBeVisible();
      await admin.getByRole('combobox', { name: '状态' }).selectOption('disabled');
      await expect(admin.getByRole('row').filter({ hasText: username })).toContainText('停用');
      expect((await member.request.get(new URL('/api/v1/auth/me', member.url()).href)).status()).toBe(401);
      const rootResponse = await admin.request.get(new URL('/api/v1/auth/me', admin.url()).href);
      const rootId = (await rootResponse.json()).data.user.id as string;
      const lastSuper = await admin.evaluate(async (id: string) => {
        const tokenResponse = await fetch('/api/v1/auth/csrf');
        const token = (await tokenResponse.json()).data.csrf_token as string;
        const response = await fetch(`/api/v1/admin/users/${id}/disable`, { method: 'POST', headers: { 'X-CSRF-Token': token } });
        return response.status;
      }, rootId);
      expect(lastSuper).toBe(409);
    } finally { await superContext.close(); }
  } finally { await restricted.close(); }
});
