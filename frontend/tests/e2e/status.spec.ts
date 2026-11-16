import { expect, test } from '@playwright/test';
test('real stack, refresh, theme, responsive layout and OpenAPI', async ({ page }, testInfo) => {
  const errors: string[] = [];
  page.on('pageerror', (error) => errors.push(error.message));
  await page.goto('/');
  await expect(page.getByTestId('overall-status')).toHaveText('基础服务运行正常');
  await expect(page.getByText('正常', { exact: true })).toHaveCount(3);
  await page.getByRole('button', { name: '刷新状态' }).click();
  await expect(page.getByTestId('overall-status')).toHaveText('基础服务运行正常');
  await expect(page.getByTestId('checked-at')).toContainText('最后检查');
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
  await page.screenshot({ path: testInfo.outputPath('status-light.png'), fullPage: true });
  await page.getByRole('button', { name: '切换主题' }).click();
  await expect(page.locator('html')).toHaveClass(/dark/);
  await page.screenshot({ path: testInfo.outputPath('status-dark.png'), fullPage: true });
  const api = await page.request.get('/api/v1/openapi.json');
  expect(api.status()).toBe(200);
  expect((await api.json()).openapi).toBe('3.0.3');
  expect(errors).toEqual([]);
});
test('unavailable UI recovers after a new successful check', async ({ page }) => {
  let unavailable = true;
  await page.route('**/api/v1/health', async (route) => {
    if (unavailable) await route.fulfill({ status: 503, contentType: 'application/json', body: JSON.stringify({ code: 50301, message: 'Service not ready', data: null }) });
    else await route.continue();
  });
  await page.goto('/');
  await expect(page.getByTestId('overall-status')).toHaveText('基础服务尚未就绪');
  await expect(page.getByText('未就绪', { exact: true })).toHaveCount(3);
  unavailable = false;
  await page.getByRole('button', { name: '刷新状态' }).click();
  await expect(page.getByTestId('overall-status')).toHaveText('基础服务运行正常');
});
