'use client';

import { useEffect, useMemo, useState, type ReactNode } from 'react';
import { usePathname, useRouter } from 'next/navigation';
import Link from 'next/link';
import { Activity, ChevronRight, Languages, Menu, Moon, Sun, UserRound, X } from 'lucide-react';
import { useTheme } from 'next-themes';
import { Button } from '@/components/ui/button';
import { AuthError, currentUser, type AdminUser } from '@/services/auth';
import { AdminNav } from '@/components/admin-nav';
import { adminRequest, has, type Access, type MenuNode } from '@/services/admin';
import { useLocale, type Locale } from '@/components/locale-provider';

function findMenuName(menus: MenuNode[], path: string): string | undefined {
  for (const menu of menus) {
    if (menu.path && (menu.path === path || (menu.path !== '/admin' && path.startsWith(`${menu.path}/`)))) return menu.name;
    const nested = findMenuName(menu.children ?? [], path);
    if (nested) return nested;
  }
  return undefined;
}

function ThemeButton() {
  const { resolvedTheme, setTheme } = useTheme();
  const dark = resolvedTheme === 'dark';
  return <Button variant="ghost" size="icon" aria-label="Toggle color theme" title="Toggle color theme" onClick={() => setTheme(dark ? 'light' : 'dark')}>
    <Sun className="hidden dark:block" aria-hidden="true" /><Moon className="block dark:hidden" aria-hidden="true" />
  </Button>;
}

function LanguageButton() {
  const { locale, setLocale } = useLocale();
  const next: Locale = locale === 'zh-CN' ? 'en-US' : 'zh-CN';
  return <Button variant="ghost" size="icon" aria-label={locale === 'zh-CN' ? 'Switch language to English' : '切换语言为简体中文'} title={next === 'en-US' ? 'English' : '简体中文'} onClick={() => setLocale(next)}>
    <Languages aria-hidden="true" />
  </Button>;
}

