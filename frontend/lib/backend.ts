import 'server-only';

const uuid = '[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}';
const resource = new RegExp(`^/api/v1/(users|roles|permissions|menus)(/${uuid})?$`);
const adminResource = new RegExp(`^/api/v1/admin/(users|roles)(/${uuid})?$`);
const userAction = new RegExp(`^/api/v1/admin/users/${uuid}/(enable|disable|roles)$`);
const userPassword = new RegExp(`^/api/v1/admin/users/${uuid}/password$`);
const roleAction = new RegExp(`^/api/v1/admin/roles/${uuid}/permissions$`);
const menuDetail = new RegExp(`^/api/v1/admin/menus/${uuid}$`);
const menuRoles = new RegExp(`^/api/v1/admin/menus/${uuid}/roles$`);
const mediaDetail = new RegExp(`^/api/v1/admin/media/${uuid}(?:/content)?$`);
const logsDetail = new RegExp(`^/api/v1/admin/(audit-logs|login-logs)/${uuid}$`);
const profileSession = new RegExp(`^/api/v1/admin/profile/sessions/${uuid}$`);
const articleDetail = new RegExp(`^/api/v1/admin/articles/${uuid}$`);
const categoryDetail = new RegExp(`^/api/v1/admin/article-categories/${uuid}$`);
const articleAction = new RegExp(`^/api/v1/admin/articles/${uuid}/(publish|unpublish)$`);
const notificationRead = new RegExp(`^/api/v1/admin/notifications/${uuid}/read$`);
const apiKeyRevoke = new RegExp(`^/api/v1/admin/api-keys/${uuid}/revoke$`);
const jobDetail = new RegExp(`^/api/v1/admin/jobs/${uuid}$`);
const jobRetry = new RegExp(`^/api/v1/admin/jobs/${uuid}/retry$`);
const pluginID = '[a-z][a-z0-9-]{2,31}';
const pluginKey = '[a-z][a-z0-9_-]{1,47}';
const pluginConfig = new RegExp(`^/api/v1/admin/plugins/${pluginID}/config$`);
const pluginLifecycle = new RegExp(`^/api/v1/admin/plugins/${pluginID}/(enable|disable|uninstall|upgrade)$`);
const pluginRoute = new RegExp(`^/api/v1/admin/plugins/${pluginID}/routes/${pluginKey}$`);
const methods = new Set(['GET', 'POST', 'PUT', 'PATCH', 'DELETE']);
const maxBody = 1024 * 1024;

function allowed(path: string, method: string): boolean {
  if (!methods.has(method) || path.includes('%') || path.includes('\\') || path.includes('?') || path.includes('#')) return false;
  if (['/api/v1/health', '/api/v1/health/live', '/api/v1/health/ready', '/api/v1/openapi.json', '/api/v1/auth/csrf', '/api/v1/auth/me'].includes(path)) return method === 'GET';
  if (['/api/v1/auth/login', '/api/v1/auth/logout', '/api/v1/auth/logout-all'].includes(path)) return method === 'POST';
  if (path === '/api/v1/admin/permissions/effective' || path === '/api/v1/admin/permissions/grouped' || path === '/api/v1/admin/permissions') return method === 'GET';
  if (['/api/v1/admin/dashboard/summary', '/api/v1/admin/dashboard/activity', '/api/v1/admin/dashboard/trends'].includes(path)) return method === 'GET';
  if (path === '/api/v1/admin/settings') return method === 'GET' || method === 'PUT';
  if (path === '/api/v1/admin/profile') return method === 'GET' || method === 'PUT';
  if (path === '/api/v1/admin/profile/password') return method === 'PUT';
  if (path === '/api/v1/admin/profile/sessions') return method === 'GET';
  if (path === '/api/v1/admin/profile/sessions/revoke-others') return method === 'POST';
  if (profileSession.test(path)) return method === 'DELETE';
  if (path === '/api/v1/admin/articles' || path === '/api/v1/admin/article-categories') return method === 'GET' || method === 'POST';
  if (articleDetail.test(path)) return ['GET','PUT','DELETE'].includes(method);
  if (categoryDetail.test(path)) return method === 'PUT' || method === 'DELETE';
  if (articleAction.test(path)) return method === 'POST';
  if (path === '/api/v1/admin/notifications') return method === 'GET' || method === 'POST';
  if (notificationRead.test(path)) return method === 'POST';
  if (path === '/api/v1/admin/api-keys') return method === 'GET' || method === 'POST';
  if (apiKeyRevoke.test(path)) return method === 'POST';
  if (path === '/api/v1/admin/jobs') return method === 'GET' || method === 'POST';
  if (jobDetail.test(path)) return method === 'GET';
  if (jobRetry.test(path)) return method === 'POST';
  if (path === '/api/v1/admin/audit-logs' || path === '/api/v1/admin/login-logs' || logsDetail.test(path)) return method === 'GET';
  if (path === '/api/v1/admin/media') return method === 'GET' || method === 'POST';
  if (path === '/api/v1/admin/plugins' || path === '/api/v1/admin/plugins/packages' || path === '/api/v1/admin/plugins/events') return method === 'GET';
  if (path === '/api/v1/admin/plugins/install') return method === 'POST';
  if (pluginConfig.test(path)) return method === 'GET' || method === 'PUT';
  if (pluginLifecycle.test(path)) return method === 'POST';
  if (pluginRoute.test(path)) return ['GET', 'POST', 'PUT', 'DELETE'].includes(method);
  if (mediaDetail.test(path)) return path.endsWith('/content') ? method === 'GET' : method === 'GET' || method === 'DELETE';
  if (path === '/api/v1/admin/menus/my' || path === '/api/v1/admin/menus/tree' || path === '/api/v1/admin/menus/roles' || path === '/api/v1/admin/menus/permissions' || path === '/api/v1/admin/menus') {
    if (path.endsWith('/my') || path.endsWith('/tree') || path.endsWith('/roles') || method === 'GET') return method === 'GET';
    return ['GET', 'POST'].includes(method);
  }
  if (path === '/api/v1/admin/menus/reorder') return method === 'PUT';
  if (menuRoles.test(path)) return method === 'PUT';
  if (menuDetail.test(path)) return ['GET', 'PUT', 'DELETE'].includes(method);
  if (adminResource.test(path)) {
    const isDetail = new RegExp(`^/api/v1/admin/(users|roles)/${uuid}$`).test(path);
    return isDetail ? ['GET', 'PUT', 'DELETE'].includes(method) : ['GET', 'POST'].includes(method);
  }
  if (userAction.test(path)) return path.endsWith('/roles') ? method === 'PUT' : method === 'POST';
  if (userPassword.test(path)) return method === 'PUT';
  if (roleAction.test(path)) return method === 'PUT';
  return resource.test(path);
}

