'use client';

import { useCallback, useEffect, useState, type FormEvent } from 'react';
import Link from 'next/link';
import { Plus, Search } from 'lucide-react';
import { AdminShell } from '@/components/admin-shell';
import { Button } from '@/components/ui/button';
import { has, type Access, type Page } from '@/services/admin';
import { articleList, categoryList, type Article, type Category } from '@/services/articles';

export default function ArticlesPage(){return <AdminShell title="文章" permission="articles:view">{access=><ArticlesView access={access}/>}</AdminShell>}
function ArticlesView({access}:{access:Access}){
  const [page,setPage]=useState(1),[draft,setDraft]=useState(''),[query,setQuery]=useState(''),[status,setStatus]=useState(''),[categoryID,setCategoryID]=useState('');
  const [items,setItems]=useState<Page<Article>|null>(null),[categories,setCategories]=useState<Category[]>([]),[error,setError]=useState('');
  const refresh=useCallback(async()=>{setError('');try{const result=await articleList(page,query,status,categoryID);setItems(result)}catch(e){setError(e instanceof Error?e.message:'文章加载失败。')}},[page,query,status,categoryID]);
  useEffect(()=>{queueMicrotask(()=>{void refresh()})},[refresh]);
  useEffect(()=>{if(has(access,'article_categories:view')){void categoryList().then(result=>setCategories(result.items)).catch(()=>{})}},[access]);
  return <div className="space-y-5"><div className="flex flex-wrap items-center justify-between gap-3"><p className="text-sm text-muted-foreground">文章与草稿</p><div className="flex gap-2">{has(access,'article_categories:view')&&<Link href="/admin/articles/categories"><Button variant="outline">分类</Button></Link>}{has(access,'articles:create')&&<Link href="/admin/articles/new"><Button><Plus size={16}/>新建文章</Button></Link>}</div></div>
    <form className="grid gap-3 sm:grid-cols-[minmax(0,1fr)_160px_180px_auto] sm:items-end" onSubmit={(e:FormEvent)=>{e.preventDefault();setPage(1);setQuery(draft.trim())}}><label><span className="admin-label">搜索标题或 Slug</span><input className="auth-input" maxLength={100} value={draft} onChange={e=>setDraft(e.target.value)}/></label><label><span className="admin-label">状态</span><select className="auth-input" value={status} onChange={e=>{setPage(1);setStatus(e.target.value)}}><option value="">全部</option><option value="draft">草稿</option><option value="published">已发布</option></select></label><label><span className="admin-label">分类</span><select className="auth-input" value={categoryID} onChange={e=>{setPage(1);setCategoryID(e.target.value)}}><option value="">全部</option>{categories.map(c=><option key={c.id} value={c.id}>{c.name}</option>)}</select></label><Button type="submit" variant="outline"><Search size={16}/>搜索</Button></form>
    {error&&<div role="alert" className="admin-panel text-sm text-[var(--bad)]">{error}<Button className="ml-3" variant="outline" onClick={()=>void refresh()}>重试</Button></div>}
    {!items&&!error?<p role="status" className="text-sm text-muted-foreground">正在加载文章…</p>:items&&<><p className="text-sm text-muted-foreground">共 {items.total} 篇文章</p><div className="overflow-x-auto rounded-md border border-border"><table className="admin-table"><thead><tr><th>标题</th><th>Slug</th><th>分类</th><th>状态</th><th>更新时间</th><th>操作</th></tr></thead><tbody>{items.items.map(item=><tr key={item.id}><td className="font-medium">{item.title}</td><td className="text-muted-foreground">{item.slug}</td><td>{categories.find(c=>c.id===item.category_id)?.name??'未分类'}</td><td>{item.status==='published'?'已发布':'草稿'}</td><td>{new Date(item.updated_at).toLocaleString()}</td><td><Link href={`/admin/articles/${item.id}`} className="text-primary underline-offset-2 hover:underline">查看/编辑</Link></td></tr>)}</tbody></table>{items.items.length===0&&<p className="p-6 text-center text-sm text-muted-foreground">没有符合条件的文章。</p>}</div><div className="flex items-center justify-between gap-3"><Button variant="outline" disabled={page<=1} onClick={()=>setPage(page-1)}>上一页</Button><span className="text-sm">第 {page} 页</span><Button variant="outline" disabled={page*20>=items.total} onClick={()=>setPage(page+1)}>下一页</Button></div></>}
  </div>
}
