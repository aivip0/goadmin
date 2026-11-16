import type { Envelope } from '@/types/api';

export interface AdminUser { id: string; username: string; email: string; display_name: string }

export class AuthError extends Error {
  constructor(public readonly status: number, message: string) { super(message); }
}

async function request<T>(path: string, options?: RequestInit): Promise<T> {
  let response: Response;
  try {
    response = await fetch(`/api/v1/auth/${path}`, { cache: 'no-store', credentials: 'same-origin', ...options });
  } catch { throw new AuthError(503, '暂时无法连接服务，请稍后再试。'); }
  let body: Envelope<T>;
  try { body = await response.json() as Envelope<T>; }
  catch { throw new AuthError(503, '服务响应异常，请稍后再试。'); }
  if (!response.ok || body.code !== 0) {
    const message = response.status === 401 && path === 'login' ? '用户名或密码不正确。' :
      response.status === 429 ? `尝试过于频繁，请 ${response.headers.get('retry-after') ?? '稍后'} 秒后重试。` :
      response.status === 403 && body.code === 40301 ? '安全校验已失效，请重试。' :
      response.status === 401 ? '登录状态已失效，请重新登录。' : '操作失败，请稍后再试。';
    throw new AuthError(response.status, message);
  }
  return body.data as T;
}

export async function currentUser(): Promise<AdminUser> {
  return (await request<{ user: AdminUser }>('me')).user;
}

async function csrf(): Promise<string> {
  return (await request<{ csrf_token: string }>('csrf')).csrf_token;
}

export async function login(username: string, password: string): Promise<AdminUser> {
  const token = await csrf();
  const result = await request<{ user: AdminUser }>('login', {
    method: 'POST', headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': token },
    body: JSON.stringify({ username, password }),
  });
  return result.user;
}

export async function logout(all = false): Promise<void> {
  const token = await csrf();
  await request<null>(all ? 'logout-all' : 'logout', { method: 'POST', headers: { 'X-CSRF-Token': token } });
}