function failure(status: number, code: number, message: string): Response {
  return Response.json({ code, message, data: null }, { status, headers: { 'Cache-Control': 'no-store', 'X-Content-Type-Options': 'nosniff' } });
}

async function readBody(request: Request): Promise<Uint8Array | undefined> {
  if (!request.body) return undefined;
  if (Number(request.headers.get('content-length')) > maxBody) throw new RangeError('Body too large');
  const reader = request.body.getReader();
  const chunks: Uint8Array[] = [];
  let size = 0;
  try {
    for (;;) {
      const { done, value } = await reader.read();
      if (done) break;
      size += value.byteLength;
      if (size > maxBody) throw new RangeError('Body too large');
      chunks.push(value);
    }
  } finally { await reader.cancel(); reader.releaseLock(); }
  const body = new Uint8Array(size);
  let offset = 0;
  for (const chunk of chunks) { body.set(chunk, offset); offset += chunk.byteLength; }
  return body;
}

function mediaQuery(path: string, request: Request): boolean {
  const query = new URL(request.url).searchParams;
  const keys = path === '/api/v1/admin/media' ? ['page', 'page_size', 'type', 'q'] : path.endsWith('/content') ? ['download'] : [];
  return [...query.keys()].every((key) => keys.includes(key) && query.getAll(key).length === 1);
}

function streamLimit(source: ReadableStream<Uint8Array>, abort: AbortController, max: number, exceeded: () => void): ReadableStream<Uint8Array> {
  let seen = 0;
  return source.pipeThrough(new TransformStream<Uint8Array, Uint8Array>({
    transform(chunk, controller) {
      seen += chunk.byteLength;
      if (seen > max) {
        exceeded();
        abort.abort();
        controller.error(new RangeError('Media body too large'));
      } else controller.enqueue(chunk);
    },
  }));
}

