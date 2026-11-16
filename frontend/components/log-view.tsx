'use client';

import { useCallback, useEffect, useState, type FormEvent } from 'react';
import { useRouter } from 'next/navigation';
import { RefreshCw, Search, X } from 'lucide-react';
import { AdminShell } from '@/components/admin-shell';
import { Button } from '@/components/ui/button';
import type { Page } from '@/services/admin';
import { AuthError } from '@/services/auth';
import { loadLogDetail, loadLogs, type LogEntry, type LogKind } from '@/services/logs';

const empty: Page<LogEntry> = { items: [], total: 0, page: 1, page_size: 20 };
const formatTime = (value: string) => new Date(value).toISOString().replace('T', ' ').slice(0, 19) + ' UTC';

export function LogView({ kind }: { kind: LogKind }) {
  const audit = kind === 'audit-logs';
  return <AdminShell permission={audit ? 'audit:view' : 'login_logs:view'} title={audit ? '操作审计' : '登录日志'}>{() => <LogTable kind={kind} />}</AdminShell>;
}

function LogTable({ kind }: { kind: LogKind }) {
  const router = useRouter();
  const audit = kind === 'audit-logs';
  const [draft, setDraft] = useState({ from: '', to: '', user_id: '', action: '', outcome: '' });
  const [filters, setFilters] = useState(draft);
  const [page, setPage] = useState(1);
  const [revision, setRevision] = useState(0);
  const [data, setData] = useState<Page<LogEntry>>(empty);
  const [selected, setSelected] = useState<LogEntry | null>(null);
  const [loading, setLoading] = useState(true);
  const [detailLoading, setDetailLoading] = useState(false);
  const [error, setError] = useState('');
  const [detailError, setDetailError] = useState('');

  useEffect(() => {
    let alive = true;
    const params = new URLSearchParams({ page: String(page), page_size: '20' });
    for (const [key, value] of Object.entries(filters)) if (value && (audit || key !== 'action') && (!audit || key !== 'outcome')) params.set(key, value);
    loadLogs(kind, params).then(result => { if (alive) { setData(result); setError(''); setLoading(false); } })
      .catch((reason: unknown) => { if (!alive) return; if (reason instanceof AuthError && reason.status === 401) { router.replace('/login'); return; } setError('无法读取日志，请重试。'); setLoading(false); });
    return () => { alive = false; };
  }, [kind, audit, filters, page, revision, router]);

  const refresh = useCallback(() => { setLoading(true); setRevision(value => value + 1); }, []);
  const submit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault(); setPage(1); setLoading(true); setSelected(null); setFilters({ ...draft }); setRevision(value => value + 1);
  };
  const select = async (id: string) => {
    setDetailLoading(true); setDetailError(''); setSelected(null);
    try { setSelected(await loadLogDetail(kind, id)); }
    catch (reason) { if (reason instanceof AuthError && reason.status === 401) router.replace('/login'); else setDetailError('无法读取详情，请重试。'); }
    finally { setDetailLoading(false); }
  };
  const pages = Math.max(1, Math.ceil(data.total / data.page_size));
  return <div className="space-y-5 pb-10">
    <form onSubmit={submit} className="admin-panel grid gap-3 sm:grid-cols-2 xl:grid-cols-[repeat(4,minmax(0,1fr))_auto]" aria-label="日志筛选">
      <label className="block"><span className="admin-label">开始日期 (UTC)</span><input type="date" className="auth-input" value={draft.from} onChange={event => setDraft({ ...draft, from: event.target.value })} /></label>
      <label className="block"><span className="admin-label">结束日期 (UTC)</span><input type="date" className="auth-input" value={draft.to} onChange={event => setDraft({ ...draft, to: event.target.value })} /></label>
      <label className="block"><span className="admin-label">管理员 UUID</span><input className="auth-input" value={draft.user_id} onChange={event => setDraft({ ...draft, user_id: event.target.value.trim() })} placeholder="按管理员筛选" /></label>
      {audit ? <label className="block"><span className="admin-label">操作代码</span><input className="auth-input" value={draft.action} onChange={event => setDraft({ ...draft, action: event.target.value.trim() })} placeholder="例如 user.update" /></label>
        : <label className="block"><span className="admin-label">登录结果</span><select className="auth-input" value={draft.outcome} onChange={event => setDraft({ ...draft, outcome: event.target.value })}><option value="">全部结果</option><option value="success">成功</option><option value="failure">失败</option><option value="rate_limited">限流</option></select></label>}
      <div className="flex items-end gap-2"><Button type="submit" disabled={loading}><Search size={16} aria-hidden="true" />筛选</Button><Button type="button" variant="outline" onClick={refresh} disabled={loading} title="刷新日志" aria-label="刷新日志"><RefreshCw size={16} aria-hidden="true" /></Button></div>
    </form>
    {error && <div className="admin-panel flex flex-wrap items-center gap-3 text-sm text-[var(--bad)]" role="alert">{error}<Button variant="outline" onClick={refresh}>重试</Button></div>}
    {loading ? <p className="admin-panel text-sm text-muted-foreground" role="status">正在读取日志…</p> : !error && <section className="admin-panel min-w-0" aria-label={audit ? '操作记录' : '登录记录'}>
      <div className="flex flex-wrap justify-between gap-2"><h2 className="font-semibold">{audit ? '操作记录' : '登录记录'}</h2><span className="text-sm text-muted-foreground">共 {data.total} 条 · UTC</span></div>
      {data.items.length ? <div className="mt-4 overflow-x-auto"><table className="w-full min-w-[720px] text-left text-sm"><thead className="border-b border-border text-xs text-muted-foreground"><tr><th className="py-3 font-medium">时间</th><th className="py-3 font-medium">管理员</th><th className="py-3 font-medium">{audit ? '操作' : '结果'}</th><th className="py-3 font-medium">{audit ? '对象' : '账号类型'}</th><th className="py-3 font-medium">详情</th></tr></thead><tbody className="divide-y divide-border">{data.items.map(item => <tr key={item.id}><td className="py-3 pr-4 whitespace-nowrap">{formatTime(item.created_at)}</td><td className="py-3 pr-4 break-all">{item.actor}</td><td className="py-3 pr-4 break-all">{audit ? item.action : item.outcome}</td><td className="py-3 pr-4 break-all">{audit ? item.target_type : item.account_kind}</td><td className="py-3"><Button variant="ghost" onClick={() => void select(item.id)} aria-label={`查看记录 ${item.id} 详情`}>查看</Button></td></tr>)}</tbody></table></div> : <p className="mt-5 text-sm text-muted-foreground">当前筛选范围内没有记录。</p>}
      <div className="mt-4 flex flex-wrap items-center justify-end gap-3 text-sm"><Button variant="outline" disabled={page <= 1} onClick={() => { setPage(page - 1); setLoading(true); }}>上一页</Button><span>第 {page} / {pages} 页</span><Button variant="outline" disabled={page >= pages} onClick={() => { setPage(page + 1); setLoading(true); }}>下一页</Button></div>
    </section>}
    {detailLoading && <p role="status" className="admin-panel text-sm">正在读取详情…</p>}
    {detailError && <p role="alert" className="admin-panel text-sm text-[var(--bad)]">{detailError}</p>}
    {selected && <section className="admin-panel" aria-label="日志详情"><div className="flex items-center justify-between gap-3"><h2 className="font-semibold">记录详情</h2><Button variant="ghost" onClick={() => setSelected(null)} title="关闭详情" aria-label="关闭详情"><X size={16} /></Button></div><dl className="mt-4 grid gap-x-6 gap-y-3 text-sm sm:grid-cols-2">{Object.entries(selected).filter(([, value]) => value !== null && value !== undefined && value !== '').map(([key, value]) => <div key={key} className="min-w-0"><dt className="text-xs text-muted-foreground">{key}</dt><dd className="mt-1 break-all">{key === 'created_at' ? formatTime(String(value)) : String(value)}</dd></div>)}</dl></section>}
  </div>;
}
