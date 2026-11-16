'use client';

import { useCallback, useEffect, useState } from 'react';
import { ListTree, RefreshCw } from 'lucide-react';
import { AdminShell } from '@/components/admin-shell';
import { Button } from '@/components/ui/button';
import { adminRequest, has, type Access, type Page } from '@/services/admin';

type Job = { id: string; kind: string; status: 'queued'|'running'|'succeeded'|'failed'; attempts: number; affected_rows: number; last_failure: string|null; retry_of: string|null; created_at: string };
type Run = { id: string; attempt: number; status: string; failure_code: string|null; affected_rows: number; started_at: string; finished_at: string|null };
type Detail = Job & { runs: Run[] };
const status: Record<Job['status'],string> = { queued: '等待执行', running: '执行中', succeeded: '已完成', failed: '失败' };

export default function JobsPage() { return <AdminShell title="后台任务" permission="jobs:view">{access => <JobsView access={access} />}</AdminShell>; }
function JobsView({ access }: { access: Access }) {
  const [data, setData] = useState<Page<Job>>({ items: [], total: 0, page: 1, page_size: 20 });
  const [page, setPage] = useState(1);
  const [revision, setRevision] = useState(0);
  const [loading, setLoading] = useState(true);
  const [working, setWorking] = useState(false);
  const [detail, setDetail] = useState<Detail|null>(null);
  const [error, setError] = useState('');
  const refresh = useCallback(() => setRevision(n => n + 1), []);
  useEffect(() => { let live = true; adminRequest<Page<Job>>(`jobs?page=${page}&page_size=20`).then(value => { if (live) { setData(value); setError(''); } }).catch(() => { if (live) setError('任务列表加载失败。'); }).finally(() => { if (live) setLoading(false); }); return () => { live = false; }; }, [page, revision]);
  async function enqueue() { setWorking(true); try { await adminRequest('jobs', 'POST', { kind: 'session_cleanup' }); refresh(); } catch { setError('提交任务失败。'); } finally { setWorking(false); } }
  async function retry(id: string) { setWorking(true); try { await adminRequest(`jobs/${id}/retry`, 'POST'); refresh(); } catch { setError('重试失败，任务可能不处于失败状态或已重试。'); } finally { setWorking(false); } }
  async function select(id: string) { try { setDetail(await adminRequest<Detail>(`jobs/${id}`)); } catch { setError('执行记录加载失败。'); } }
  const pages = Math.max(1, Math.ceil(data.total / data.page_size));
  return <div className="space-y-5 pb-10">
    <section className="admin-panel flex flex-wrap items-center justify-between gap-3"><div><h2 className="font-semibold">会话清理</h2><p className="mt-1 text-sm text-muted-foreground">按批清理过期超过 30 天的历史会话，每次最多 100 条。执行与重试记录持久保存。</p></div>{has(access, 'jobs:create') && <Button disabled={working} onClick={() => void enqueue()}><ListTree size={16} aria-hidden="true" />提交任务</Button>}</section>
    {error && <div className="admin-panel flex gap-3 text-sm text-[var(--bad)]" role="alert">{error}<Button variant="outline" onClick={refresh}>重试</Button></div>}
    <section className="admin-panel"><div className="flex items-center justify-between gap-3"><h2 className="font-semibold">执行队列 · {data.total}</h2><Button variant="outline" aria-label="刷新任务" title="刷新任务" onClick={() => { setLoading(true); refresh(); }}><RefreshCw size={16} /></Button></div>
      {loading ? <p role="status" className="mt-4 text-sm">正在加载…</p> : data.items.length ? <ul className="mt-4 divide-y divide-border">{data.items.map(job => <li key={job.id} className="flex flex-wrap items-center justify-between gap-3 py-3"><div><strong className="text-sm">{job.kind}</strong><p className="mt-1 text-xs text-muted-foreground">{status[job.status]} · 第 {job.attempts} 次尝试 · 处理 {job.affected_rows} 条 · {new Date(job.created_at).toLocaleString()}</p></div><div className="flex gap-2"><Button variant="outline" onClick={() => void select(job.id)}>详情</Button>{job.status === 'failed' && has(access, 'jobs:retry') && <Button variant="outline" disabled={working} onClick={() => void retry(job.id)}>重试</Button>}</div></li>)}</ul> : <p className="mt-4 text-sm text-muted-foreground">暂无任务。</p>}
      <div className="mt-4 flex justify-end gap-3 text-sm"><Button variant="outline" disabled={page <= 1} onClick={() => { setLoading(true); setPage(page-1); }}>上一页</Button><span className="self-center">{page} / {pages}</span><Button variant="outline" disabled={page >= pages} onClick={() => { setLoading(true); setPage(page+1); }}>下一页</Button></div>
    </section>
    {detail && <section className="admin-panel"><div className="flex justify-between gap-3"><h2 className="font-semibold">执行详情</h2><Button variant="ghost" onClick={() => setDetail(null)}>关闭</Button></div><p className="mt-2 break-all text-xs text-muted-foreground">任务 ID：{detail.id}</p>{detail.runs.length ? <ul className="mt-3 divide-y divide-border text-sm">{detail.runs.map(run => <li key={run.id} className="py-2">第 {run.attempt} 次 · {status[run.status as Job['status']] ?? run.status} · 处理 {run.affected_rows} 条{run.failure_code ? ` · ${run.failure_code}` : ''}</li>)}</ul> : <p className="mt-3 text-sm text-muted-foreground">尚无执行记录。</p>}</section>}
  </div>;
}
