'use client';

import { useCallback, useEffect, useState, type FormEvent } from 'react';
import { KeyRound, RefreshCw } from 'lucide-react';
import { AdminShell } from '@/components/admin-shell';
import { Button } from '@/components/ui/button';
import { adminRequest, has, type Access, type Page } from '@/services/admin';

type Key = { id: string; name: string; prefix: string; scopes: string[]; expires_at: string; revoked_at: string | null; last_used_at: string | null; created_at: string };
type Issued = Key & { token: string };

export default function APIKeysPage() { return <AdminShell title="API Keys" permission="api_keys:view">{access => <KeysView access={access} />}</AdminShell>; }

function KeysView({ access }: { access: Access }) {
  const [data, setData] = useState<Page<Key>>({ items: [], total: 0, page: 1, page_size: 20 });
  const [page, setPage] = useState(1);
  const [revision, setRevision] = useState(0);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [name, setName] = useState('');
  const [days, setDays] = useState(30);
  const [issued, setIssued] = useState('');
  const [working, setWorking] = useState(false);
  const refresh = useCallback(() => setRevision(n => n + 1), []);
  useEffect(() => { let live = true; adminRequest<Page<Key>>(`api-keys?page=${page}&page_size=20`).then(value => { if (live) { setData(value); setError(''); } }).catch(() => { if (live) setError('无法加载 API Keys。'); }).finally(() => { if (live) setLoading(false); }); return () => { live = false; }; }, [page, revision]);
  async function create(event: FormEvent) { event.preventDefault(); setWorking(true); setIssued(''); try { const value = await adminRequest<Issued>('api-keys', 'POST', { name, expires_in_days: days }); setIssued(value.token); setName(''); refresh(); } catch { setError('创建失败，请检查名称、有效期和权限。'); } finally { setWorking(false); } }
  async function revoke(id: string) { if (!window.confirm('撤销后该密钥立即失效，确定继续？')) return; setWorking(true); try { await adminRequest(`api-keys/${id}/revoke`, 'POST'); refresh(); } catch { setError('撤销失败，请重试。'); } finally { setWorking(false); } }
  const pages = Math.max(1, Math.ceil(data.total / data.page_size));
  return <div className="space-y-5 pb-10">
    <p className="text-sm text-muted-foreground">密钥仅用于只读 Dashboard API。创建时只显示一次，服务端仅保存摘要；请保存在安全的凭据管理器中。</p>
    {has(access, 'api_keys:create') && <form onSubmit={create} className="admin-panel flex flex-wrap items-end gap-3"><label className="min-w-48 flex-1"><span className="admin-label">密钥名称</span><input className="auth-input" required maxLength={100} value={name} onChange={e => setName(e.target.value)} /></label><label className="w-36"><span className="admin-label">有效期（天）</span><input className="auth-input" type="number" min={1} max={90} value={days} onChange={e => setDays(Number(e.target.value))} /></label><Button type="submit" disabled={working || !name.trim()}><KeyRound size={16} aria-hidden="true" />创建</Button></form>}
    {issued && <section role="status" className="admin-panel border border-[var(--warning)]"><h2 className="font-semibold">新密钥，仅此一次显示</h2><code className="mt-3 block break-all select-all text-sm">{issued}</code><Button className="mt-3" variant="outline" onClick={() => setIssued('')}>已安全保存，关闭</Button></section>}
    {error && <div className="admin-panel flex gap-3 text-sm text-[var(--bad)]" role="alert">{error}<Button variant="outline" onClick={refresh}>重试</Button></div>}
    <section className="admin-panel"><div className="flex items-center justify-between gap-3"><h2 className="font-semibold">我的密钥 · {data.total}</h2><Button variant="outline" title="刷新密钥" aria-label="刷新密钥" onClick={() => { setLoading(true); refresh(); }}><RefreshCw size={16} /></Button></div>
      {loading ? <p role="status" className="mt-4 text-sm">正在加载…</p> : data.items.length ? <ul className="mt-4 divide-y divide-border">{data.items.map(item => <li key={item.id} className="flex flex-wrap items-center justify-between gap-4 py-3"><div><strong className="text-sm">{item.name}</strong><p className="mt-1 text-xs text-muted-foreground">{item.prefix}… · {item.revoked_at ? '已撤销' : new Date(item.expires_at) <= new Date() ? '已过期' : `到期 ${new Date(item.expires_at).toLocaleDateString()}`}</p></div>{!item.revoked_at && has(access, 'api_keys:revoke') && <Button variant="outline" disabled={working} onClick={() => void revoke(item.id)}>撤销</Button>}</li>)}</ul> : <p className="mt-4 text-sm text-muted-foreground">尚无 API Key。</p>}
      <div className="mt-4 flex justify-end gap-3 text-sm"><Button variant="outline" disabled={page <= 1} onClick={() => { setLoading(true); setPage(page - 1); }}>上一页</Button><span className="self-center">{page} / {pages}</span><Button variant="outline" disabled={page >= pages} onClick={() => { setLoading(true); setPage(page + 1); }}>下一页</Button></div>
    </section>
  </div>;
}
