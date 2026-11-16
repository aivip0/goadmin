'use client';

import { useCallback, useEffect, useState } from 'react';
import Link from 'next/link';
import { useRouter } from 'next/navigation';
import { Activity, ArrowUpRight, Clock3, LogOut, Monitor, RefreshCw, ShieldCheck, UsersRound } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { logout, AuthError, type AdminUser } from '@/services/auth';
import { AdminShell } from '@/components/admin-shell';
import { has, type Access, type MenuNode } from '@/services/admin';
import { loadDashboard, type ActivityPage, type DashboardSummary, type DashboardTrends } from '@/services/dashboard';
import { useLocale } from '@/components/locale-provider';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';

const pagePermissions: Record<string,string> = { '/admin':'dashboard:view', '/admin/users':'users:view', '/admin/roles':'roles:view', '/admin/permissions':'permissions:view', '/admin/menus':'menus:view', '/admin/settings':'settings:view', '/admin/media':'media:view', '/admin/audit-logs':'audit:view', '/admin/login-logs':'login_logs:view' };
function quickLinks(menus: MenuNode[], access: Access): { id: string; name: string; path: string }[] {
  const links: { id: string; name: string; path: string }[] = [];
  const visit = (nodes: MenuNode[]) => nodes.forEach(node => {
    if (node.menu_type === 'page' && node.path && node.path !== '/admin' && pagePermissions[node.path] && has(access, pagePermissions[node.path])) links.push({id:node.id,name:node.name,path:node.path});
    visit(node.children ?? []);
  });
  visit(menus);
  return links;
}

export default function AdminHome() {
  return <AdminShell permission="dashboard:view" title="管理概览">{(access,user,menus)=><Dashboard access={access} user={user} menus={menus}/>}</AdminShell>;
}

