import type { Envelope } from '@/types/api';
import { AuthError } from '@/services/auth';

export interface Permission { id: string; code: string; name: string; description: string }
export interface Role { id: string; code: string; name: string; description: string; status: string; is_system: boolean; is_super_admin: boolean; permissions: Permission[]; users: { id: string; username: string; status: string }[]; created_at: string; updated_at: string }
export interface User { id: string; username: string; email: string; display_name: string; status: string; roles: Role[]; permissions: string[]; created_at: string; updated_at: string }
export interface Page<T> { items: T[]; total: number; page: number; page_size: number }
export interface Access { permissions: string[]; is_super_admin: boolean }
export interface MenuNode { id: string; name: string; menu_type: 'directory' | 'page' | 'external'; path?: string; component_key?: string; icon?: string; is_external: boolean; children: MenuNode[] }
export interface Menu { id: string; parent_id: string | null; code: string; name: string; menu_type: 'directory' | 'page' | 'external'; path: string; component_key: string; icon: string; sort_order: number; visible: boolean; status: string; permission_code: string; is_system: boolean; role_ids: string[] }
export interface MenuRole { id: string; code: string; name: string }
export interface PluginRoute { key: string; title: string; permission: string; view: 'overview'|'settings'|'records'; methods: string[] }
export interface PluginManifest { id: string; name: string; version: string; description: string; author: string; dependencies: { id: string; version: string }[]; permissions: Permission[]; menus: { key: string; name: string; route_key: string; icon: string; sort_order: number; permission: string }[]; routes: PluginRoute[]; config: { key: string; label: string; type: 'string'|'integer'|'boolean'|'json'; required: boolean; secret: boolean }[] }
export interface Plugin { id: string; name: string; version: string; description: string; author: string; status: 'installed'|'enabled'|'disabled'|'failed'; failure_code?: string; installed_at: string; updated_at: string; manifest: PluginManifest }
export interface PluginPackage { manifest: PluginManifest; version: string; key_id: string }
export interface PluginEvent { id: string; plugin_id: string; plugin_version: string; event_type: string; outcome: 'succeeded'|'failed'; failure_code?: string; created_at: string }
export interface PluginConfig { key: string; label: string; type: 'string'|'integer'|'boolean'|'json'; required: boolean; secret: boolean; configured: boolean; value?: unknown }

export async function adminRequest<T>(path: string, method = 'GET', payload?: unknown): Promise<T> {
  const headers: Record<string, string> = {};
  if (method !== 'GET') {
    const csrf = await fetch('/api/v1/auth/csrf', { cache: 'no-store', credentials: 'same-origin' });
    if (csrf.status === 401) throw new AuthError(401, '登录状态已失效。');
    if (!csrf.ok) throw new AuthError(csrf.status, '无法获取安全校验信息。');
    const result = await csrf.json() as Envelope<{ csrf_token: string }>;
    if (!result.data?.csrf_token) throw new AuthError(503, '安全校验信息无效。');
    headers['X-CSRF-Token'] = result.data.csrf_token;
    if (payload !== undefined) headers['Content-Type'] = 'application/json';
  }
  let response: Response;
  try {
    response = await fetch(`/api/v1/admin/${path}`, { method, headers, body: payload === undefined ? undefined : JSON.stringify(payload), credentials: 'same-origin', cache: 'no-store' });
  } catch { throw new AuthError(503, '暂时无法连接服务。'); }
  let body: Envelope<T>;
  try { body = await response.json() as Envelope<T>; } catch { throw new AuthError(503, '服务响应异常。'); }
  if (!response.ok || body.code !== 0) {
    const messages: Record<number, string> = { 400: '输入格式无效。', 401: '登录状态已失效。', 403: '没有执行此操作的权限。', 404: '记录已不存在。', 409: '操作冲突：请检查唯一字段、关联关系或最后一位超级管理员。', 422: '业务校验失败。', 429: '尝试过于频繁，请稍后重试。' };
    throw new AuthError(response.status, messages[response.status] ?? '操作失败，请稍后重试。');
  }
  return body.data as T;
}
export const has = (access: Access | null, code: string) => !!access && (access.is_super_admin || access.permissions.includes(code));
