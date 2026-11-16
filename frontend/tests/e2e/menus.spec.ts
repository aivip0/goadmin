import { expect, test } from '@playwright/test';

test.skip(process.env.E2E_MENUS !== '1', 'Requires real Go API and an isolated PostgreSQL database');

test('real menu CRUD, RBAC navigation, revocation and disabled menu in desktop and mobile browsers', async ({ page, browser }, testInfo) => {
  const suffix = `${Date.now()}_${testInfo.project.name}`.replace(/[^a-z0-9_]/gi, '').toLowerCase();
  const roleCode = `m24_${suffix}`;
  const roleName = `Menu Viewer ${testInfo.project.name}`;
  const username = `m24_${suffix}`;
  const password = `dedicated menu E2E password ${suffix}`;
  const directoryName = `E2E Operations ${suffix}`;
  const pageName = `E2E User Directory ${suffix}`;

  await page.goto('/login');
  await page.getByLabel('用户名').fill(process.env.E2E_ADMIN_USER!);
  await page.getByLabel('密码', { exact: true }).fill(process.env.E2E_ADMIN_PASSWORD!);
  await page.getByRole('button', { name: '登录', exact: true }).click();
  await expect(page).toHaveURL(/\/admin$/);

  const api = async <T,>(method: string, path: string, payload?: unknown): Promise<T> => {
    const response = await page.evaluate(async ({ method, path, payload }) => {
      const csrf = await fetch('/api/v1/auth/csrf', { credentials: 'same-origin' }).then(r => r.json());
      const result = await fetch(`/api/v1/admin/${path}`, {
        method,
        credentials: 'same-origin',
        headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf.data.csrf_token },
        body: payload === undefined ? undefined : JSON.stringify(payload),
      });
      return { status: result.status, body: await result.json() };
    }, { method, path, payload });
    expect(response.status, `${method} ${path}`).toBeLessThan(300);
    expect(response.body.code, `${method} ${path}`).toBe(0);
    return response.body.data as T;
  };

  const role = await api<{ id: string }>('POST', 'roles', { code: roleCode, name: roleName, description: 'Phase 2.4 browser test role' });
  const catalog = await api<Array<{ id: string; code: string }>>('GET', 'permissions');
  const usersView = catalog.find(item => item.code === 'users:view');
  expect(usersView).toBeTruthy();
  await api('PUT', `roles/${role.id}/permissions`, { permission_ids: [usersView!.id] });
  const user = await api<{ id: string }>('POST', 'users', { username, email: `${username}@example.test`, display_name: 'Menu E2E', password });
  await api('PUT', `users/${user.id}/roles`, { role_ids: [role.id] });

  await page.goto('/admin/menus');
  await expect(page.getByRole('heading', { name: '菜单管理' })).toBeVisible();
  const panel = page.getByRole('region', { name: '新建菜单' });
  await page.getByRole('button', { name: '新建菜单' }).click();
  await panel.getByLabel('菜单类型').selectOption('directory');
  await panel.getByLabel('名称').fill(directoryName);
  await panel.getByLabel('唯一代码').fill(`m24_dir_${suffix}`);
  await panel.getByRole('group', { name: '分配角色' }).getByRole('checkbox', { name: new RegExp(roleName) }).check();
  await panel.getByRole('button', { name: '保存菜单' }).click();
  await expect(page.getByText('菜单已创建。')).toBeVisible();

  await page.getByRole('button', { name: '新建菜单' }).click();
  const pagePanel = page.getByRole('region', { name: '新建菜单' });
  await pagePanel.getByLabel('菜单类型').selectOption('page');
  await pagePanel.getByLabel('名称').fill(pageName);
  await pagePanel.getByLabel('唯一代码').fill(`m24_page_${suffix}`);
  await expect(pagePanel.getByLabel('父级目录').locator('option', { hasText: directoryName })).toHaveCount(1);
  await pagePanel.getByLabel('父级目录').selectOption({ label: directoryName });
  await pagePanel.getByLabel('受控页面').selectOption('users');
  await expect(pagePanel.getByLabel('访问权限')).toHaveValue('users:view');
  await pagePanel.getByRole('group', { name: '分配角色' }).getByRole('checkbox', { name: new RegExp(roleName) }).check();
  await pagePanel.getByRole('button', { name: '保存菜单' }).click();
  await expect(page.getByText('菜单已创建。')).toBeVisible();

  const memberContext = await browser.newContext();
  const member = await memberContext.newPage();
  try {
    await member.goto('/login');
    await member.getByLabel('用户名').fill(username);
    await member.getByLabel('密码', { exact: true }).fill(password);
    await member.getByRole('button', { name: '登录', exact: true }).click();
    await expect(member).toHaveURL(/\/admin$/);
    const openMemberNavigation = async () => {
      const toggle = member.getByRole('button', { name: '打开导航' });
      if (await toggle.isVisible()) await toggle.click();
    };
    await openMemberNavigation();
    const nav = member.getByRole('navigation', { name: '管理导航' });
    await expect(nav.getByRole('button', { name: directoryName })).toBeVisible();
    await expect(nav.getByRole('link', { name: pageName })).toBeVisible();
    await expect(nav.getByRole('link')).toHaveCount(1);
    await nav.getByRole('link', { name: pageName }).click();
    await expect(member).toHaveURL(/\/admin\/users$/);
    await expect(member.getByRole('heading', { name: '管理员用户' })).toBeVisible();

    await member.goto('/admin/roles');
    await expect(member.getByText('没有访问此页面的权限')).toBeVisible();
    const denied = await member.request.get(new URL('/api/v1/admin/menus', member.url()).href);
    expect(denied.status()).toBe(403);

    await page.getByRole('button', { name: `编辑 ${pageName}` }).click();
    const detail = page.getByRole('region', { name: '菜单详情' });
    await detail.getByRole('group', { name: '分配角色' }).getByRole('checkbox', { name: new RegExp(roleName) }).uncheck();
    page.once('dialog', dialog => dialog.accept());
    await detail.getByRole('button', { name: '单独保存角色授权' }).click();
    await expect(page.getByText('菜单角色授权已更新。')).toBeVisible();
    await member.reload();
    await openMemberNavigation();
    await expect(nav.getByRole('link', { name: pageName })).toHaveCount(0);
    await expect(nav.getByRole('button', { name: directoryName })).toHaveCount(0);

    await page.getByRole('button', { name: `编辑 ${pageName}` }).click();
    const updated = page.getByRole('region', { name: '菜单详情' });
    await updated.getByRole('group', { name: '分配角色' }).getByRole('checkbox', { name: new RegExp(roleName) }).check();
    page.once('dialog', dialog => dialog.accept());
    await updated.getByRole('button', { name: '单独保存角色授权' }).click();
    await expect(page.getByText('菜单角色授权已更新。')).toBeVisible();
    await page.getByRole('button', { name: `编辑 ${pageName}` }).click();
    await page.getByLabel('启用状态').selectOption('disabled');
    await page.getByRole('button', { name: '保存菜单' }).click();
    await expect(page.getByText('菜单已更新。')).toBeVisible();
    await member.reload();
    await openMemberNavigation();
    await expect(nav.getByRole('link', { name: pageName })).toHaveCount(0);
  } finally {
    await memberContext.close();
  }
  await page.screenshot({ path: testInfo.outputPath('menus-admin.png'), fullPage: true });
});
