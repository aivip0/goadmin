import { expect, test, type Browser, type Page } from '@playwright/test';

test.skip(process.env.E2E_PROFILE !== '1', 'Requires isolated PostgreSQL and real Go API');

async function login(page: Page, username: string, password: string) {
  await page.goto('/login');
  await page.getByLabel('用户名').fill(username);
  await page.getByLabel('密码', { exact: true }).fill(password);
  await page.getByRole('button', { name: '登录', exact: true }).click();
  await expect(page).toHaveURL(/\/admin$/);
}

async function createSession(browser: Browser, username: string, password: string) {
  const context = await browser.newContext();
  const page = await context.newPage();
  await login(page, username, password);
  return { context, page };
}

test('self-service profile, CSRF, session ownership/revocation and password lifecycle', async ({ page, browser }, testInfo) => {
  const rootUser = process.env.E2E_ADMIN_USER!;
  const rootPassword = process.env.E2E_ADMIN_PASSWORD!;
  await login(page, rootUser, rootPassword);
  const suffix = `${Date.now()}_${testInfo.project.name}`.replace(/[^a-z0-9_]/gi, '').toLowerCase();
  const username = `profile_${suffix}`;
  const initialPassword = `dedicated profile E2E password ${suffix}`;
  const created = await page.evaluate(async ({ username, password }) => {
    const csrf = await fetch('/api/v1/auth/csrf').then(response => response.json());
    const result = await fetch('/api/v1/admin/users', {
      method: 'POST', headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf.data.csrf_token },
      body: JSON.stringify({ username, email: `${username}@example.test`, password }),
    });
    return result.status;
  }, { username, password: initialPassword });
  expect(created).toBe(200);

  const mainContext = await browser.newContext();
  const main = await mainContext.newPage();
  let second: Awaited<ReturnType<typeof createSession>> | undefined;
  let third: Awaited<ReturnType<typeof createSession>> | undefined;
  try {
    await login(main, username, initialPassword);
    await main.goto('/admin/profile');
    await expect(main.getByRole('heading', { name: '个人资料' })).toBeVisible();
    const cookie = (await mainContext.cookies()).find(item => item.name === 'goadmin_session');
    expect(cookie?.httpOnly).toBe(true);
    expect(await main.evaluate(() => document.cookie)).not.toContain('goadmin_session');
    const profileResponse = await main.request.get(new URL('/api/v1/admin/profile', main.url()).href);
    expect(profileResponse.status()).toBe(200);
    const profileJSON = await profileResponse.json();
    expect(profileJSON.data.user.username).toBe(username);
    expect(JSON.stringify(profileJSON)).not.toContain(initialPassword);

    const csrf = await main.evaluate(async () => (await (await fetch('/api/v1/auth/csrf')).json()).data.csrf_token as string);
    const badCSRF = await main.request.put(new URL('/api/v1/admin/profile', main.url()).href, { headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': `${csrf}invalid` }, data: { email: 'bad@example.test', display_name: 'Bad' } });
    expect(badCSRF.status()).toBe(403);

    await main.getByLabel('显示名称').fill('Profile Updated');
    await main.getByLabel('邮箱').fill(`${username}@updated.example.test`);
    await main.getByRole('button', { name: '保存资料' }).click();
    await expect(main.getByRole('status')).toContainText('个人资料已更新');

    second = await createSession(browser, username, initialPassword);
    third = await createSession(browser, username, initialPassword);
    const sessionList = await main.request.get(new URL('/api/v1/admin/profile/sessions', main.url()).href);
    const sessions = (await sessionList.json()).data.items as { id: string; current: boolean }[];
    expect(sessions.filter(item => item.current)).toHaveLength(1);
    expect(sessions.length).toBe(3);
    const secondSessions = await second.page.request.get(new URL('/api/v1/admin/profile/sessions', second.page.url()).href);
    const secondSessionID = ((await secondSessions.json()).data.items as { id: string; current: boolean }[]).find(item => item.current)!.id;
    const revokeSelected = await main.evaluate(async ({ id }) => {
      const token = (await (await fetch('/api/v1/auth/csrf')).json()).data.csrf_token;
      return fetch(`/api/v1/admin/profile/sessions/${id}`, { method: 'DELETE', headers: { 'X-CSRF-Token': token } }).then(response => response.json());
    }, { id: secondSessionID });
    expect(revokeSelected.data.current_revoked).toBe(false);
    const secondMe = await second.page.request.get(new URL('/api/v1/auth/me', second.page.url()).href);
    expect(secondMe.status()).toBe(401);
    await second.context.close(); second = undefined;

    main.once('dialog', dialog => dialog.accept());
    await main.getByRole('button', { name: '撤销其他会话' }).click();
    await expect(main.getByRole('status')).toContainText('其他会话已撤销');
    const thirdMe = await third.page.request.get(new URL('/api/v1/auth/me', third.page.url()).href);
    expect(thirdMe.status()).toBe(401);
    await third.context.close(); third = undefined;

    await main.getByLabel('当前密码').fill('incorrect current password');
    await main.getByLabel('新密码', { exact: true }).fill('new dedicated profile E2E password');
    await main.getByLabel('确认新密码', { exact: true }).fill('new dedicated profile E2E password');
    await main.getByRole('button', { name: '更新密码' }).click();
    await expect(main.getByRole('alert').first()).toBeVisible();
    await expect(main).toHaveURL(/\/admin\/profile/);
    await main.getByLabel('当前密码').fill(initialPassword);
    await main.getByRole('button', { name: '更新密码' }).click();
    await expect(main).toHaveURL(/\/login\?reason=password-changed/);
    const staleMe = await main.request.get(new URL('/api/v1/auth/me', main.url()).href);
    expect(staleMe.status()).toBe(401);
    await login(main, username, 'new dedicated profile E2E password');
    await main.goto('/admin/profile');
    await expect(main.getByRole('heading', { name: '个人资料' })).toBeVisible();
    await main.screenshot({ path: testInfo.outputPath('profile.png'), fullPage: true });
    const dimensions = await main.evaluate(() => ({ scroll: document.documentElement.scrollWidth, client: document.documentElement.clientWidth }));
    expect(dimensions.scroll).toBeLessThanOrEqual(dimensions.client + 1);
  } finally {
    if (second) await second.context.close();
    if (third) await third.context.close();
    await mainContext.close();
  }
});
