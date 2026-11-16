'use client';

import { useCallback, useEffect, useState, type FormEvent } from 'react';
import { Eye, EyeOff, RefreshCw, ShieldCheck, X } from 'lucide-react';
import { useRouter } from 'next/navigation';
import { AdminShell } from '@/components/admin-shell';
import { Button } from '@/components/ui/button';
import { AuthError, type AdminUser } from '@/services/auth';
import { adminRequest } from '@/services/admin';

interface Session { id: string; current: boolean; created_at: string; last_seen_at: string; expires_at: string }
interface SessionPage { items: Session[]; total: number; page: number; page_size: number }
interface ProfileResponse { user: AdminUser }

export default function ProfilePage() {
  return <AdminShell title="个人资料">{(_access, user) => <ProfileView user={user} />}</AdminShell>;
}

function ProfileView({ user }: { user: AdminUser }) {
  const router = useRouter();
  const [profile, setProfile] = useState({ email: user.email, display_name: user.display_name });
  const [sessions, setSessions] = useState<SessionPage | null>(null);
  const [passwords, setPasswords] = useState({ current_password: '', new_password: '', confirmation: '' });
  const [showPasswords, setShowPasswords] = useState(false);
  const [busy, setBusy] = useState('');
  const [error, setError] = useState('');
  const [notice, setNotice] = useState('');

  const failure = useCallback((reason: unknown) => {
    if (reason instanceof AuthError && reason.status === 401) { setSessions(null); router.replace('/login'); return; }
    setError(reason instanceof Error ? reason.message : '请求失败，请重试。');
  }, [router]);
  const loadSessions = useCallback(async (page = 1) => {
    setError('');
    try { setSessions(await adminRequest<SessionPage>(`profile/sessions?page=${page}&page_size=20`)); }
    catch (reason) { failure(reason); }
  }, [failure]);
  useEffect(() => { queueMicrotask(() => { void loadSessions(); }); }, [loadSessions]);

  const saveProfile = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault(); setBusy('profile'); setError(''); setNotice('');
    try {
      const result = await adminRequest<ProfileResponse>('profile', 'PUT', profile);
      setProfile({ email: result.user.email, display_name: result.user.display_name });
      setNotice('个人资料已更新。');
    } catch (reason) { failure(reason); }
    finally { setBusy(''); }
  };
  const changePassword = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault(); setError(''); setNotice('');
    if (passwords.new_password !== passwords.confirmation) { setError('两次输入的新密码不一致。'); return; }
    setBusy('password');
    try {
      await adminRequest<null>('profile/password', 'PUT', { current_password: passwords.current_password, new_password: passwords.new_password });
      setPasswords({ current_password: '', new_password: '', confirmation: '' });
      router.replace('/login?reason=password-changed');
    } catch (reason) { failure(reason); }
    finally { setBusy(''); }
  };
  const revoke = async (session: Session) => {
    if (!window.confirm(session.current ? '撤销当前会话后将立即退出，继续吗？' : '撤销此会话？')) return;
    setBusy(session.id); setError(''); setNotice('');
    try {
      const result = await adminRequest<{ current_revoked: boolean }>(`profile/sessions/${session.id}`, 'DELETE');
      if (result.current_revoked) { router.replace('/login'); return; }
      setNotice('会话已撤销。'); await loadSessions(sessions?.page ?? 1);
    } catch (reason) { failure(reason); }
    finally { setBusy(''); }
  };
  const revokeOthers = async () => {
    if (!window.confirm('撤销此账号的其他所有有效会话？当前会话会保留。')) return;
    setBusy('others'); setError(''); setNotice('');
    try { await adminRequest<null>('profile/sessions/revoke-others', 'POST'); setNotice('其他会话已撤销。'); await loadSessions(sessions?.page ?? 1); }
    catch (reason) { failure(reason); }
    finally { setBusy(''); }
  };

  return <div className="space-y-6">
    <p className="text-sm text-muted-foreground">管理当前账号资料、密码和已登录会话。会话只显示时间信息，不会显示认证凭据或设备指纹。</p>
    {error && <div role="alert" className="admin-panel border-[var(--bad)] text-sm text-[var(--bad)]">{error}</div>}
    {notice && <p role="status" className="admin-panel text-sm">{notice}</p>}
    <form onSubmit={event => void saveProfile(event)} className="admin-panel max-w-3xl space-y-4">
      <h2 className="text-lg font-semibold">基本资料</h2>
      <label className="block"><span className="admin-label">用户名</span><input className="auth-input" value={user.username} readOnly aria-readonly="true" /></label>
      <label className="block"><span className="admin-label">显示名称</span><input className="auth-input" required maxLength={128} autoComplete="nickname" value={profile.display_name} disabled={busy !== ''} onChange={event => setProfile({ ...profile, display_name: event.target.value })} /></label>
      <label className="block"><span className="admin-label">邮箱</span><input className="auth-input" type="email" required maxLength={254} autoComplete="email" value={profile.email} disabled={busy !== ''} onChange={event => setProfile({ ...profile, email: event.target.value })} /></label>
      <Button type="submit" disabled={busy !== ''}>保存资料</Button>
    </form>
    <form onSubmit={event => void changePassword(event)} className="admin-panel max-w-3xl space-y-4">
      <div className="flex items-center gap-2"><ShieldCheck size={19} aria-hidden="true" /><h2 className="text-lg font-semibold">修改密码</h2></div>
      <p className="text-sm text-muted-foreground">新密码至少 15 个字符。成功后所有会话都会撤销，请使用新密码重新登录。</p>
      {([['current_password', '当前密码', 'current-password'], ['new_password', '新密码', 'new-password'], ['confirmation', '确认新密码', 'new-password']] as const).map(([key, label, autocomplete]) => <div key={key}><label htmlFor={`profile-${key}`} className="admin-label">{label}</label><span className="relative block"><input id={`profile-${key}`} className="auth-input pr-12" type={showPasswords ? 'text' : 'password'} required minLength={key === 'current_password' ? 1 : 15} maxLength={128} autoComplete={autocomplete} value={passwords[key]} disabled={busy !== ''} onChange={event => setPasswords({ ...passwords, [key]: event.target.value })} /><button type="button" className="absolute inset-y-0 right-0 grid w-11 place-items-center rounded-r-md text-muted-foreground hover:text-foreground" aria-label={showPasswords ? '隐藏密码' : '显示密码'} title={showPasswords ? '隐藏密码' : '显示密码'} onClick={() => setShowPasswords(!showPasswords)}>{showPasswords ? <EyeOff size={17} /> : <Eye size={17} />}</button></span></div>)}
      <Button type="submit" disabled={busy !== ''}>{busy === 'password' ? '正在更新…' : '更新密码'}</Button>
    </form>
    <section className="space-y-4" aria-labelledby="profile-sessions-title">
      <div className="flex flex-wrap items-center justify-between gap-3"><div><h2 id="profile-sessions-title" className="text-lg font-semibold">有效会话</h2><p className="text-sm text-muted-foreground">当前及近期活动且未过期的登录会话。</p></div><div className="flex gap-2"><Button variant="outline" aria-label="刷新会话" title="刷新会话" onClick={() => void loadSessions(sessions?.page ?? 1)} disabled={busy !== ''}><RefreshCw size={16} aria-hidden="true" /></Button><Button variant="outline" onClick={() => void revokeOthers()} disabled={busy !== ''}>撤销其他会话</Button></div></div>
      {!sessions ? <div className="admin-panel" role={error ? 'alert' : 'status'}>{error || '正在加载会话…'}{error && <Button className="ml-3" variant="outline" onClick={() => void loadSessions()}>重试</Button>}</div> : sessions.items.length === 0 ? <div className="admin-panel text-sm text-muted-foreground">没有其他有效会话。</div> : <>
        <ul className="divide-y divide-border rounded-md border border-border">{sessions.items.map(session => <li key={session.id} className="flex flex-col gap-3 p-4 sm:flex-row sm:items-center sm:justify-between"><div className="grid min-w-0 flex-1 gap-2 text-sm sm:grid-cols-3"><div><span className="admin-label">创建时间</span><time dateTime={session.created_at}>{new Date(session.created_at).toLocaleString()}</time>{session.current && <span className="ml-2 inline-flex items-center rounded-sm bg-muted px-2 py-0.5 text-xs">当前会话</span>}</div><div><span className="admin-label">最后活动</span><time dateTime={session.last_seen_at}>{new Date(session.last_seen_at).toLocaleString()}</time></div><div><span className="admin-label">绝对过期</span><time dateTime={session.expires_at}>{new Date(session.expires_at).toLocaleString()}</time></div></div><Button variant="outline" disabled={busy !== ''} onClick={() => void revoke(session)}><X size={16} aria-hidden="true" />撤销</Button></li>)}</ul>
        <div className="flex items-center justify-between text-sm"><span>共 {sessions.total} 个会话</span><div className="flex gap-2"><Button variant="outline" disabled={sessions.page <= 1 || busy !== ''} onClick={() => void loadSessions(sessions.page - 1)}>上一页</Button><span className="self-center">第 {sessions.page} 页</span><Button variant="outline" disabled={sessions.page * sessions.page_size >= sessions.total || busy !== ''} onClick={() => void loadSessions(sessions.page + 1)}>下一页</Button></div></div>
      </>}
    </section>
  </div>;
}