export function AdminShell({ permission, title, children }: { permission?: string; title: string; children: (access: Access, user: AdminUser, menus: MenuNode[]) => ReactNode }) {
  const router = useRouter();
  const pathname = usePathname();
  const { t } = useLocale();
  const [user, setUser] = useState<AdminUser | null>(null);
  const [access, setAccess] = useState<Access | null>(null);
  const [menus, setMenus] = useState<MenuNode[] | null>(null);
  const [error, setError] = useState('');
  const [mobileOpen, setMobileOpen] = useState(false);
  const activeName = useMemo(() => menus ? findMenuName(menus, pathname) : undefined, [menus, pathname]);

  useEffect(() => {
    let alive = true;
    currentUser().then(async current => {
      const effective = await adminRequest<Access>('permissions/effective');
      const navigation = await adminRequest<MenuNode[]>('menus/my');
      if (alive) { setUser(current); setAccess(effective); setMenus(navigation); }
    }).catch((e: unknown) => {
      if (!alive) return;
      if (e instanceof AuthError && e.status === 401) router.replace('/login');
      else setError('暂时无法加载管理权限，请刷新重试。');
    });
    return () => { alive = false; };
  }, [router]);

  const navigation = error
    ? <div role="alert" className="space-y-2 px-3 text-sm text-[var(--bad)]"><p>{t('暂时无法加载管理权限，请刷新重试。')}</p><Button variant="outline" onClick={() => location.reload()}>{t('重试')}</Button></div>
    : menus === null
      ? <p role="status" className="px-3 text-sm text-muted-foreground">{t('正在加载菜单…')}</p>
      : menus.length
        ? <AdminNav menus={menus} onNavigate={() => setMobileOpen(false)} />
        : <p className="px-3 text-sm text-muted-foreground">{t('当前账号没有已分配的菜单。')}</p>;

  return <div className="min-h-screen">
    <aside aria-label={t('侧边导航')} className="fixed inset-y-0 left-0 z-40 hidden w-[258px] flex-col border-r border-border bg-[var(--sidebar)] lg:flex">
      <Link href="/admin" className="flex h-[68px] shrink-0 items-center gap-3 border-b border-border px-5 font-semibold tracking-tight">
        <span className="flex size-9 items-center justify-center rounded-md bg-primary text-primary-foreground"><Activity size={19} aria-hidden="true" /></span>
        <span>GoAdmin <span className="font-normal text-muted-foreground">Kit</span></span>
      </Link>
      <div className="overflow-y-auto px-3 py-5">
        <div className="mb-2 px-3 text-[11px] font-semibold uppercase tracking-[.08em] text-muted-foreground">{t('管理模块')}</div>
        {navigation}
      </div>
      <div className="mt-auto border-t border-border p-3">
        <Link href="/admin/profile" className="flex min-w-0 items-center gap-3 rounded-md p-2.5 hover:bg-[var(--sidebar-muted)]">
          <span className="flex size-9 shrink-0 items-center justify-center rounded-full bg-muted text-foreground"><UserRound size={17} aria-hidden="true" /></span>
          <span className="min-w-0"><span className="block truncate text-sm font-medium">{user?.display_name || user?.username || 'GoAdmin'}</span><span className="block text-xs text-muted-foreground">{t('个人资料')}</span></span>
        </Link>
      </div>
    </aside>

    {mobileOpen && <div className="fixed inset-0 z-50 lg:hidden" role="presentation">
      <button className="absolute inset-0 bg-black/45" aria-label={t('关闭')} onClick={() => setMobileOpen(false)} />
      <aside aria-label={t('侧边导航')} className="absolute inset-y-0 left-0 flex w-[min(84vw,300px)] flex-col border-r border-border bg-[var(--sidebar)] shadow-xl">
        <div className="flex h-[68px] shrink-0 items-center justify-between border-b border-border px-5">
          <Link href="/admin" className="inline-flex items-center gap-3 font-semibold"><span className="flex size-9 items-center justify-center rounded-md bg-primary text-primary-foreground"><Activity size={19} aria-hidden="true" /></span>GoAdmin Kit</Link>
          <Button variant="ghost" size="icon" aria-label={t('关闭')} onClick={() => setMobileOpen(false)}><X aria-hidden="true" /></Button>
        </div>
        <div className="overflow-y-auto px-3 py-5"><div className="mb-2 px-3 text-[11px] font-semibold uppercase tracking-[.08em] text-muted-foreground">{t('管理模块')}</div>{navigation}</div>
      </aside>
    </div>}

    <div className="min-h-screen lg:pl-[258px]">
      <header className="sticky top-0 z-30 flex h-[68px] items-center justify-between gap-3 border-b border-border bg-[color-mix(in_srgb,var(--surface)_94%,transparent)] px-4 backdrop-blur-md sm:px-6 lg:px-8">
        <div className="flex min-w-0 items-center gap-3">
          <Button className="lg:hidden" variant="ghost" size="icon" aria-label={t('打开导航')} onClick={() => setMobileOpen(true)}><Menu aria-hidden="true" /></Button>
          <nav aria-label="Breadcrumb" className="flex min-w-0 items-center gap-2 text-sm">
            <Link href="/admin" className="hidden text-muted-foreground hover:text-foreground sm:inline">{t('工作区')}</Link>
            <ChevronRight className="hidden size-4 text-muted-foreground sm:block" aria-hidden="true" />
            <span className="truncate font-medium">{t(activeName ?? title)}</span>
          </nav>
        </div>
        <div className="flex shrink-0 items-center gap-1">
          <LanguageButton />
          <ThemeButton />
          <div className="mx-1 hidden h-6 w-px bg-border sm:block" />
          <Link href="/admin/profile" className="flex min-w-0 items-center gap-2 rounded-md px-2 py-1.5 hover:bg-muted" aria-label={t('个人资料')} title={t('个人资料')}>
            <span className="hidden max-w-36 truncate text-sm font-medium sm:inline">{user?.display_name || user?.username || ''}</span>
            <span className="flex size-8 items-center justify-center rounded-full bg-muted"><UserRound size={16} aria-hidden="true" /></span>
          </Link>
        </div>
      </header>
      <main id="main-content" tabIndex={-1} className="admin-shell-scroll mx-auto w-full max-w-[1600px] px-4 py-6 sm:px-6 lg:px-8 lg:py-8">
        <div className="admin-page-content">
          <div className="mb-6 border-b border-border pb-5 sm:mb-7">
            <p className="mb-1 text-xs font-semibold uppercase tracking-[.08em] text-muted-foreground">{t('GoAdmin Kit 管理控制台')}</p>
            <h1 className="text-[22px] font-semibold leading-tight sm:text-[26px]">{t(title)}</h1>
          </div>
          {error ? <div role="alert" className="text-sm text-[var(--bad)]">{t('无法加载菜单或权限，请重试。')}</div>
            : !user || !access || !menus ? <p role="status" className="text-sm text-muted-foreground">{t('正在验证登录状态…')}</p>
              : permission && !has(access, permission)
                ? <section role="alert" className="admin-panel max-w-xl"><h2 className="font-semibold">{t('没有访问此页面的权限。')}</h2><p className="mt-2 text-sm text-muted-foreground">{t('请使用已授权的导航项目，或联系管理员调整角色权限。')}</p></section>
                : children(access, user, menus)}
        </div>
      </main>
    </div>
  </div>;
}
