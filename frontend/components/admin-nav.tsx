'use client';

import Link from 'next/link';
import { usePathname } from 'next/navigation';
import { useState } from 'react';
import { Activity, Bell, ChevronDown, ExternalLink, FileText, Folder, Images, KeyRound, LayoutDashboard, ListTree, LogIn, Puzzle, ScrollText, Settings, Shield, Users, type LucideIcon } from 'lucide-react';
import type { MenuNode } from '@/services/admin';
import { useLocale } from '@/components/locale-provider';

const icons: Record<string,LucideIcon> = { LayoutDashboard, Users, Shield, KeyRound, ListTree, ExternalLink, Folder, FileText, Settings, Images, ScrollText, LogIn, Bell, Puzzle };

export function AdminNav({ menus, onNavigate }: { menus: MenuNode[]; onNavigate?: () => void }) {
  const pathname=usePathname();
  const { t } = useLocale();
  return <nav aria-label={t('管理导航')} className="space-y-1">{menus.map(menu=><NavNode key={menu.id} menu={menu} pathname={pathname} depth={0} onNavigate={onNavigate}/>)}</nav>;
}

function NavNode({menu,pathname,depth,onNavigate}:{menu:MenuNode;pathname:string;depth:number;onNavigate?:()=>void}){
  const [open,setOpen]=useState(true);
  const { t } = useLocale();
  const Icon=icons[menu.icon??'']??Activity;
  const active=!!menu.path&&(pathname===menu.path||pathname.startsWith(`${menu.path}/`));
  if(menu.menu_type==='directory') return <section className="space-y-1">
    <button type="button" aria-expanded={open} onClick={()=>setOpen(!open)} className="admin-nav w-full justify-between text-left" style={{paddingLeft:12+depth*14}}><span className="inline-flex items-center gap-2"><Icon size={16} aria-hidden="true"/>{t(menu.name)}</span><ChevronDown size={15} className={open?'':'-rotate-90'} aria-hidden="true"/></button>
    {open&&<div className="space-y-1">{menu.children.map(child=><NavNode key={child.id} menu={child} pathname={pathname} depth={depth+1} onNavigate={onNavigate}/>)}</div>}
  </section>;
  const className=`admin-nav w-full ${active?'bg-muted font-medium text-foreground':''}`;
  return menu.is_external?<a className={className} style={{paddingLeft:12+depth*14}} href={menu.path} target="_blank" rel="noopener noreferrer" onClick={onNavigate}><Icon size={16} aria-hidden="true"/>{t(menu.name)}<ExternalLink className="ml-auto" size={14} aria-hidden="true"/></a>:<Link className={className} style={{paddingLeft:12+depth*14}} href={menu.path??'/admin'} aria-current={active?'page':undefined} onClick={onNavigate}><Icon size={16} aria-hidden="true"/>{t(menu.name)}</Link>;
}
