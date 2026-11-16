import { afterAll, beforeAll, expect, it, vi } from 'vitest';
import { createServer, type Server } from 'node:http';
import type { AddressInfo } from 'node:net';
vi.mock('server-only', () => ({}));
import { backendResponse } from '@/lib/backend';

let server: Server;
let base: string;
beforeAll(async () => {
  server = createServer(async (request, response) => {
    if (request.url?.startsWith('/api/v1/admin/media/') && request.method === 'GET') {
      response.writeHead(200, { 'Content-Type': 'text/plain', 'Content-Disposition': 'attachment; filename="sample.txt"', 'Content-Security-Policy': "default-src 'none'; sandbox", 'X-Content-Type-Options': 'nosniff' });
      response.write(Buffer.from('first ')); response.end(Buffer.from('second')); return;
    }
    if (request.url === '/api/v1/auth/me') {
      response.writeHead(200, { 'Content-Type': 'application/octet-stream', 'X-Request-ID': 'upstream-id' });
      response.end(Buffer.from([0, 255, 128, 10])); return;
    }
    if (request.method === 'DELETE') { response.writeHead(204); response.end(); return; }
    const chunks: Buffer[] = [];
    for await (const chunk of request) chunks.push(Buffer.from(chunk));
    response.writeHead(422, {
      'Content-Type': 'application/json', 'X-Request-ID': 'upstream-id',
      'Set-Cookie': ['goadmin_session=fixture; Path=/; HttpOnly; SameSite=Lax; Expires=Thu, 12 Nov 2026 12:00:00 GMT', 'goadmin_csrf=fixture; Path=/; HttpOnly; SameSite=Lax'],
    });
    response.end(JSON.stringify({ method: request.method, body: Buffer.concat(chunks).toString(), headers: request.headers }));
  });
  await new Promise<void>((resolve) => server.listen(0, '127.0.0.1', resolve));
  base = `http://127.0.0.1:${(server.address() as AddressInfo).port}`;
});
afterAll(async () => { server.closeAllConnections(); await new Promise<void>((resolve, reject) => server.close((err) => err ? reject(err) : resolve())); });

it.each(['POST', 'PUT', 'PATCH'])('forwards %s body and only necessary headers over real HTTP', async (method) => {
  vi.stubEnv('API_INTERNAL_URL', base);
  const response = await backendResponse('/api/v1/users', new Request('http://frontend/api/v1/users', {
    method, body: '{"display_name":"Example"}', headers: {
      Cookie: 'unrelated=private; goadmin_session=session-fixture; goadmin_csrf=csrf-fixture',
      'Content-Type': 'application/json', Origin: 'http://frontend', 'X-CSRF-Token': 'csrf-fixture',
      Forwarded: 'for=attacker', 'X-Forwarded-For': '1.2.3.4', 'X-Forwarded-Host': 'attacker',
      'X-Real-IP': '1.2.3.4', Authorization: 'private', 'X-Request-ID': 'attacker-id',
    },
  }));
  expect(response.status).toBe(422);
  expect(response.headers.get('x-request-id')).toBe('upstream-id');
  expect(response.headers.getSetCookie()).toHaveLength(2);
  expect(response.headers.getSetCookie()[0]).toContain('Expires=Thu, 12 Nov');
  const result = await response.json();
  expect(result.method).toBe(method);
  expect(result.body).toBe('{"display_name":"Example"}');
  expect(result.headers.cookie).toBe('goadmin_session=session-fixture; goadmin_csrf=csrf-fixture');
  expect(result.headers.origin).toBe('http://frontend');
  expect(result.headers['x-csrf-token']).toBe('csrf-fixture');
  for (const header of ['forwarded', 'x-forwarded-for', 'x-forwarded-host', 'x-real-ip', 'authorization', 'x-request-id']) expect(result.headers[header]).toBeUndefined();
  expect(result.headers.host).toBe(new URL(base).host);
});

it('preserves binary and empty responses', async () => {
  vi.stubEnv('API_INTERNAL_URL', base);
  const binary = await backendResponse('/api/v1/auth/me');
  expect(binary.headers.get('content-type')).toBe('application/octet-stream');
  expect([...new Uint8Array(await binary.arrayBuffer())]).toEqual([0, 255, 128, 10]);
  const empty = await backendResponse('/api/v1/users', new Request('http://frontend/api/v1/users', { method: 'DELETE' }));
  expect(empty.status).toBe(204); expect(await empty.text()).toBe('');
});

