'use client';

import { useCallback, useEffect, useMemo, useState } from 'react';
import { Check, CircleAlert, Clock3, Download, Puzzle, RefreshCw, Settings2, ShieldCheck, Trash2, Upload, X } from 'lucide-react';
import { AdminShell } from '@/components/admin-shell';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { adminRequest, has, type Access, type Plugin, type PluginConfig, type PluginEvent, type PluginPackage } from '@/services/admin';
import { useLocale } from '@/components/locale-provider';

type Tab = 'installed' | 'packages' | 'events';
const states: Record<Plugin['status'], string> = { installed: '已安装', enabled: '启用', disabled: '停用', failed: '插件状态异常' };
const compareVersion = (a: string, b: string) => {
  const left = a.split(/[+-]/)[0].split('.').map(Number); const right = b.split(/[+-]/)[0].split('.').map(Number);
  for (let i = 0; i < 3; i++) if ((left[i] ?? 0) !== (right[i] ?? 0)) return (left[i] ?? 0) - (right[i] ?? 0);
  return a.localeCompare(b);
};

export default function PluginsPage() {
  return <AdminShell title="插件管理" permission="plugins:view">{access => <PluginConsole access={access} />}</AdminShell>;
}

function PluginConsole({ access }: { access: Access }) {
  const { t, locale } = useLocale();
  const [tab, setTab] = useState<Tab>('installed');
  const [plugins, setPlugins] = useState<Plugin[]>([]);
  const [packages, setPackages] = useState<PluginPackage[]>([]);
  const [events, setEvents] = useState<PluginEvent[]>([]);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState('');
  const [error, setError] = useState('');
  const [notice, setNotice] = useState('');
  const [configPlugin, setConfigPlugin] = useState<Plugin | null>(null);
  const [configFields, setConfigFields] = useState<PluginConfig[]>([]);
  const [configValues, setConfigValues] = useState<Record<string, string>>({});

  const refresh = useCallback(async () => {
    setLoading(true); setError('');
    try {
      const [installed, trusted, history] = await Promise.all([
        adminRequest<Plugin[]>('plugins'), adminRequest<PluginPackage[]>('plugins/packages'), adminRequest<PluginEvent[]>('plugins/events?limit=50'),
      ]);
      setPlugins(installed); setPackages(trusted); setEvents(history);
    } catch { setError(t('插件数据加载失败，请重试。')); }
    finally { setLoading(false); }
  }, [t]);
  useEffect(() => { const timer = window.setTimeout(() => void refresh(), 0); return () => window.clearTimeout(timer); }, [refresh]);

  const installedByID = useMemo(() => new Map(plugins.map(plugin => [plugin.id, plugin])), [plugins]);
  const perform = async (key: string, path: string, method: string, success = '插件操作已完成。') => {
    setBusy(key); setError(''); setNotice('');
    try { await adminRequest(path, method, method === 'POST' ? {} : undefined); setNotice(t(success)); await refresh(); }
    catch { setError(t('插件操作失败，请检查依赖和签名包。')); }
    finally { setBusy(''); }
  };
  const openConfig = async (plugin: Plugin) => {
    setBusy(plugin.id); setError('');
    try { const fields = await adminRequest<PluginConfig[]>(`plugins/${plugin.id}/config`); setConfigFields(fields); setConfigValues(Object.fromEntries(fields.map(field => [field.key, field.value === undefined ? '' : typeof field.value === 'string' ? field.value : JSON.stringify(field.value)]))); setConfigPlugin(plugin); }
    catch { setError(t('插件数据加载失败，请重试。')); }
    finally { setBusy(''); }
  };
  const saveConfig = async () => {
    if (!configPlugin) return;
    const values: Record<string, unknown> = {};
    try {
      for (const field of configFields) {
        const raw = configValues[field.key] ?? '';
        if (field.secret && raw === '') continue;
        if (raw === '' && !field.required) continue;
        values[field.key] = field.type === 'integer' ? Number(raw) : field.type === 'boolean' ? raw === 'true' : field.type === 'json' ? JSON.parse(raw) : raw;
      }
    } catch { setError(t('插件操作失败，请检查依赖和签名包。')); return; }
    setBusy(configPlugin.id); setError('');
    try { await adminRequest(`plugins/${configPlugin.id}/config`, 'PUT', { values }); setNotice(t('配置已保存。')); setConfigPlugin(null); await refresh(); }
    catch { setError(t('插件操作失败，请检查依赖和签名包。')); }
    finally { setBusy(''); }
  };

  const date = (value: string) => new Intl.DateTimeFormat(locale, { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(value));
  return <div className="space-y-5">
    <div className="flex flex-wrap items-center justify-between gap-3">
      <div><p className="text-sm text-muted-foreground">{t('可信本地包：服务端仅列出签名有效且文件校验通过的版本。不会执行上传的二进制、Shell 或 JavaScript。')}</p></div>
      <Button variant="outline" onClick={() => void refresh()} disabled={loading}><RefreshCw aria-hidden="true" />{t('刷新插件')}</Button>
    </div>
    {error && <p role="alert" className="flex items-center gap-2 text-sm text-[var(--bad)]"><CircleAlert size={17} aria-hidden="true" />{error}</p>}
    {notice && <p role="status" className="flex items-center gap-2 text-sm text-[var(--good)]"><Check size={17} aria-hidden="true" />{notice}</p>}
    <div role="tablist" aria-label={t('插件管理')} className="flex gap-2 border-b border-border">
      {(['installed', 'packages', 'events'] as const).map(value => <button key={value} role="tab" aria-selected={tab === value} className={`min-h-11 border-b-2 px-3 text-sm font-medium ${tab === value ? 'border-primary text-foreground' : 'border-transparent text-muted-foreground hover:text-foreground'}`} onClick={() => setTab(value)}>{t(value === 'installed' ? '已安装' : value === 'packages' ? '本地可信包' : '事件记录')}</button>)}
    </div>
    {loading ? <p role="status" className="py-10 text-center text-sm text-muted-foreground">{t('加载中…')}</p> : tab === 'installed' ? plugins.length === 0 ? <Empty text={t('没有已安装的插件。')} /> : <div className="grid gap-4 xl:grid-cols-2">
      {plugins.map(plugin => <Card key={plugin.id}>
        <CardHeader><div className="flex min-w-0 items-start gap-3"><span className="flex size-10 shrink-0 items-center justify-center rounded-md bg-muted"><Puzzle size={19} aria-hidden="true" /></span><div className="min-w-0"><CardTitle>{plugin.name}</CardTitle><p className="mt-1 break-all font-mono text-xs text-muted-foreground">{plugin.id} · {plugin.version}</p></div><Status status={plugin.status} label={t(states[plugin.status])} /></div></CardHeader>
        <CardContent className="space-y-4"><p className="text-sm leading-6 text-muted-foreground">{plugin.description || t('描述')}</p>
          <div className="grid gap-3 text-sm sm:grid-cols-2"><Meta label={t('作者')} value={plugin.author || '—'} /><Meta label={t('安装时间')} value={date(plugin.installed_at)} /><Meta label={t('权限')} value={String(plugin.manifest.permissions.length)} /><Meta label={t('路由')} value={String(plugin.manifest.routes.length)} /></div>
          {plugin.failure_code && <p role="alert" className="rounded-md border border-[var(--bad)]/30 bg-[var(--bad)]/5 p-3 text-sm text-[var(--bad)]">{t('插件状态异常')} · {plugin.failure_code}</p>}
          <div className="flex flex-wrap gap-2 border-t border-border pt-4">
            {has(access, 'plugins:configure') && plugin.manifest.config.length > 0 && <Button variant="outline" disabled={!!busy} onClick={() => void openConfig(plugin)}><Settings2 aria-hidden="true" />{t('配置')}</Button>}
            {plugin.status !== 'enabled' && has(access, 'plugins:enable') && <Button disabled={!!busy} onClick={() => void perform(plugin.id, `plugins/${plugin.id}/enable`, 'POST')}>{t('启用插件')}</Button>}
            {plugin.status === 'enabled' && has(access, 'plugins:disable') && <Button variant="outline" disabled={!!busy} onClick={() => void perform(plugin.id, `plugins/${plugin.id}/disable`, 'POST')}>{t('停用插件')}</Button>}
            {has(access, 'plugins:uninstall') && plugin.status !== 'enabled' && <Button variant="outline" disabled={!!busy} className="text-[var(--bad)]" onClick={() => { if (window.confirm(t('确认卸载该插件？其插件数据迁移不会自动回滚，事件记录会保留。'))) void perform(plugin.id, `plugins/${plugin.id}/uninstall`, 'POST'); }}><Trash2 aria-hidden="true" />{t('卸载插件')}</Button>}
          </div>
        </CardContent>
      </Card>)}
    </div> : tab === 'packages' ? packages.length === 0 ? <Empty text={t('可信包目录中没有可验证的插件包。')} /> : <div className="grid gap-4 xl:grid-cols-2">
      {packages.map(pkg => { const installed = installedByID.get(pkg.manifest.id); const isUpgrade = installed && compareVersion(pkg.version, installed.version) > 0; return <Card key={`${pkg.manifest.id}:${pkg.version}`}>
        <CardHeader><div className="flex min-w-0 items-start gap-3"><span className="flex size-10 shrink-0 items-center justify-center rounded-md bg-muted"><ShieldCheck size={19} aria-hidden="true" /></span><div className="min-w-0"><CardTitle>{pkg.manifest.name}</CardTitle><p className="mt-1 break-all font-mono text-xs text-muted-foreground">{pkg.manifest.id} · {pkg.version}</p></div></div></CardHeader>
        <CardContent className="space-y-4"><p className="text-sm leading-6 text-muted-foreground">{pkg.manifest.description || t('描述')}</p><div className="grid gap-3 text-sm sm:grid-cols-2"><Meta label={t('作者')} value={pkg.manifest.author || '—'} /><Meta label={t('签名密钥')} value={pkg.key_id} /><Meta label={t('依赖')} value={String(pkg.manifest.dependencies.length)} /><Meta label={t('权限')} value={String(pkg.manifest.permissions.length)} /></div>
          <div className="flex flex-wrap gap-2 border-t border-border pt-4">{!installed && has(access, 'plugins:install') && <Button disabled={!!busy} onClick={() => { setBusy(pkg.manifest.id); void adminRequest('plugins/install', 'POST', { id: pkg.manifest.id, version: pkg.version }).then(async () => { setNotice(t('插件已安装。')); await refresh(); }).catch(() => setError(t('插件操作失败，请检查依赖和签名包。'))).finally(() => setBusy('')); }}><Download aria-hidden="true" />{t('安装插件')}</Button>}
            {isUpgrade && has(access, 'plugins:migrate') && <Button disabled={!!busy} onClick={() => void perform(pkg.manifest.id, `plugins/${pkg.manifest.id}/upgrade`, 'POST', '插件已更新。')}><Upload aria-hidden="true" />{t('升级插件')}</Button>}
            {installed && !isUpgrade && <span className="self-center text-sm text-muted-foreground">{installed.version === pkg.version ? t('已安装') : '—'}</span>}
          </div>
        </CardContent>
      </Card>; })}
    </div> : events.length === 0 ? <Empty text={t('暂无插件事件。')} /> : <div className="overflow-hidden rounded-md border border-border"><div className="hidden grid-cols-[minmax(0,1fr)_130px_120px_120px] gap-4 bg-muted px-4 py-3 text-xs font-semibold uppercase text-muted-foreground md:grid"><span>{t('插件')}</span><span>{t('事件')}</span><span>{t('结果')}</span><span><Clock3 className="mr-1 inline" size={14} />{t('更新时间')}</span></div>{events.map(event => <div key={event.id} className="grid gap-2 border-t border-border px-4 py-3 text-sm first:border-t-0 md:grid-cols-[minmax(0,1fr)_130px_120px_120px] md:items-center md:gap-4"><span className="font-mono text-xs">{event.plugin_id} · {event.plugin_version}</span><span>{t(event.event_type)}</span><span className={event.outcome === 'failed' ? 'text-[var(--bad)]' : 'text-[var(--good)]'}>{t(event.outcome === 'failed' ? '失败' : '成功')}{event.failure_code ? ` · ${event.failure_code}` : ''}</span><time className="text-xs text-muted-foreground">{date(event.created_at)}</time></div>)}</div>}

    {configPlugin && <div className="fixed inset-0 z-[100] grid place-items-center bg-black/50 p-4" role="presentation" onMouseDown={event => { if (event.target === event.currentTarget) setConfigPlugin(null); }}><section role="dialog" aria-modal="true" aria-labelledby="plugin-config-title" className="max-h-[90dvh] w-full max-w-xl overflow-y-auto rounded-md border border-border bg-[var(--surface)] p-5 shadow-xl"><div className="mb-5 flex items-start justify-between gap-4"><div><h2 id="plugin-config-title" className="text-lg font-semibold">{configPlugin.name} · {t('配置')}</h2><p className="mt-1 text-sm text-muted-foreground">{t('敏感配置不回显，留空表示保持原值。')}</p></div><Button variant="ghost" size="icon" aria-label={t('关闭')} onClick={() => setConfigPlugin(null)}><X aria-hidden="true" /></Button></div><div className="space-y-4">{configFields.map(field => <label key={field.key} className="block"><span className="mb-1 block text-sm font-medium">{field.label}{field.required ? ' *' : ''}</span>{field.type === 'boolean' ? <select className="auth-input" value={configValues[field.key] || 'false'} onChange={e => setConfigValues(v => ({ ...v, [field.key]: e.target.value }))}><option value="true">true</option><option value="false">false</option></select> : field.type === 'json' ? <textarea className="auth-input min-h-28 font-mono" value={configValues[field.key] ?? ''} onChange={e => setConfigValues(v => ({ ...v, [field.key]: e.target.value }))} /> : <input className="auth-input" type={field.secret ? 'password' : field.type === 'integer' ? 'number' : 'text'} autoComplete={field.secret ? 'new-password' : 'off'} value={configValues[field.key] ?? ''} placeholder={field.secret && field.configured ? t('已配置（值已隐藏）') : field.configured ? '' : t('配置未设置')} onChange={e => setConfigValues(v => ({ ...v, [field.key]: e.target.value }))} />}<span className="mt-1 block text-xs text-muted-foreground">{field.key}{field.secret ? ` · ${t('已配置（值已隐藏）')}` : ''}</span></label>)}</div><div className="mt-6 flex justify-end gap-2"><Button variant="outline" onClick={() => setConfigPlugin(null)}>{t('取消')}</Button><Button disabled={!!busy} onClick={() => void saveConfig()}>{t('保存')}</Button></div></section></div>}
  </div>;
}

function Empty({ text }: { text: string }) { return <div className="grid min-h-48 place-items-center rounded-md border border-dashed border-border px-5 text-center text-sm text-muted-foreground">{text}</div>; }
function Meta({ label, value }: { label: string; value: string }) { return <div className="min-w-0"><span className="block text-xs text-muted-foreground">{label}</span><span className="block break-all font-medium">{value}</span></div>; }
function Status({ status, label }: { status: Plugin['status']; label: string }) { return <span className={`ml-auto shrink-0 rounded-full border px-2.5 py-1 text-xs font-medium ${status === 'enabled' ? 'border-[var(--good)]/30 bg-[var(--good)]/10 text-[var(--good)]' : status === 'failed' ? 'border-[var(--bad)]/30 bg-[var(--bad)]/10 text-[var(--bad)]' : 'border-border text-muted-foreground'}`}>{label}</span>; }