function Dashboard({access,user,menus}:{access:Access;user:AdminUser;menus:MenuNode[]}) {
  const { locale, t } = useLocale();
  const router=useRouter();
  const [range,setRange]=useState<'7d'|'30d'>('7d');
  const [page,setPage]=useState(1);
  const [revision,setRevision]=useState(0);
  const [data,setData]=useState<[DashboardSummary,ActivityPage,DashboardTrends]|null>(null);
  const [loading,setLoading]=useState(true);
  const [error,setError]=useState('');
  const [working,setWorking]=useState(false);
  const [signoutError,setSignoutError]=useState('');

  useEffect(()=>{
    let alive=true;
    loadDashboard(range,page).then(result=>{if(alive){setData(result);setError('');setLoading(false);}})
      .catch((e:unknown)=>{if(!alive)return;if(e instanceof AuthError && e.status===401){router.replace('/login');return;}setError(t('无法获取仪表盘数据，请重试。'));setLoading(false);});
    return()=>{alive=false;};
  },[range,page,revision,router,t]);
  const reload=useCallback(()=>{setLoading(true);setError('');setRevision(n=>n+1);},[]);
  const selectRange=(value:'7d'|'30d')=>{setLoading(true);setRange(value);};
  const changePage=(value:number)=>{setLoading(true);setPage(value);};
  const signOut=async(all:boolean)=>{
    setWorking(true);setSignoutError('');
    try{await logout(all);router.replace('/login');}
    catch(e){setSignoutError(e instanceof Error?t(e.message):t('退出失败，请重试。'));}
    finally{setWorking(false);}
  };
  const links=quickLinks(menus,access);
  const [summary,activity,trends]=data??[];
  const max=Math.max(1,...(trends?.points??[]).map(p=>p.count));
  return <div className="space-y-6 pb-10">
    <section className="flex flex-col gap-4 border-b border-border pb-6 sm:flex-row sm:items-end sm:justify-between" aria-label="当前会话">
      <div><p className="mb-2 text-xs font-semibold tracking-wide text-muted-foreground">{t('当前会话 · 概览')}</p><h2 className="text-xl font-semibold sm:text-2xl">{t('你好，')}{user.display_name||user.username}</h2><p className="mt-1 text-sm text-muted-foreground">{t('已连接管理员账号 · 数据来自当前数据库')}</p></div>
      <Button variant="outline" onClick={reload} disabled={loading}><RefreshCw size={16} aria-hidden="true"/>{t('刷新数据')}</Button>
    </section>
    {error&&<section role="alert" className="admin-panel flex flex-wrap items-center gap-3 text-sm text-[var(--bad)]"><p>{error}</p><Button variant="outline" onClick={reload}>{t('重试')}</Button></section>}
    {loading&&<p role="status" className="admin-panel text-sm text-muted-foreground">{t('正在加载真实统计数据…')}</p>}
    {!loading&&!error&&summary&&activity&&trends&&<>
      <section aria-label="管理统计" className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
        {[
          [t('管理员'),summary.administrators,locale === 'en-US' ? `${summary.active_administrators} active` : `${summary.active_administrators} 位启用`,UsersRound],
          [t('角色'),summary.roles,locale === 'en-US' ? `${summary.active_roles} active` : `${summary.active_roles} 个启用`,ShieldCheck],
          [t('安全事件'),summary.security_events,t('已记录操作'),Activity],
          [t('登录成功'),summary.login_successes,t('累计认证成功'),ShieldCheck],
          [t('登录失败'),summary.login_failures,t('含限流拒绝'),Activity],
          [t('服务状态'),t(summary.service==='ready'?'正常':'暂不可用'),t('数据库及迁移就绪检查'),Monitor],
        ].map(([label,value,sub,Icon])=>{
          const Symbol=Icon as typeof Activity;
          return <Card key={String(label)} role="article" className="min-w-0"><CardHeader><CardTitle>{String(label)}</CardTitle><Symbol size={18} aria-hidden="true" className="text-primary"/></CardHeader><CardContent><p className="text-3xl font-semibold tabular-nums">{value as string|number}</p><p className="mt-2 text-xs text-muted-foreground">{String(sub)}</p></CardContent></Card>;
        })}
      </section>
      <div className="grid gap-6 xl:grid-cols-[minmax(0,1.5fr)_minmax(280px,1fr)]">
        <section className="admin-panel min-w-0" aria-label={t('安全事件趋势')}><div className="flex flex-wrap items-center justify-between gap-3"><div><h2 className="font-semibold">{t('安全事件趋势')}</h2><p className="mt-1 text-xs text-muted-foreground">{t('按 UTC 日期汇总，包含今天')}</p></div><div className="flex gap-1" role="group" aria-label={t('趋势时间范围')}><Button variant={range==='7d'?'default':'outline'} aria-pressed={range==='7d'} onClick={()=>selectRange('7d')}>{t('7 天')}</Button><Button variant={range==='30d'?'default':'outline'} aria-pressed={range==='30d'} onClick={()=>selectRange('30d')}>{t('30 天')}</Button></div></div>
          {trends.points.every(point=>point.count===0)?<p className="mt-6 text-sm text-muted-foreground">{t('所选时间内暂无安全事件。')}</p>:null}
          <div className="mt-7 flex h-44 items-end gap-1 overflow-x-auto border-b border-border pb-1" role="img" aria-label={locale === 'en-US' ? `Security events over ${trends.points.length} days; ${trends.points.reduce((sum,p)=>sum+p.count,0)} total` : `过去 ${trends.points.length} 天的安全事件趋势；总计 ${trends.points.reduce((sum,p)=>sum+p.count,0)} 次`}>
            {trends.points.map(point=><div key={point.date} title={locale === 'en-US' ? `${point.date} UTC: ${point.count} events` : `${point.date} UTC：${point.count} 次`} className="group relative flex min-w-1 flex-1 items-end justify-center self-stretch" aria-hidden="true"><div className="w-full max-w-8 rounded-t-sm bg-primary/75 group-hover:bg-primary" style={{height:`${Math.max(point.count?6:2,point.count/max*100)}%`}}/></div>)}
          </div><p className="mt-3 text-xs text-muted-foreground">{trends.points[0]?.date} — {trends.points.at(-1)?.date} · UTC</p>
          <details className="mt-3 text-xs text-muted-foreground"><summary className="cursor-pointer">{t('查看每日数据')}</summary><ul className="mt-2 grid max-h-40 gap-1 overflow-auto sm:grid-cols-2">{trends.points.map(p=><li key={p.date}>{p.date} UTC · {p.count} {locale === 'en-US' ? 'events' : '次'}</li>)}</ul></details>
        </section>
        <section className="admin-panel" aria-label={t('快捷导航')}><h2 className="font-semibold">{t('快捷导航')}</h2><p className="mt-1 text-xs text-muted-foreground">{t('依据当前角色和菜单授权')}</p>{links.length?<div className="mt-5 grid gap-2 sm:grid-cols-2 xl:grid-cols-1">{links.map(link=><Link key={link.id} href={link.path} className="flex min-h-11 items-center justify-between rounded-md border border-border px-3 text-sm hover:bg-muted">{t(link.name)}<ArrowUpRight size={16} aria-hidden="true"/></Link>)}</div>:<p className="mt-5 text-sm text-muted-foreground">{t('当前没有可访问的快捷入口。')}</p>}</section>
      </div>
      <section className="admin-panel min-w-0" aria-label={t('登录事件趋势')}><h2 className="font-semibold">{t('登录事件趋势')}</h2><p className="mt-1 text-xs text-muted-foreground">{locale === 'en-US' ? `Last ${trends.login_successes.length} days · UTC · success and failure (including rate limits)` : `最近 ${trends.login_successes.length} 天 · UTC · 成功与失败（含限流）`}</p>
        <div className="mt-5 grid max-h-52 gap-1 overflow-auto text-xs sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-5">{trends.login_successes.map((point,index)=><div key={point.date} className="flex justify-between gap-3 border-b border-border py-2"><time dateTime={point.date}>{point.date}</time><span><span className="text-[var(--good)]">{point.count} {t('成功')}</span> · <span className="text-[var(--bad)]">{trends.login_failures[index]?.count ?? 0} {t('失败')}</span></span></div>)}</div>
      </section>
      <section className="admin-panel" aria-label={t('最近操作')}><div className="flex flex-wrap items-end justify-between gap-3"><div><h2 className="font-semibold">{t('最近操作')}</h2><p className="mt-1 text-xs text-muted-foreground">{locale === 'en-US' ? `Security event records · ${activity.total} total` : `安全事件记录 · 共 ${activity.total} 条`}</p></div><span className="text-xs text-muted-foreground">{t('时间均为 UTC')}</span></div>
        {activity.items.length?<ul className="mt-4 divide-y divide-border">{activity.items.map(item=><li key={item.id} className="flex flex-col gap-1 py-3 text-sm sm:flex-row sm:items-center sm:justify-between sm:gap-4"><div className="min-w-0"><span className="font-medium break-all">{item.actor}</span><span className="ml-2 break-all text-muted-foreground">{item.action} · {item.target_type}</span></div><time className="shrink-0 text-xs text-muted-foreground" dateTime={item.created_at}><Clock3 size={13} aria-hidden="true" className="mr-1 inline"/>{new Date(item.created_at).toISOString().replace('T',' ').slice(0,19)} UTC</time></li>)}</ul>:<p className="mt-5 text-sm text-muted-foreground">{t('暂无操作记录。')}</p>}
        {activity.total>activity.page_size&&<div className="mt-4 flex items-center justify-end gap-3 text-sm"><Button variant="outline" disabled={page===1} onClick={()=>changePage(page-1)}>{t('上一页')}</Button><span>{locale === 'en-US' ? `Page ${page} of ${Math.ceil(activity.total/activity.page_size)}` : `第 ${page} / ${Math.ceil(activity.total/activity.page_size)} 页`}</span><Button variant="outline" disabled={page>=Math.ceil(activity.total/activity.page_size)} onClick={()=>changePage(page+1)}>{t('下一页')}</Button></div>}
      </section>
    </>}
    {signoutError&&<p role="alert" className="text-sm text-[var(--bad)]">{signoutError}</p>}
    <section className="flex flex-wrap gap-3 border-t border-border pt-5" aria-label={t('会话操作')}><Button variant="outline" disabled={working} onClick={()=>void signOut(false)}><LogOut/>{t('退出登录')}</Button><Button variant="ghost" disabled={working} onClick={()=>void signOut(true)}><Monitor/>{t('退出所有设备')}</Button></section>
  </div>;
}