it('allows only POST for logout-all and forwards its CSRF cookie', async () => {
  vi.stubEnv('API_INTERNAL_URL', base);
  const response = await backendResponse('/api/v1/auth/logout-all', new Request('http://frontend/api/v1/auth/logout-all', {
    method: 'POST', headers: { Cookie: 'goadmin_session=secret; goadmin_csrf=csrf', Origin: 'http://frontend', 'X-CSRF-Token': 'csrf' },
  }));
  expect(response.status).toBe(422);
  const result = await response.json();
  expect(result.headers.cookie).toBe('goadmin_session=secret; goadmin_csrf=csrf');
  expect((await backendResponse('/api/v1/auth/logout-all')).status).toBe(404);
});

it.each(['/api/v1/private', '//attacker.invalid', 'http://attacker.invalid', '/api/v1/users/../private', '/api/v1/users/%2e%2e', '/api/v1/users/not-a-uuid', '/api/v1/users?url=http://attacker'])('rejects illegal path %s without fetching', async (path) => {
  const fetch = vi.fn(); vi.stubGlobal('fetch', fetch);
  expect((await backendResponse(path)).status).toBe(404); expect(fetch).not.toHaveBeenCalled();
});

it('rejects duplicate auth cookies, oversized bodies, and unsafe methods', async () => {
  vi.stubEnv('API_INTERNAL_URL', base);
  const duplicate = new Request('http://frontend', { headers: { Cookie: 'goadmin_session=one; goadmin_session=two' } });
  expect((await backendResponse('/api/v1/auth/me', duplicate)).status).toBe(400);
  expect((await backendResponse('/api/v1/users', new Request('http://frontend', { method: 'POST', body: 'a'.repeat(1024 * 1024 + 1) }))).status).toBe(413);
  expect((await backendResponse('/api/v1/users', new Request('http://frontend', { method: 'OPTIONS' }))).status).toBe(404);
});

it('handles timeout without exposing upstream details', async () => {
  vi.stubGlobal('fetch', vi.fn().mockImplementation(async (_url, options) => {
    expect(options.signal).toBeInstanceOf(AbortSignal);
    throw new DOMException('private network details', 'TimeoutError');
  }));
  const response = await backendResponse('/api/v1/health');
  expect(response.status).toBe(503); expect(await response.text()).not.toContain('private');
});

it('only proxies declared RBAC paths and methods with CSRF and cookies', async () => {
  vi.stubEnv('API_INTERNAL_URL', base);
  const id = '11111111-1111-4111-8111-111111111111';
  const forwarded = await backendResponse(`/api/v1/admin/users/${id}/roles`, new Request(`http://frontend/api/v1/admin/users/${id}/roles`, { method: 'PUT', headers: { Cookie: 'goadmin_session=test; goadmin_csrf=csrf', Origin: 'http://frontend', 'X-CSRF-Token': 'csrf', 'Content-Type': 'application/json', 'X-Forwarded-For': '198.51.100.1' }, body: '{"role_ids":[]}' }));
  expect(forwarded.status).toBe(422);
  const result = await forwarded.json();
  expect(result.headers.cookie).toBe('goadmin_session=test; goadmin_csrf=csrf');
  expect(result.headers['x-forwarded-for']).toBeUndefined();
  expect(result.body).toBe('{"role_ids":[]}');
  for (const path of [`/api/v1/admin/users/${id}/unknown`, '/api/v1/admin/internal', '/api/v1/admin/permissions/create']) {
    expect((await backendResponse(path)).status).toBe(404);
  }
  expect((await backendResponse('/api/v1/admin/permissions', new Request('http://frontend', { method: 'POST' }))).status).toBe(404);
  for (const [path, method] of [['/api/v1/admin/menus', 'GET'], ['/api/v1/admin/menus/tree', 'GET'], ['/api/v1/admin/menus/my', 'GET'], ['/api/v1/admin/menus/roles', 'GET'], ['/api/v1/admin/menus/permissions', 'GET'], ['/api/v1/admin/menus/reorder', 'PUT'], [`/api/v1/admin/menus/${id}/roles`, 'PUT']] as const) {
    expect((await backendResponse(path, new Request(`http://frontend${path}`, { method }))).status).toBe(422);
  }
  for (const path of ['/api/v1/admin/dashboard/summary', '/api/v1/admin/dashboard/activity', '/api/v1/admin/dashboard/trends']) {
    expect((await backendResponse(path, new Request(`http://frontend${path}`))).status).toBe(422);
    expect((await backendResponse(path, new Request(`http://frontend${path}`, { method:'POST' }))).status).toBe(404);
  }
  expect((await backendResponse('/api/v1/admin/dashboard/private')).status).toBe(404);
  for (const method of ['GET', 'PUT']) {
    expect((await backendResponse('/api/v1/admin/settings', new Request('http://frontend/api/v1/admin/settings', { method }))).status).toBe(422);
  }
  for (const method of ['POST', 'PATCH', 'DELETE']) {
    expect((await backendResponse('/api/v1/admin/settings', new Request('http://frontend/api/v1/admin/settings', { method }))).status).toBe(404);
  }
  expect((await backendResponse('/api/v1/admin/settings/private')).status).toBe(404);
  expect((await backendResponse('/api/v1/admin/menus/unlisted/action')).status).toBe(404);
});

