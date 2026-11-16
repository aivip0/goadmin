'use client';

import { useCallback, useEffect, useState } from 'react';
import Image from 'next/image';
import { Download, Eye, FileText, Images, RotateCcw, Trash2, Upload } from 'lucide-react';
import { AdminShell } from '@/components/admin-shell';
import { Button } from '@/components/ui/button';
import { adminRequest, has, type Access, type Page } from '@/services/admin';
import { mediaBlob, uploadMedia, type MediaFile } from '@/services/media';

const imageTypes = ['image/png', 'image/jpeg', 'image/webp'];
const maxImage = 10 * 1024 * 1024;
const maxFile = 25 * 1024 * 1024;

export default function MediaPage() {
  return <AdminShell permission="media:view" title="媒体库">{access => <MediaView access={access} />}</AdminShell>;
}

function MediaView({ access }: { access: Access }) {
  const [result, setResult] = useState<Page<MediaFile> | null>(null);
  const [page, setPage] = useState(1);
  const [kind, setKind] = useState('');
  const [query, setQuery] = useState('');
  const [search, setSearch] = useState('');
  const [file, setFile] = useState<File | null>(null);
  const [progress, setProgress] = useState<number | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [notice, setNotice] = useState('');
  const [preview, setPreview] = useState<{ url: string; name: string } | null>(null);
  const load = useCallback(async () => {
    setError('');
    try {
      const params = new URLSearchParams({ page: String(page), page_size: '25' });
      if (kind) params.set('type', kind);
      if (search) params.set('q', search);
      setResult(await adminRequest<Page<MediaFile>>(`media?${params}`));
    } catch (e) { setError(e instanceof Error ? e.message : '无法读取媒体文件。'); }
  }, [page, kind, search]);
  useEffect(() => { queueMicrotask(() => { void load(); }); }, [load]);
  useEffect(() => () => { if (preview) URL.revokeObjectURL(preview.url); }, [preview]);
  const onUpload = async () => {
    if (!file || !has(access, 'media:upload')) return;
    if (file.size === 0 || file.size > maxFile || (file.type.startsWith('image/') && file.size > maxImage)) { setError('图片最多 10 MiB，其他文件最多 25 MiB，且不能是空文件。'); return; }
    setBusy(true); setError(''); setNotice(''); setProgress(0);
    try {
      await uploadMedia(file, setProgress);
      setFile(null); setPage(1); await load(); setNotice('文件已保存。');
    } catch (e) { setError(e instanceof Error ? e.message : '上传失败。'); }
    finally { setBusy(false); setProgress(null); }
  };
  const openPreview = async (item: MediaFile) => {
    setError('');
    try { const blob = await mediaBlob(item.id); setPreview({ url: URL.createObjectURL(blob), name: item.original_name }); }
    catch (e) { setError(e instanceof Error ? e.message : '无法预览。'); }
  };
  const download = async (item: MediaFile) => {
    setError('');
    try {
      // Native browser navigation saves the proxied stream without buffering 25 MiB in JavaScript.
      const anchor = document.createElement('a'); anchor.href = `/api/v1/admin/media/${item.id}/content?download=1`; anchor.download = item.original_name; anchor.click();
    } catch (e) { setError(e instanceof Error ? e.message : '无法下载。'); }
  };
  const remove = async (item: MediaFile) => {
    if (!window.confirm(`确认删除“${item.original_name}”？已关联 Logo 的图片不能删除。`)) return;
    setBusy(true); setError(''); setNotice('');
    try { await adminRequest(`media/${item.id}`, 'DELETE'); await load(); setNotice('文件已删除。'); }
    catch (e) { setError(e instanceof Error ? e.message : '删除失败。'); }
    finally { setBusy(false); }
  };
  return <div className="space-y-5">
    {has(access, 'media:upload') && <section className="admin-panel space-y-3"><h2 className="text-lg font-semibold">上传文件</h2>
      <div className="flex flex-col gap-3 sm:flex-row sm:items-end"><label className="min-w-0 flex-1"><span className="admin-label">选择文件</span><input className="auth-input" type="file" accept=".png,.jpg,.jpeg,.webp,.pdf,.txt" disabled={busy} onChange={event => setFile(event.currentTarget.files?.[0] ?? null)} /></label><Button disabled={busy || !file} onClick={() => void onUpload()}><Upload size={16} aria-hidden="true" />{busy ? '保存中…' : '上传'}</Button></div>
      <p className="text-xs text-muted-foreground">PNG、JPG、WebP 图片最多 10 MiB；PDF、TXT 附件最多 25 MiB。文件仅对有权限的管理员可见。</p>
      {progress !== null && <div role="status" className="space-y-1 text-xs"><span>发送到服务器：{progress}%{progress === 100 ? ' · 正在校验并保存' : ''}</span><progress className="block w-full" max="100" value={progress} /></div>}
    </section>}
    {error && <div role="alert" className="admin-panel text-[var(--bad)]">{error} <Button variant="outline" onClick={() => void load()}><RotateCcw size={14} />重试</Button></div>}
    {notice && <p role="status" className="admin-panel">{notice}</p>}
    <section className="space-y-4"><div className="flex flex-col gap-3 sm:flex-row sm:items-end"><label><span className="admin-label">类型</span><select className="auth-input" value={kind} onChange={event => { setKind(event.target.value); setPage(1); }}><option value="">全部</option><option value="image">图片</option><option value="document">附件</option></select></label><form className="flex flex-1 gap-2" onSubmit={event => { event.preventDefault(); setPage(1); setSearch(query.trim()); }}><label className="flex-1"><span className="admin-label">文件名</span><input className="auth-input" maxLength={80} value={query} onChange={event => setQuery(event.target.value)} /></label><Button type="submit" variant="outline" className="self-end">搜索</Button></form></div>
      {result === null && !error ? <p role="status">正在加载媒体文件…</p> : result && <><p className="text-sm text-muted-foreground">共 {result.total} 个文件</p>{result.items.length === 0 ? <div className="admin-panel py-12 text-center text-muted-foreground"><Images className="mx-auto mb-3" size={24} aria-hidden="true" />没有符合条件的文件。</div> : <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">{result.items.map(item => <article key={item.id} className="rounded-md border border-border bg-[var(--surface)] p-4"><div className="flex items-start gap-3"><span className="flex size-10 shrink-0 items-center justify-center rounded-md bg-muted">{imageTypes.includes(item.mime_type) ? <Images size={20} aria-hidden="true" /> : <FileText size={20} aria-hidden="true" />}</span><div className="min-w-0 flex-1"><h3 className="truncate text-sm font-semibold" title={item.original_name}>{item.original_name}</h3><p className="text-xs text-muted-foreground">{(item.byte_size / 1024).toFixed(1)} KiB · {new Date(item.created_at).toLocaleDateString()}</p></div></div><div className="mt-4 flex flex-wrap gap-2">{imageTypes.includes(item.mime_type) && <Button variant="outline" onClick={() => void openPreview(item)}><Eye size={14} />预览</Button>}<Button variant="outline" onClick={() => void download(item)}><Download size={14} />下载</Button>{has(access, 'media:delete') && <Button variant="ghost" disabled={busy} onClick={() => void remove(item)}><Trash2 size={14} />删除</Button>}</div></article>)}</div>}
        <div className="flex items-center justify-end gap-3"><Button variant="outline" disabled={page <= 1} onClick={() => setPage(page - 1)}>上一页</Button><span className="text-sm">{page} / {Math.max(1, Math.ceil(result.total / 25))}</span><Button variant="outline" disabled={page * 25 >= result.total} onClick={() => setPage(page + 1)}>下一页</Button></div></>}
    </section>
    {preview && <div role="dialog" aria-modal="true" aria-label={`预览 ${preview.name}`} className="fixed inset-0 z-50 flex flex-col items-center justify-center gap-4 bg-black/85 p-4" onClick={() => setPreview(null)}><Image unoptimized width={1200} height={800} src={preview.url} alt={preview.name} className="max-h-[80vh] max-w-full object-contain" onClick={event => event.stopPropagation()} /><Button variant="outline" onClick={() => setPreview(null)}>关闭预览</Button></div>}
  </div>;
}
