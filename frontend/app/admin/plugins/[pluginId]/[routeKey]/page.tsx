'use client';

import { useCallback, useEffect, useState } from 'react';
import { useParams } from 'next/navigation';
import Link from 'next/link';
import { CircleAlert, RefreshCw, Send } from 'lucide-react';
import { AdminShell } from '@/components/admin-shell';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { adminRequest, type PluginRoute } from '@/services/admin';
import { useLocale } from '@/components/locale-provider';

type RouteResult = { route?: PluginRoute; status?: string; [key: string]: unknown };
const idPattern = /^[a-z][a-z0-9-]{2,31}$/;
const keyPattern = /^[a-z][a-z0-9_-]{1,47}$/;

export default function PluginRoutePage() {
  const params = useParams<{ pluginId: string; routeKey: string }>();
  const { t } = useLocale();
  const pluginId = Array.isArray(params.pluginId) ? params.pluginId[0] : params.pluginId;
  const routeKey = Array.isArray(params.routeKey) ? params.routeKey[0] : params.routeKey;
  const [result, setResult] = useState<RouteResult | null>(null);
  const [method, setMethod] = useState('GET');
  const [body, setBody] = useState('{}');
  const [output, setOutput] = useState<unknown>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');

  const request = useCallback(async (verb: string, payload?: unknown) => {
    if (!idPattern.test(pluginId) || !keyPattern.test(routeKey)) {
      setError(t('插件路由标识无效。'));
      setLoading(false);
      return;
    }
    setLoading(true);
    setError('');
    try {
      const value = await adminRequest<RouteResult>(`plugins/${pluginId}/routes/${routeKey}`, verb, payload);
      setResult(value);
      setOutput(value);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : t('插件路由请求失败。'));
    } finally {
      setLoading(false);
    }
  }, [pluginId, routeKey, t]);

  useEffect(() => { const timer = window.setTimeout(() => void request('GET'), 0); return () => window.clearTimeout(timer); }, [request]);

  const submit = async () => {
    let payload: unknown;
    try { payload = JSON.parse(body); }
    catch { setError(t('请求内容必须是有效 JSON。')); return; }
    await request(method, payload);
  };

  const route = result?.route;
  return <AdminShell title={route?.title ?? '插件路由'}>
    {() => <div className="max-w-4xl space-y-5">
      <Link href="/admin/plugins" className="text-sm text-muted-foreground underline-offset-4 hover:text-foreground hover:underline">{t('返回插件管理')}</Link>
      {error && <p role="alert" className="flex items-center gap-2 text-sm text-[var(--bad)]"><CircleAlert size={17} aria-hidden="true" />{error}</p>}
      <Card>
        <CardHeader><CardTitle>{route?.title ?? t('插件路由')}</CardTitle><p className="font-mono text-xs text-muted-foreground">{pluginId} / {routeKey} · {route?.permission ?? ''}</p></CardHeader>
        <CardContent className="space-y-4">
          {result?.status === 'provider_unavailable' && <p role="status" className="text-sm text-muted-foreground">{t('此插件未提供可用的编译期 Provider。')}</p>}
          {route && route.methods.some(value => value !== 'GET') && <div className="space-y-3 rounded-md border border-border p-4">
            <div className="flex flex-wrap items-end gap-3"><label><span className="mb-1 block text-xs text-muted-foreground">{t('请求方法')}</span><select className="auth-input" value={method} onChange={event => setMethod(event.target.value)}>{route.methods.map(value => <option key={value}>{value}</option>)}</select></label><Button onClick={() => void submit()} disabled={loading}><Send size={15} aria-hidden="true" />{t('发送请求')}</Button><Button variant="outline" onClick={() => void request('GET')} disabled={loading}><RefreshCw size={15} aria-hidden="true" />{t('刷新')}</Button></div>
            {method !== 'GET' && <label className="block"><span className="mb-1 block text-xs text-muted-foreground">JSON</span><textarea className="auth-input min-h-28 font-mono" value={body} onChange={event => setBody(event.target.value)} aria-label={t('请求内容 JSON')} /> </label>}
          </div>}
          {loading ? <p role="status" className="text-sm text-muted-foreground">{t('加载中…')}</p> : output !== null && <pre className="max-h-[60dvh] overflow-auto rounded-md border border-border bg-muted/50 p-4 text-xs leading-5">{JSON.stringify(output, null, 2)}</pre>}
        </CardContent>
      </Card>
    </div>}
  </AdminShell>;
}
