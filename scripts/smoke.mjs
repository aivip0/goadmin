import assert from 'node:assert/strict';
import { loadEnvFile } from 'node:process';
try { loadEnvFile(); } catch (error) { if (error.code !== 'ENOENT') throw error; }
const api = `http://127.0.0.1:${process.env.API_PORT ?? 8080}`;
const web = `http://127.0.0.1:${process.env.FRONTEND_PORT ?? 3000}`;
async function request(url) { return fetch(url, { signal: AbortSignal.timeout(10000) }); }
for (const path of ['/api/v1/health', '/api/v1/health/ready', '/api/v1/health/live']) {
  const response = await request(api + path);
  assert.equal(response.status, 200, path);
  assert.equal((await response.json()).code, 0);
  assert.match(response.headers.get('x-request-id'), /^[a-f\d]{32}$/);
}
for (const base of [api, web]) {
  const response = await request(base + '/api/v1/openapi.json');
  assert.equal(response.status, 200);
  const document = await response.json();
  assert.equal(document.openapi, '3.0.3');
  assert.ok(document.paths['/api/v1/health']);
}
assert.equal((await request(api + '/api/v1/users')).status, 404);
assert.equal((await request(web + '/')).status, 200);
const proxied = await request(web + '/api/v1/health');
assert.equal(proxied.status, 200);
assert.equal((await proxied.json()).data.database, 'connected');
console.log('Smoke passed: Go API, Next.js, proxy, PostgreSQL readiness, migration, OpenAPI, deferred routes.');
