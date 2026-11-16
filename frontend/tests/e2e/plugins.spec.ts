import { expect, request, test } from '@playwright/test';

test.skip(process.env.E2E_PLUGINS !== '1', 'Requires isolated real PostgreSQL, Go API and initialized administrator');

test('real plugin management API and responsive admin surface', async ({ page }, testInfo) => {
  await page.goto('/login');
  await page.getByLabel('用户名').fill(process.env.E2E_ADMIN_USER!);
  await page.getByLabel('密码', { exact: true }).fill(process.env.E2E_ADMIN_PASSWORD!);
  await page.getByRole('button', { name: '登录', exact: true }).click();
  await expect(page).toHaveURL(/\/admin$/);

  const pluginList = await page.request.get('/api/v1/admin/plugins');
  const packages = await page.request.get('/api/v1/admin/plugins/packages');
  const events = await page.request.get('/api/v1/admin/plugins/events');
  expect(pluginList.status()).toBe(200);
  expect(packages.status()).toBe(200);
  expect(events.status()).toBe(200);

  await page.goto('/admin/plugins');
  await expect(page.getByRole('heading', { name: '插件管理' })).toBeVisible();
  await expect(page.getByText('没有已安装的插件。')).toBeVisible();
  await page.getByRole('tab', { name: '本地可信包' }).click();
  await expect(page.getByText('可信包目录中没有可验证的插件包。')).toBeVisible();
  await page.getByRole('tab', { name: '事件记录' }).click();
  await expect(page.getByText('暂无插件事件。')).toBeVisible();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth + 1)).toBe(true);
  await page.screenshot({ path: testInfo.outputPath(`plugins-${testInfo.project.name}.png`), fullPage: true });

  const unauthenticated = await request.newContext();
  try {
    expect((await unauthenticated.get(new URL('/api/v1/admin/plugins', page.url()).href)).status()).toBe(401);
  } finally { await unauthenticated.dispose(); }
});
