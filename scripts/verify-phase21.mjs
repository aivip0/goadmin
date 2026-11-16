import { spawn, execFileSync } from 'node:child_process';
import { once } from 'node:events';
import { createServer } from 'node:net';
import { createServer as createHttpServer, request as httpRequest } from 'node:http';
import { randomBytes } from 'node:crypto';
import { mkdtempSync, rmSync, chmodSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { setTimeout as delay } from 'node:timers/promises';
import { fileURLToPath } from 'node:url';

const root = fileURLToPath(new URL('../', import.meta.url));
try { process.loadEnvFile(`${root}.env`); } catch (error) { if (error.code !== 'ENOENT') throw error; }
const auth = process.env.E2E_AUTH === '1';
const dashboard = process.env.E2E_DASHBOARD === '1';
const settings = process.env.E2E_SETTINGS === '1';
const media = process.env.E2E_MEDIA === '1';
const mediaHTTP = process.env.E2E_MEDIA_HTTP === '1';
const logs = process.env.E2E_LOGS === '1';
const profile = process.env.E2E_PROFILE === '1';
const articles = process.env.E2E_ARTICLES === '1';
const starter = process.env.E2E_STARTER === '1';
const freshInit = process.env.E2E_FRESH_INIT === '1';
const project = process.env.E2E_PROJECT;
if (project && project !== 'desktop' && project !== 'mobile') throw new Error('E2E_PROJECT must be desktop or mobile');
const phaseCode = starter ? '40' : articles ? '36' : profile ? '35' : logs ? '34' : media ? '33' : settings ? '32' : dashboard ? '31' : process.env.E2E_MENUS === '1' ? '24' : process.env.E2E_RBAC === '1' ? '23' : auth ? '22' : '21';
const phase = starter ? 'starter' : articles ? '3.6' : profile ? '3.5' : logs ? '3.4' : media ? '3.3' : settings ? '3.2' : dashboard ? '3.1' : process.env.E2E_MENUS === '1' ? '2.4' : process.env.E2E_RBAC === '1' ? '2.3' : auth ? '2.2' : '2.1';
const name = `phase${phaseCode}_e2e_${randomBytes(8).toString('hex')}`;
if (process.env.E2E_DB_ISOLATED !== '1') throw new Error('E2E_DB_ISOLATED=1 is required; refusing to use an unspecified database');
const container = process.env.E2E_DB_CONTAINER;
const user = process.env.POSTGRES_USER;
const adminDatabase = process.env.POSTGRES_DB;
if (!container || !user || !adminDatabase) throw new Error('E2E_DB_CONTAINER, POSTGRES_USER and POSTGRES_DB are required for isolated E2E');
const isolationLabel = execFileSync('docker', ['inspect', '-f', '{{ index .Config.Labels "goadmin.test.isolated" }}', container], { encoding: 'utf8' }).trim();
if (isolationLabel !== 'true') throw new Error(`Refusing E2E database container ${container}: missing goadmin.test.isolated=true`);
const sql = (statement, database = adminDatabase) => execFileSync('docker', ['exec', container, 'psql', '-At', '-v', 'ON_ERROR_STOP=1', '-U', user, '-d', database, '-c', statement], { stdio: ['ignore', 'pipe', 'pipe'] });
const children = [];
let delayedProxy;
const binDir = mkdtempSync(`${tmpdir()}/goadmin-phase${phaseCode}-bin-`);
const mediaRoot = media ? mkdtempSync(`${tmpdir()}/goadmin-phase33-media-`) : null;
if (mediaRoot) chmodSync(mediaRoot, 0o700);
async function port() {
  const listener = createServer();
  listener.listen(0, '127.0.0.1'); await once(listener, 'listening');
  const value = listener.address().port;
  await new Promise((resolve) => listener.close(resolve));
  return value;
}
async function start(command, args, env, cwd) {
  const child = spawn(command, args, { cwd, env, stdio: ['ignore', 'pipe', 'pipe'] });
  children.push(child);
  // Server logs are deliberately not persisted: test traffic includes CSRF tokens.
  child.stdout.resume(); child.stderr.resume();
  child.on('error', () => {});
  return child;
}
async function ready(url, child) {
  for (let i = 0; i < 150; i++) {
    if (child.exitCode !== null || child.signalCode !== null) throw new Error('Isolated test server exited');
    try { if ((await fetch(url, { signal: AbortSignal.timeout(500) })).ok) return; } catch {}
    await delay(200);
  }
  throw new Error('Isolated server readiness timed out');
}

let created = false;
try {
  sql(`CREATE DATABASE "${name}"`); created = true;
  const apiPort = await port(); const frontendPort = await port();
  const origin = `http://127.0.0.1:${frontendPort}`;
  const env = { ...process.env, APP_ENV: 'test', POSTGRES_DB: name, POSTGRES_HOST: '127.0.0.1', API_HOST: '127.0.0.1', API_PORT: String(apiPort), PUBLIC_ORIGINS: origin, CSRF_SECRET: randomBytes(32).toString('hex'), COOKIE_SECURE: 'false', TRUSTED_PROXIES: '', MEDIA_STORAGE_ROOT: mediaRoot ?? '' };
  const binaries = { api: `${binDir}/api`, migrate: `${binDir}/migrate`, admin: `${binDir}/admin` };
  for (const [name, path] of Object.entries(binaries)) execFileSync('go', ['build', '-o', path, `./cmd/${name}`], { cwd: `${root}backend`, env, stdio: ['ignore', 'pipe', 'pipe'] });
  execFileSync(binaries.migrate, ['up'], { cwd: root, env, stdio: ['ignore', 'pipe', 'pipe'] });
  if (freshInit) {
    const before = sql(`SELECT schema_version,updated_at FROM app_metadata WHERE component='core'`, name).toString();
    execFileSync(binaries.migrate, ['up'], { cwd: root, env, stdio: ['ignore', 'pipe', 'pipe'] });
    const after = sql(`SELECT schema_version,updated_at FROM app_metadata WHERE component='core'`, name).toString();
    if (before !== after || !before.startsWith('13|')) throw new Error('Fresh migration repeat changed the core schema');
  }
  const credentials = auth || profile || articles || starter ? { username: `phase22_${randomBytes(5).toString('hex')}`, email: `phase22_${randomBytes(5).toString('hex')}@example.test`, password: randomBytes(28).toString('base64url') } : null;
  if (credentials) execFileSync(binaries.admin, ['create'], { cwd: root, env: { ...env, ADMIN_INIT_DATABASE: name, ADMIN_INIT_USERNAME: credentials.username, ADMIN_INIT_EMAIL: credentials.email, ADMIN_INIT_PASSWORD: credentials.password }, stdio: ['ignore', 'pipe', 'pipe'] });
  if (freshInit && credentials) {
    const adminCount = sql(`SELECT count(*) FROM admin_users`, name).toString().trim();
    const before = sql(`SELECT count(*),max(updated_at) FROM menus`, name).toString();
    let refused = false;
    try {
      execFileSync(binaries.admin, ['create'], { cwd: root, env: { ...env, ADMIN_INIT_DATABASE: name, ADMIN_INIT_USERNAME: `another_${randomBytes(5).toString('hex')}`, ADMIN_INIT_EMAIL: `another_${randomBytes(5).toString('hex')}@example.test`, ADMIN_INIT_PASSWORD: randomBytes(28).toString('base64url') }, stdio: ['ignore', 'pipe', 'pipe'] });
    } catch { refused = true; }
    const afterCount = sql(`SELECT count(*) FROM admin_users`, name).toString().trim();
    if (!refused || adminCount !== '1' || afterCount !== '1') throw new Error(`Second initial administrator guard failed (refused=${refused}, before=${adminCount}, after=${afterCount})`);
    execFileSync(binaries.admin, ['menus-init'], { cwd: root, env: { ...env, ADMIN_INIT_DATABASE: name }, stdio: ['ignore', 'pipe', 'pipe'] });
    if (before !== sql(`SELECT count(*),max(updated_at) FROM menus`, name).toString()) throw new Error('Menu initialization was not idempotent');
    console.log('Fresh migration repeat, initial administrator guard and menu initialization verified.');
  }
  if (media) execFileSync(binaries.admin, ['media-bind'], { cwd: root, env: { ...env, ADMIN_INIT_DATABASE: name, MEDIA_BIND_CONFIRM_DATABASE: name }, stdio: ['ignore', 'pipe', 'pipe'] });
  const api = await start(binaries.api, [], env, root);
  await ready(`http://127.0.0.1:${apiPort}/api/v1/health`, api);
  let upstreamURL = `http://127.0.0.1:${apiPort}`;
  if (mediaHTTP) {
    const targetAPI = upstreamURL;
    delayedProxy = createHttpServer((incoming, outgoing) => {
      const proxy = httpRequest(targetAPI + incoming.url, { method: incoming.method, headers: incoming.headers }, (upstream) => {
        const mediaContent = /\/api\/v1\/admin\/media\/[0-9a-f-]{36}\/content/.test(incoming.url ?? '');
        const headDelay = mediaContent && incoming.headers.accept === 'application/x-media-test-head';
        const headTimeout = mediaContent && incoming.headers.accept === 'application/x-media-test-head-timeout';
        const idleDelay = mediaContent && incoming.headers.accept === 'application/x-media-test-idle';
        const totalDelay = mediaContent && incoming.headers.accept === 'application/x-media-test-total';
        if (headDelay || headTimeout) {
          upstream.pause();
          setTimeout(() => { if (!outgoing.destroyed) { outgoing.writeHead(upstream.statusCode, upstream.headers); upstream.pipe(outgoing); upstream.resume(); } }, headTimeout ? 47000 : 17000);
        } else if (idleDelay) {
          upstream.once('data', (first) => {
            upstream.pause(); outgoing.writeHead(upstream.statusCode, upstream.headers); outgoing.write(first.subarray(0, 1));
            setTimeout(() => { if (!outgoing.destroyed) { outgoing.write(first.subarray(1)); upstream.pipe(outgoing); upstream.resume(); } }, 17000);
          });
        } else if (totalDelay) {
          upstream.once('data', (first) => {
            upstream.pause(); outgoing.writeHead(upstream.statusCode, upstream.headers); outgoing.write(first.subarray(0, 1));
            let sent = 1;
            const ticker = setInterval(() => {
              if (outgoing.destroyed) { clearInterval(ticker); return; }
              if (sent < first.length) { outgoing.write(first.subarray(sent, sent + 1)); sent++; }
              else { clearInterval(ticker); upstream.pipe(outgoing); upstream.resume(); }
            }, 10000);
          });
        } else { outgoing.writeHead(upstream.statusCode, upstream.headers); upstream.pipe(outgoing); }
      });
      proxy.on('error', () => { if (!outgoing.headersSent) outgoing.writeHead(502); outgoing.end(); });
      outgoing.on('close', () => proxy.destroy());
      incoming.pipe(proxy);
    });
    delayedProxy.listen(0, '127.0.0.1');
    await once(delayedProxy, 'listening');
    upstreamURL = `http://127.0.0.1:${delayedProxy.address().port}`;
  }
  const frontend = await start(process.execPath, ['node_modules/next/dist/bin/next', 'start', '--hostname', '127.0.0.1', '--port', String(frontendPort)], { ...env, API_INTERNAL_URL: upstreamURL }, `${root}frontend`);
  await ready(`${origin}/health`, frontend);
  const tests = spawn(process.execPath, ['node_modules/@playwright/test/cli.js', 'test', ...(mediaHTTP ? ['tests/e2e/media-http.spec.ts'] : []), ...(project ? ['--project', project] : []), '--workers', '1', '--output', `${root}frontend/test-results/${name}`], { cwd: `${root}frontend`, env: { ...env, E2E_BASE_URL: origin, E2E_SECURITY: '1', E2E_AUTH: auth ? '1' : '', E2E_RBAC: process.env.E2E_RBAC === '1' ? '1' : '', E2E_MENUS: process.env.E2E_MENUS === '1' ? '1' : '', E2E_DASHBOARD: dashboard ? '1' : '', E2E_SETTINGS: settings ? '1' : '', E2E_MEDIA: media ? '1' : '', E2E_MEDIA_HTTP: mediaHTTP ? '1' : '', E2E_LOGS: logs ? '1' : '', E2E_PROFILE: profile ? '1' : '', E2E_ARTICLES: articles ? '1' : '', E2E_STARTER: starter ? '1' : '', E2E_ADMIN_USER: credentials?.username ?? '', E2E_ADMIN_PASSWORD: credentials?.password ?? '' }, stdio: 'inherit' });
  const [status] = await once(tests, 'exit');
  if (status !== 0) throw new Error(`Phase ${phase} browser tests failed`);
  if (process.env.E2E_CHECK === '1') {
    execFileSync('make', ['check'], {
      cwd: root,
      env: { ...env, FRONTEND_PORT: String(frontendPort), API_INTERNAL_URL: upstreamURL },
      stdio: 'inherit',
    });
  }
  console.log(`Phase ${phase} isolated database/API/Next browser verification passed.`);
} finally {
  if (delayedProxy) { delayedProxy.closeAllConnections(); await new Promise((resolve) => delayedProxy.close(resolve)); }
  for (const child of children.reverse()) {
    if (child.exitCode === null && child.signalCode === null) {
      const exited = once(child, 'exit'); child.kill('SIGTERM');
      await Promise.race([exited, delay(5000)]);
      if (child.exitCode === null && child.signalCode === null) { child.kill('SIGKILL'); await exited; }
    }
  }
  // Only this invocation's cryptographically random test database can be removed.
  if (created && /^phase(?:2[12345]|3[123456]|40)_e2e_[0-9a-f]{16}$/.test(name)) { sql(`DROP DATABASE "${name}"`); console.log('Removed isolated test database.'); }
  rmSync(binDir, { recursive: true, force: true });
  if (mediaRoot) rmSync(mediaRoot, { recursive: true, force: true });
}
