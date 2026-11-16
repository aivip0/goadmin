'use client';

import { useCallback, useEffect, useState, type FormEvent } from 'react';
import { RotateCcw, Save } from 'lucide-react';
import { AdminShell } from '@/components/admin-shell';
import { Button } from '@/components/ui/button';
import { adminRequest, has, type Access, type Page } from '@/services/admin';
import type { MediaFile } from '@/services/media';

type SettingKey = 'site.name' | 'site.description' | 'branding.logo_path' | 'localization.timezone' | 'localization.language' | 'general.default_page_size';
type Values = Record<SettingKey, string | number>;
type Response = { settings: Record<SettingKey, { value?: string | number; sensitive: boolean; configured: boolean }> };
const keys: SettingKey[] = ['site.name', 'site.description', 'branding.logo_path', 'localization.timezone', 'localization.language', 'general.default_page_size'];

export default function SettingsPage() {
  return <AdminShell permission="settings:view" title="系统设置">{access => <SettingsView access={access} />}</AdminShell>;
}

function SettingsView({ access }: { access: Access }) {
  const [saved, setSaved] = useState<Values | null>(null);
  const [form, setForm] = useState<Values | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [notice, setNotice] = useState('');
  const [images, setImages] = useState<MediaFile[]>([]);
  const writable = has(access, 'settings:update');
  const load = useCallback(async () => {
    setError('');
    try {
      const result = await adminRequest<Response>('settings');
      const values = {} as Values;
      for (const key of keys) {
        if (result.settings[key]?.sensitive || result.settings[key]?.value === undefined) throw new Error('设置数据不可用');
        (values as Record<string, string | number>)[key] = result.settings[key].value;
      }
      setSaved(values); setForm({ ...values });
    } catch (e) { setError(e instanceof Error ? e.message : '无法加载设置。'); }
  }, []);
  useEffect(() => { queueMicrotask(() => { void load(); }); }, [load]);
  useEffect(() => {
    if (!has(access, 'media:view')) return;
    adminRequest<Page<MediaFile>>('media?type=image&page=1&page_size=100').then(result => setImages(result.items)).catch(() => setImages([]));
  }, [access]);
  const dirty = !!form && !!saved && keys.some(key => form[key] !== saved[key]);
  useEffect(() => {
    if (!dirty) return;
    const warn = (event: BeforeUnloadEvent) => { event.preventDefault(); };
    window.addEventListener('beforeunload', warn);
    return () => window.removeEventListener('beforeunload', warn);
  }, [dirty]);
  const update = (key: SettingKey, value: string | number) => setForm(previous => previous ? { ...previous, [key]: value } : previous);
  const submit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault(); if (!form || !saved || !dirty || !writable) return;
    const changes: Record<string, string | number> = {};
    for (const key of keys) if (form[key] !== saved[key]) changes[key] = form[key];
    setBusy(true); setError(''); setNotice('');
    try { await adminRequest<Response>('settings', 'PUT', { settings: changes }); await load(); setNotice('系统设置已保存。'); }
    catch (e) { setError(e instanceof Error ? e.message : '保存失败，请重试。'); }
    finally { setBusy(false); }
  };
  if (!form) return <section className="admin-panel" role={error ? 'alert' : 'status'}>{error ? <><p>{error}</p><Button className="mt-3" variant="outline" onClick={() => void load()}>重试</Button></> : '正在加载设置…'}</section>;
  return <form onSubmit={event => void submit(event)} className="space-y-5">
    <p className="text-sm text-muted-foreground">只管理非敏感的站点展示与本地化参数。数据库时间始终为 UTC；更改时区只影响展示。</p>
    {error && <div role="alert" className="admin-panel text-[var(--bad)]">{error}</div>}
    {notice && <p role="status" className="admin-panel">{notice}</p>}
    <section className="admin-panel space-y-4"><h2 className="text-lg font-semibold">基本信息</h2>
      <label className="block"><span className="admin-label">站点名称</span><input className="auth-input" required maxLength={100} disabled={!writable || busy} value={form['site.name']} onChange={e => update('site.name', e.target.value)} /></label>
      <label className="block"><span className="admin-label">站点描述</span><textarea className="auth-input min-h-24" maxLength={500} disabled={!writable || busy} value={form['site.description']} onChange={e => update('site.description', e.target.value)} /></label>
    </section>
    <div className="grid gap-5 xl:grid-cols-2"><section className="admin-panel space-y-4"><h2 className="text-lg font-semibold">品牌标识</h2>
      <label className="block"><span className="admin-label">Logo 同源路径</span><input className="auth-input" placeholder="/logo.svg" maxLength={2048} disabled={!writable || busy} value={form['branding.logo_path']} onChange={e => update('branding.logo_path', e.target.value)} /></label>
      {has(access, 'media:view') && <label className="block"><span className="admin-label">从媒体库选择图片</span><select className="auth-input" disabled={!writable || busy} value={images.some(item => form['branding.logo_path'] === `/api/v1/admin/media/${item.id}/content`) ? form['branding.logo_path'] : ''} onChange={event => update('branding.logo_path', event.target.value)}><option value="">选择已验证的图片</option>{images.map(item => <option key={item.id} value={`/api/v1/admin/media/${item.id}/content`}>{item.original_name}</option>)}</select></label>}
      <p className="text-xs text-muted-foreground">媒体库图片保持私有，仅登录且拥有 media:view 权限的管理员可以读取。旧的站内路径仍可使用。</p>
    </section><section className="admin-panel space-y-4"><h2 className="text-lg font-semibold">语言与时区</h2>
      <label className="block"><span className="admin-label">IANA 时区</span><input className="auth-input" required maxLength={64} placeholder="Asia/Shanghai" disabled={!writable || busy} value={form['localization.timezone']} onChange={e => update('localization.timezone', e.target.value)} /></label>
      <label className="block"><span className="admin-label">显示语言</span><select className="auth-input" disabled={!writable || busy} value={form['localization.language']} onChange={e => update('localization.language', e.target.value)}><option value="en">English</option><option value="zh-CN">简体中文</option><option value="zh-TW">繁體中文</option></select></label>
    </section></div>
    <section className="admin-panel space-y-3"><h2 className="text-lg font-semibold">常规偏好</h2><label className="block max-w-xs"><span className="admin-label">默认分页条数</span><select className="auth-input" disabled={!writable || busy} value={form['general.default_page_size']} onChange={e => update('general.default_page_size', Number(e.target.value))}>{[10, 25, 50, 100].map(size => <option key={size} value={size}>{size}</option>)}</select></label><p className="text-xs text-muted-foreground">供后续模块使用；不改变现有列表的分页行为。</p></section>
    <div className="flex flex-wrap gap-3">{writable && <Button type="submit" disabled={busy || !dirty}><Save size={16} aria-hidden="true" />{busy ? '保存中…' : '保存更改'}</Button>}{dirty && <Button type="button" variant="outline" disabled={busy} onClick={() => setForm(saved && { ...saved })}><RotateCcw size={16} aria-hidden="true" />撤销未保存更改</Button>}</div>
  </form>;
}
