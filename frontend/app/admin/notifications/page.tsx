'use client';

import { useCallback, useEffect, useState, type FormEvent } from 'react';
import { Bell, RefreshCw, Send } from 'lucide-react';
import { AdminShell } from '@/components/admin-shell';
import { Button } from '@/components/ui/button';
import { adminRequest, has, type Access, type Page, type User } from '@/services/admin';

interface Notice { id: string; title: string; body: string; recipient_id: string; sender_id: string; read_at: string | null; created_at: string }
interface Inbox extends Page<Notice> { unread: number }

export default function NotificationsPage() {
  return <AdminShell title="站内通知" permission="notifications:view">{access => <InboxView access={access} />}</AdminShell>;
}

function InboxView({ access }: { access: Access }) {
  const [data, setData] = useState<Inbox>({ items: [], total: 0, unread: 0, page: 1, page_size: 20 });
  const [page, setPage] = useState(1);
  const [revision, setRevision] = useState(0);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [recipient, setRecipient] = useState('');
  const [users, setUsers] = useState<User[]>([]);
  const [title, setTitle] = useState('');
  const [body, setBody] = useState('');
  const [sending, setSending] = useState(false);
  const refresh = useCallback(() => setRevision(value => value + 1), []);
  useEffect(() => { let live = true; adminRequest<Inbox>(`notifications?page=${page}&page_size=20`).then(value => { if (live) { setData(value); setError(''); } }).catch(() => { if (live) setError('通知加载失败，请重试。'); }).finally(() => { if (live) setLoading(false); }); return () => { live = false; }; }, [page, revision]);
  useEffect(() => { if (!has(access, 'notifications:create') || !has(access, 'users:view')) return; let live = true; adminRequest<Page<User>>('users?page=1&page_size=50').then(value => { if (live) setUsers(value.items); }).catch(() => {}); return () => { live = false; }; }, [access]);
  async function send(event: FormEvent) { event.preventDefault(); setSending(true); setError(''); try { await adminRequest('notifications', 'POST', { recipient_id: recipient, title, body }); setTitle(''); setBody(''); refresh(); } catch { setError('发送失败，请检查收件人、权限和内容。'); } finally { setSending(false); } }
  async function mark(id: string) { try { await adminRequest(`notifications/${id}/read`, 'POST'); refresh(); } catch { setError('标记已读失败，请重试。'); } }
  const pages = Math.max(1, Math.ceil(data.total / data.page_size));
  return <div className="space-y-5 pb-10">
    {has(access, 'notifications:create') && <form className="admin-panel space-y-4" onSubmit={send}><h2 className="font-semibold">发送通知</h2><div className="grid gap-4 sm:grid-cols-2"><label><span className="admin-label">收件人 UUID</span><input required className="auth-input" value={recipient} onChange={e => setRecipient(e.target.value)} list="notice-users" placeholder="管理员 UUID" /><datalist id="notice-users">{users.map(user => <option key={user.id} value={user.id}>{user.username}</option>)}</datalist></label><label><span className="admin-label">标题</span><input required maxLength={160} className="auth-input" value={title} onChange={e => setTitle(e.target.value)} /></label></div><label className="block"><span className="admin-label">正文</span><textarea required maxLength={4000} className="auth-input min-h-24" value={body} onChange={e => setBody(e.target.value)} /></label><Button disabled={sending || !recipient || !title.trim() || !body.trim()} type="submit"><Send size={16} aria-hidden="true" />{sending ? '发送中…' : '发送'}</Button></form>}
    {error && <div className="admin-panel flex items-center gap-3 text-sm text-[var(--bad)]" role="alert">{error}<Button variant="outline" onClick={refresh}>重试</Button></div>}
    <section className="admin-panel"><div className="flex items-center justify-between gap-3"><h2 className="font-semibold">我的通知 · 未读 {data.unread}</h2><Button variant="outline" onClick={() => { setLoading(true); refresh(); }} title="刷新通知" aria-label="刷新通知"><RefreshCw size={16} /></Button></div>
      {loading ? <p role="status" className="mt-5 text-sm text-muted-foreground">正在读取通知…</p> : data.items.length ? <ul className="mt-4 divide-y divide-border">{data.items.map(item => <li key={item.id} className="flex gap-3 py-4"><Bell className="mt-1 shrink-0 text-muted-foreground" size={18} aria-hidden="true" /><div className="min-w-0 flex-1"><div className="flex flex-wrap items-center gap-2"><strong className="text-sm">{item.title}</strong>{!item.read_at && <span className="text-xs text-primary">未读</span>}</div><p className="mt-1 whitespace-pre-wrap break-words text-sm">{item.body}</p><time className="mt-2 block text-xs text-muted-foreground">{new Date(item.created_at).toLocaleString()}</time></div>{!item.read_at && <Button variant="outline" onClick={() => void mark(item.id)}>标记已读</Button>}</li>)}</ul> : <p className="mt-5 text-sm text-muted-foreground">暂无通知。</p>}
      <div className="mt-4 flex items-center justify-end gap-3 text-sm"><Button variant="outline" disabled={page <= 1} onClick={() => { setLoading(true); setPage(page - 1); }}>上一页</Button><span>{page} / {pages}</span><Button variant="outline" disabled={page >= pages} onClick={() => { setLoading(true); setPage(page + 1); }}>下一页</Button></div>
    </section>
  </div>;
}
