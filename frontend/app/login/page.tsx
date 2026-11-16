'use client';

import { useEffect, useState } from 'react';
import { useRouter } from 'next/navigation';
import Link from 'next/link';
import { useForm } from 'react-hook-form';
import { zodResolver } from '@hookform/resolvers/zod';
import { z } from 'zod';
import { ArrowRight, Eye, EyeOff, LockKeyhole, LoaderCircle } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { currentUser, login } from '@/services/auth';
import { useLocale } from '@/components/locale-provider';

const schema = z.object({ username: z.string().trim().min(3, '请输入用户名').max(64), password: z.string().min(1, '请输入密码') });
type Form = z.infer<typeof schema>;

export default function Login() {
  const { locale, setLocale, t } = useLocale();
  const router = useRouter();
  const [checking, setChecking] = useState(true);
  const [visible, setVisible] = useState(false);
  const [failure, setFailure] = useState('');
  const { register, handleSubmit, formState: { errors, isSubmitting } } = useForm<Form>({ resolver: zodResolver(schema) });
  useEffect(() => { let active = true; void currentUser().then(() => { if (active) router.replace('/admin'); }).catch(() => { if (active) setChecking(false); }); return () => { active = false; }; }, [router]);

  const submit = handleSubmit(async (values) => {
    setFailure('');
    try { await login(values.username, values.password); router.replace('/admin'); }
    catch (error) { setFailure(error instanceof Error ? error.message : '登录失败，请稍后重试。'); }
  });

  return <main className="flex min-h-screen flex-col bg-background">
    <header className="border-b border-border bg-[var(--surface)]"><div className="mx-auto flex min-h-18 max-w-5xl items-center justify-between px-5 sm:px-8"><Link href="/" className="inline-flex items-center gap-3 text-lg font-semibold"><span className="flex size-9 items-center justify-center rounded-md bg-primary text-primary-foreground"><LockKeyhole size={20} aria-hidden="true" /></span>GoAdmin Kit</Link><Button variant="ghost" onClick={() => setLocale(locale === 'zh-CN' ? 'en-US' : 'zh-CN')}>{locale === 'zh-CN' ? 'English' : '简体中文'}</Button></div></header>
    <div className="mx-auto flex w-full max-w-5xl flex-1 items-center justify-center px-5 py-12 sm:px-8">
      <div className="admin-panel w-full max-w-[440px] px-6 py-8 sm:px-8">
        <p className="mb-3 text-xs font-semibold uppercase text-muted-foreground">{t('管理员访问')}</p>
        <h1 className="text-3xl font-semibold">{t('登录管理后台')}</h1>
        <p className="mt-3 text-sm text-muted-foreground">{t('使用已初始化的管理员账号继续。')}</p>
        {checking ? <div role="status" className="mt-10 flex items-center gap-3 text-sm text-muted-foreground"><LoaderCircle className="size-4 animate-spin" aria-hidden="true" />{t('正在检查登录状态')}</div> :
          <form className="mt-9 space-y-5" onSubmit={(event) => void submit(event)} noValidate>
            <div><label htmlFor="username" className="mb-2 block text-sm font-medium">{t('用户名')}</label><input id="username" autoComplete="username" autoCapitalize="none" spellCheck={false} aria-invalid={!!errors.username} aria-describedby={errors.username ? 'username-error' : undefined} className="auth-input" {...register('username')} />{errors.username && <p id="username-error" role="alert" className="mt-1 text-sm text-[var(--bad)]">{t(errors.username.message ?? '')}</p>}</div>
            <div><label htmlFor="password" className="mb-2 block text-sm font-medium">{t('密码')}</label><div className="relative"><input id="password" type={visible ? 'text' : 'password'} autoComplete="current-password" aria-invalid={!!errors.password} aria-describedby={errors.password ? 'password-error' : undefined} className="auth-input pr-12" {...register('password')} /><button type="button" className="absolute right-1 top-1 flex size-9 items-center justify-center rounded-md text-muted-foreground hover:text-foreground focus-visible:outline-2 focus-visible:outline-ring" onClick={() => setVisible(!visible)} aria-label={t(visible ? '隐藏密码' : '显示密码')} title={t(visible ? '隐藏密码' : '显示密码')}>{visible ? <EyeOff size={19} /> : <Eye size={19} />}</button></div>{errors.password && <p id="password-error" role="alert" className="mt-1 text-sm text-[var(--bad)]">{t(errors.password.message ?? '')}</p>}</div>
            {failure && <p role="alert" className="border-l-2 border-[var(--bad)] py-1 pl-3 text-sm text-[var(--bad)]">{failure}</p>}
            <Button type="submit" disabled={isSubmitting} className="h-11 w-full">{isSubmitting ? <><LoaderCircle className="animate-spin" />{t('正在登录')}</> : <>{t('登录')} <ArrowRight /></>}</Button>
          </form>}
      </div>
    </div>
  </main>;
}
