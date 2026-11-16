import assert from 'node:assert/strict';
import { execFileSync } from 'node:child_process';
import { createRequire } from 'node:module';
import { mkdir } from 'node:fs/promises';
import { loadEnvFile } from 'node:process';

try { loadEnvFile(); } catch (error) { if (error.code !== 'ENOENT') throw error; }
const require = createRequire(new URL('../frontend/package.json', import.meta.url));
const { chromium } = require('@playwright/test');
const api = `http://127.0.0.1:${process.env.API_PORT ?? 8080}`;
const web = `http://127.0.0.1:${process.env.FRONTEND_PORT ?? 3000}`;
const docker = (...args) => execFileSync('docker', ['compose', ...args], { encoding: 'utf8', timeout: 60000 });
const marker = () => docker('exec', '-T', 'postgres', 'psql', '-U', process.env.POSTGRES_USER ?? 'goadmin', '-d', process.env.POSTGRES_DB ?? 'goadmin', '-Atc', "SELECT id FROM app_metadata WHERE component = 'core'").trim();
const status = async (base, path) => (await fetch(base + path, { signal: AbortSignal.timeout(10000) })).status;
const original = marker();
assert.match(original, /^[a-f\d-]{36}$/);
await mkdir('frontend/test-results/resilience', { recursive: true });
const browser = await chromium.launch({ channel: 'chrome' });
const page = await browser.newPage({ viewport: { width: 1440, height: 1000 } });
const { expect } = require('@playwright/test');
try {
  docker('stop', 'postgres');
  assert.equal(await status(api, '/api/v1/health/ready'), 503);
  assert.equal(await status(api, '/api/v1/health/live'), 200);
  assert.equal(await status(web, '/api/v1/health'), 503);
  await page.goto(web);
  await expect(page.getByTestId('overall-status')).toHaveText('基础服务尚未就绪');
  await page.screenshot({ path: 'frontend/test-results/resilience/database-down.png', fullPage: true });
  console.log('Database stopped: ready=503, live=200, Next proxy=503, UI unavailable.');
} finally {
  // Always restore this project's database, including after an assertion failure.
  try {
    docker('start', 'postgres');
    await expect.poll(() => status(api, '/api/v1/health/ready'), { timeout: 60000 }).toBe(200);
    assert.equal(marker(), original, 'foundation UUID must survive database restart');
    await page.goto(web);
    await expect(page.getByTestId('overall-status')).toHaveText('基础服务运行正常');
    await page.screenshot({ path: 'frontend/test-results/resilience/database-restored.png', fullPage: true });
    console.log('Database restored: readiness and UI recovered; original UUID persisted.');
  } finally {
    await browser.close();
  }
}