it('allowlists only Articles collection, UUID detail and explicit actions', async () => {
  vi.stubEnv('API_INTERNAL_URL', base);
  const id='11111111-1111-4111-8111-111111111111';
  for (const [path,methods] of [
    ['/api/v1/admin/articles',['GET','POST']],
    [`/api/v1/admin/articles/${id}`,['GET','PUT','DELETE']],
    [`/api/v1/admin/articles/${id}/publish`,['POST']],
    [`/api/v1/admin/articles/${id}/unpublish`,['POST']],
    ['/api/v1/admin/article-categories',['GET','POST']],
    [`/api/v1/admin/article-categories/${id}`,['PUT','DELETE']],
  ] as const) {
    for (const method of methods) expect((await backendResponse(path,new Request(`http://frontend${path}`,{method}))).status).toBe(method==='DELETE'?204:422);
  }
  for (const path of ['/api/v1/admin/articles/other',`/api/v1/admin/articles/${id}/preview`, '/api/v1/admin/article-categories/other']) expect((await backendResponse(path)).status).toBe(404);
  expect((await backendResponse(`/api/v1/admin/articles/${id}/publish`,new Request('http://frontend',{method:'DELETE'}))).status).toBe(404);
});

it('allowlists only explicit starter management routes and never forwards API key credentials', async () => {
  vi.stubEnv('API_INTERNAL_URL', base);
  const id='11111111-1111-4111-8111-111111111111';
  for (const [path, methods] of [
    ['/api/v1/admin/notifications',['GET','POST']],
    [`/api/v1/admin/notifications/${id}/read`,['POST']],
    ['/api/v1/admin/api-keys',['GET','POST']],
    [`/api/v1/admin/api-keys/${id}/revoke`,['POST']],
    ['/api/v1/admin/jobs',['GET','POST']],
    [`/api/v1/admin/jobs/${id}`,['GET']],
    [`/api/v1/admin/jobs/${id}/retry`,['POST']],
  ] as const) for (const method of methods) expect((await backendResponse(path,new Request(`http://frontend${path}`,{method}))).status).toBe(422);
  for (const path of ['/api/v1/key/dashboard/summary', '/api/v1/admin/jobs/run-arbitrary', '/api/v1/admin/notifications/not-a-uuid/read']) {
    expect((await backendResponse(path)).status).toBe(404);
  }
  expect((await backendResponse('/api/v1/admin/jobs',new Request('http://frontend/api/v1/admin/jobs',{method:'DELETE'}))).status).toBe(404);
});

it('streams allowlisted media binary responses and rejects unknown paths', async () => {
  vi.stubEnv('API_INTERNAL_URL', base);
  const id = '11111111-1111-4111-8111-111111111111';
  const response = await backendResponse(`/api/v1/admin/media/${id}/content`, new Request(`http://frontend/api/v1/admin/media/${id}/content`, { headers: { Cookie: 'goadmin_session=private' } }));
  expect(response.status).toBe(200);
  expect(response.headers.get('content-disposition')).toContain('attachment');
  expect(response.headers.get('content-security-policy')).toContain('sandbox');
  expect(await response.text()).toBe('first second');
  for (const path of ['/api/v1/admin/media/arbitrary/content', `/api/v1/admin/media/${id}/storage`, '/api/v1/admin/media/../settings']) {
    expect((await backendResponse(path)).status).toBe(404);
  }
});

