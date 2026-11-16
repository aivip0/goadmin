'use client';
import { useCallback, useEffect, useRef, useState } from 'react';
import { Activity, ArrowUpRight, CheckCircle2, Database, FileCheck2, Moon, RefreshCw, Server, Sun, TriangleAlert } from 'lucide-react';
import { useTheme } from 'next-themes';
import Link from 'next/link';
import { Button } from '@/components/ui/button';
import { getHealth } from '@/services/health';
import type { Health } from '@/types/api';

export default function Home() {
  const [health, setHealth] = useState<Health | null>(null);
  const [loading, setLoading] = useState(true);
  const [failed, setFailed] = useState(false);
  const controller = useRef<AbortController | null>(null);
  const { resolvedTheme, setTheme } = useTheme();
  const refresh = useCallback(async () => {
    controller.current?.abort();
    const current = new AbortController();
    controller.current = current;
    setLoading(true);
    try { const result = await getHealth(AbortSignal.any([current.signal, AbortSignal.timeout(7000)])); if (!current.signal.aborted) { setHealth(result); setFailed(false); } }
    catch { if (!current.signal.aborted) { setHealth(null); setFailed(true); } }
    finally { if (!current.signal.aborted) setLoading(false); }
  }, []);
  useEffect(() => {
    let disposed = false;
    queueMicrotask(() => { if (!disposed) void refresh(); });
    const timer = setInterval(() => { void refresh(); }, 30000);
    return () => { disposed = true; clearInterval(timer); controller.current?.abort(); };
  }, [refresh]);
  const ok = !loading && !failed && health !== null;
  const label = loading ? '检查中' : ok ? '正常' : '未就绪';
  const StatusIcon = ok ? CheckCircle2 : failed ? TriangleAlert : Activity;
  return <div className="min-h-screen">
    <header className="border-b border-border bg-[var(--surface)]">
      <div className="mx-auto flex min-h-18 max-w-5xl items-center justify-between gap-4 px-5 sm:px-8">
        <Link href="/" className="flex items-center gap-3 text-lg font-semibold"><span className="flex size-9 items-center justify-center rounded-md bg-primary text-primary-foreground"><Activity aria-hidden="true" size={21} /></span>GoAdmin Kit</Link>
        <Button variant="ghost" size="icon" aria-label="切换主题" title="切换主题" onClick={() => setTheme(resolvedTheme === 'dark' ? 'light' : 'dark')}><Sun className="hidden dark:block" /><Moon className="dark:hidden" /></Button>
      </div>
    </header>
    <main className="mx-auto max-w-5xl px-5 py-10 sm:px-8 sm:py-14">
      <div className="mb-9 flex flex-wrap items-end justify-between gap-5">
        <div><p className="mb-2 text-xs font-semibold uppercase text-muted-foreground">Phase 1 / Foundation</p><h1 className="text-3xl font-semibold">系统状态</h1></div>
        <Button variant="outline" onClick={() => void refresh()} disabled={loading}><RefreshCw aria-hidden="true" className={loading ? 'animate-spin' : ''} />刷新状态</Button>
      </div>
      <section aria-label="服务健康状态" className="mb-10 flex min-h-24 items-center gap-4 border-y border-border py-5" aria-live="polite" aria-busy={loading}>
        <StatusIcon size={28} aria-hidden="true" style={{ color: ok ? 'var(--good)' : failed ? 'var(--bad)' : 'var(--muted-foreground)' }} />
        <div><h2 className="text-lg font-semibold" data-testid="overall-status">{loading ? '正在检查服务' : ok ? '基础服务运行正常' : '基础服务尚未就绪'}</h2><p className="mt-1 text-sm text-muted-foreground">{failed ? '无法确认 API、数据库或 Migration 状态。' : 'API · PostgreSQL · Migration'}</p></div>
      </section>
      <section aria-label="基础设施">
        <div className="mb-4 flex items-center justify-between gap-3"><h2 className="text-base font-semibold">基础设施</h2><span className="text-xs text-muted-foreground">开发环境</span></div>
        {[{ icon: Server, title: 'Go API', detail: '/api/v1/health' }, { icon: Database, title: 'PostgreSQL', detail: '数据库连接' }, { icon: FileCheck2, title: 'Migration', detail: '数据库版本' }].map(({ icon: Icon, title, detail }) => <div className="status-row" key={title}>
          <Icon className="text-muted-foreground" size={22} aria-hidden="true" /><div className="min-w-0"><h3 className="text-sm font-semibold">{title}</h3><p className="mt-1 break-all text-xs text-muted-foreground">{detail}</p></div>
          <span className="flex items-center gap-2 whitespace-nowrap text-sm" style={{ color: ok ? 'var(--good)' : failed ? 'var(--bad)' : 'var(--muted-foreground)' }}><span className="size-1.5 rounded-full bg-current" />{label}</span>
        </div>)}
      </section>
      <footer className="mt-8 flex flex-wrap justify-between gap-4 border-t border-border pt-5 text-xs text-muted-foreground">
        <span data-testid="checked-at">{health && !loading ? `最后检查 ${new Date(health.checked_at).toLocaleTimeString('zh-CN', { hour12: false })}` : '尚无已确认的检查结果'}</span>
        <a href="/api/v1/openapi.json" className="flex items-center gap-1 hover:text-foreground">OpenAPI JSON<ArrowUpRight aria-hidden="true" size={14} /></a>
      </footer>
    </main>
  </div>;
}
