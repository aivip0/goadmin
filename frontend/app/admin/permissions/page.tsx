'use client';

import { useEffect, useState } from 'react';
import { AdminShell } from '@/components/admin-shell';
import { adminRequest, type Permission } from '@/services/admin';

export default function PermissionsPage() { return <AdminShell permission="permissions:view" title="权限目录">{() => <Catalog />}</AdminShell>; }
function Catalog() {
  const [groups, setGroups] = useState<Record<string, Permission[]> | null>(null); const [error, setError] = useState('');
  useEffect(() => { void adminRequest<Record<string, Permission[]>>('permissions/grouped').then(setGroups).catch((e: unknown) => setError(e instanceof Error ? e.message : '无法读取权限。')); }, []);
  return <div className="space-y-6">{error && <p role="alert" className="text-sm text-[var(--bad)]">{error}</p>}{!groups && !error && <p role="status">正在加载权限…</p>}{groups && Object.entries(groups).map(([module, items]) => <section key={module}><h2 className="mb-3 text-base font-semibold">{module}</h2><div className="overflow-x-auto border border-border rounded-md"><table className="admin-table"><thead><tr><th>权限代码</th><th>名称</th><th>说明</th></tr></thead><tbody>{items.map(p => <tr key={p.id}><td className="font-mono text-xs">{p.code}</td><td>{p.name}</td><td className="text-muted-foreground">{p.description}</td></tr>)}</tbody></table></div></section>)}{groups && !Object.keys(groups).length && <p className="text-sm text-muted-foreground">权限目录为空。</p>}</div>;
}