async function mediaResponse(path: string, request: Request, url: URL, headers: Headers): Promise<Response> {
  if (!mediaQuery(path, request)) return failure(400, 40000, 'Invalid request');
  const isUpload = request.method === 'POST';
  const isContent = path.endsWith('/content');
  if (isUpload && (!/^multipart\/form-data;\s*boundary=[A-Za-z0-9'()+_,.\/:=?-]{1,70}$/i.test(headers.get('content-type') ?? '') || Number(request.headers.get('content-length')) > 26 * 1024 * 1024)) {
    return failure(Number(request.headers.get('content-length')) > 26 * 1024 * 1024 ? 413 : 400, Number(request.headers.get('content-length')) > 26 * 1024 * 1024 ? 41300 : 40000, 'Invalid media request');
  }
  const range = request.headers.get('range');
  if (range) headers.set('range', range);
  const abort = new AbortController();
  let tooLarge = false;
  const onCancel = () => abort.abort();
  request.signal.addEventListener('abort', onCancel, { once: true });
  const deadline = setTimeout(onCancel, isContent ? 90000 : isUpload ? 60000 : 5000);
  let firstByte: ReturnType<typeof setTimeout> | undefined;
  try {
    const body = isUpload && request.body ? streamLimit(request.body, abort, 26 * 1024 * 1024, () => { tooLarge = true; }) : undefined;
    if (isUpload && !body) return failure(400, 40000, 'Invalid request');
    const pending = fetch(url, { method: request.method, headers, body, signal: abort.signal, redirect: 'error', cache: 'no-store', duplex: 'half' } as RequestInit & { duplex: 'half' });
    if (isContent) firstByte = setTimeout(onCancel, 45000);
    const upstream = await pending;
    const output = new Headers({ 'Cache-Control': 'no-store', 'X-Content-Type-Options': 'nosniff' });
    for (const name of ['content-type', 'content-length', 'content-disposition', 'content-security-policy', 'x-request-id']) {
      const value = upstream.headers.get(name);
      if (value) output.set(name, value);
    }
    if (!isContent || !upstream.ok || !upstream.body) {
      if (firstByte) clearTimeout(firstByte);
      const data = await upstream.arrayBuffer();
      clearTimeout(deadline);
      return new Response(data, { status: upstream.status, headers: output });
    }
    const reader = upstream.body.getReader();
    let idle: ReturnType<typeof setTimeout> | undefined;
    const resetIdle = () => { if (idle) clearTimeout(idle); idle = setTimeout(onCancel, 15000); };
    let first = true;
    const stream = new ReadableStream<Uint8Array>({
      async pull(controller) {
        try {
          const { done, value } = await reader.read();
          if (done) { if (idle) clearTimeout(idle); clearTimeout(deadline); controller.close(); request.signal.removeEventListener('abort', onCancel); return; }
          if (first) { first = false; if (firstByte) clearTimeout(firstByte); }
          resetIdle(); controller.enqueue(value);
        } catch (error) { if (idle) clearTimeout(idle); clearTimeout(deadline); controller.error(error); }
      },
      async cancel() { if (idle) clearTimeout(idle); clearTimeout(deadline); onCancel(); await reader.cancel(); },
    });
    return new Response(stream, { status: upstream.status, headers: output });
  } catch (error) {
    clearTimeout(deadline);
    if (tooLarge || error instanceof RangeError) return failure(413, 41300, 'File too large');
    return failure(503, 50302, 'API unavailable');
  } finally { if (!isContent && firstByte) clearTimeout(firstByte); }
}

export async function backendResponse(path: string, request?: Request): Promise<Response> {
  const method = request?.method ?? 'GET';
  if (!allowed(path, method)) return failure(404, 40400, 'Resource not found');
  try {
    const base = new URL(process.env.API_INTERNAL_URL ?? 'http://127.0.0.1:8080');
    if (!['http:', 'https:'].includes(base.protocol) || base.username || base.password || base.pathname !== '/' || base.search || base.hash) throw new Error('Invalid API_INTERNAL_URL');
    const url = new URL(path, base);
    if (request) url.search = new URL(request.url).search;
    const headers = new Headers();
    // Never forward client-controlled routing, authorization or proxy identity headers.
    for (const name of ['accept', 'content-type', 'origin', 'x-csrf-token', 'sec-fetch-site']) {
      const value = request?.headers.get(name);
      if (value) headers.set(name, value);
    }
    const cookie = request?.headers.get('cookie');
    if (cookie) {
      if (cookie.length > 8192) return failure(400, 40000, 'Invalid request');
      const selected = cookie.split(';').map((part) => part.trim()).filter((part) => /^(goadmin_session|goadmin_csrf)=/.test(part));
      const names = selected.map((part) => part.slice(0, part.indexOf('=')));
      if (new Set(names).size !== names.length) return failure(400, 40000, 'Invalid request');
      if (selected.length) headers.set('cookie', selected.join('; '));
    }
    if (request && (path === '/api/v1/admin/media' || mediaDetail.test(path))) return await mediaResponse(path, request, url, headers);
    const body = request && method !== 'GET' ? await readBody(request) : undefined;
    const upstream = await fetch(url, { method, headers, body: body as BodyInit | undefined, cache: 'no-store', signal: AbortSignal.timeout(5000), redirect: 'error' });
    const output = new Headers({ 'Cache-Control': 'no-store', 'X-Content-Type-Options': 'nosniff' });
    for (const name of ['content-type', 'x-request-id', 'retry-after']) {
      const value = upstream.headers.get(name);
      if (value) output.set(name, value);
    }
    // Expires contains a comma; preserve each Set-Cookie independently.
    for (const value of upstream.headers.getSetCookie()) output.append('set-cookie', value);
    const data = [204, 205, 304].includes(upstream.status) ? null : await upstream.arrayBuffer();
    return new Response(data, { status: upstream.status, headers: output });
  } catch (error) {
    if (error instanceof RangeError) return failure(413, 41300, 'Request body too large');
    return failure(503, 50302, 'API unavailable');
  }
}