it('allows only bounded log GET routes with session cookie', async () => {
  vi.stubEnv('API_INTERNAL_URL', base);
  const id = '11111111-1111-4111-8111-111111111111';
  for (const resource of ['audit-logs', 'login-logs']) {
    for (const suffix of ['', `/${id}`]) {
      const path = `/api/v1/admin/${resource}${suffix}`;
      const response = await backendResponse(path, new Request(`http://frontend${path}`, { headers: { Cookie: 'goadmin_session=test; unrelated=discard', 'X-Forwarded-For': '198.51.100.1' } }));
      expect(response.status).toBe(422);
      const body = await response.json();
      expect(body.headers.cookie).toBe('goadmin_session=test');
      expect(body.headers['x-forwarded-for']).toBeUndefined();
      expect((await backendResponse(path, new Request(`http://frontend${path}`, { method: 'POST' }))).status).toBe(404);
    }
    expect((await backendResponse(`/api/v1/admin/${resource}/not-a-uuid`)).status).toBe(404);
    expect((await backendResponse(`/api/v1/admin/${resource}/${id}/raw`)).status).toBe(404);
  }
});

it('allows only the declared self-service profile routes and methods', async () => {
  vi.stubEnv('API_INTERNAL_URL', base);
  const id = '11111111-1111-4111-8111-111111111111';
  const allowed = [
    ['/api/v1/admin/profile', 'GET'], ['/api/v1/admin/profile', 'PUT'],
    ['/api/v1/admin/profile/password', 'PUT'], ['/api/v1/admin/profile/sessions', 'GET'],
    ['/api/v1/admin/profile/sessions/revoke-others', 'POST'], [`/api/v1/admin/profile/sessions/${id}`, 'DELETE'],
  ] as const;
  for (const [path, method] of allowed) {
    const response = await backendResponse(path, new Request(`http://frontend${path}`, { method, headers: { Cookie: 'goadmin_session=private; unrelated=discard', 'Content-Type': 'application/json', Origin: 'http://frontend', 'X-CSRF-Token': 'secret', 'X-Forwarded-For': '198.51.100.1' }, body: method === 'PUT' ? '{}' : undefined }));
    if (method === 'DELETE') { expect(response.status).toBe(204); continue; }
    expect(response.status).toBe(422);
    const body = await response.json();
    expect(body.headers.cookie).toBe('goadmin_session=private');
    expect(body.headers['x-forwarded-for']).toBeUndefined();
    expect(body.headers['x-csrf-token']).toBe('secret');
  }
  for (const [path, method] of [
    ['/api/v1/admin/profile/password', 'GET'], ['/api/v1/admin/profile/sessions', 'DELETE'],
    ['/api/v1/admin/profile/sessions/not-a-uuid', 'DELETE'], ['/api/v1/admin/profile/sessions/revoke-others', 'DELETE'],
    ['/api/v1/admin/profile/other', 'GET'], ['/api/v1/admin/profile/sessions/arbitrary/extra', 'DELETE'],
  ] as const) {
    expect((await backendResponse(path, new Request(`http://frontend${path}`, { method }))).status).toBe(404);
  }
});

it('forwards streamed multipart without forwarded identity headers', async () => {
  vi.stubEnv('API_INTERNAL_URL', base);
  const body = '--boundary\r\nContent-Disposition: form-data; name="file"; filename="a.txt"\r\nContent-Type: text/plain\r\n\r\nhello\r\n--boundary--\r\n';
  const request = new Request('http://frontend/api/v1/admin/media', { method: 'POST', body, headers: { 'Content-Type': 'multipart/form-data; boundary=boundary', Cookie: 'goadmin_session=private; goadmin_csrf=csrf', Origin: 'http://frontend', 'X-CSRF-Token': 'csrf', 'X-Forwarded-For': '198.51.100.1' } });
  const response = await backendResponse('/api/v1/admin/media', request);
  expect(response.status).toBe(422);
  const data = await response.json();
  expect(data.body).toBe(body);
  expect(data.headers.cookie).toBe('goadmin_session=private; goadmin_csrf=csrf');
  expect(data.headers['x-forwarded-for']).toBeUndefined();
  expect((await backendResponse('/api/v1/admin/media', new Request('http://frontend/api/v1/admin/media', { method: 'PUT' }))).status).toBe(404);
});
