'use client';

import { useCallback, useEffect, useState, type FormEvent } from 'react';
import { useRouter } from 'next/navigation';
import Link from 'next/link';
import { ArrowLeft, Save, Send, Trash2 } from 'lucide-react';
import { AdminShell } from '@/components/admin-shell';
import { Button } from '@/components/ui/button';
import { adminRequest, has, type Access } from '@/services/admin';
import { categoryList, type Article, type ArticleInput, type Category } from '@/services/articles';

const blank:ArticleInput={title:'',slug:'',summary:'',body:'',category_id:null};
export function ArticleEditor({id}:{id?:string}){return <AdminShell permission={id?'articles:view':'articles:create'} title={id?'编辑文章':'新建文章'}>{access=><Editor access={access} id={id}/>}</AdminShell>}
function Editor({access,id}:{access:Access;id?:string}){
  const router=useRouter();const [form,setForm]=useState<ArticleInput>(blank),[article,setArticle]=useState<Article|null>(null),[categories,setCategories]=useState<Category[]>([]);
  const [loading,setLoading]=useState(!!id),[busy,setBusy]=useState(false),[error,setError]=useState(''),[notice,setNotice]=useState('');
  const load=useCallback(async()=>{if(!id)return;setLoading(true);setError('');try{const data=await adminRequest<Article>(`articles/${id}`);setArticle(data);setForm({title:data.title,slug:data.slug,summary:data.summary,body:data.body,category_id:data.category_id})}catch(e){setError(e instanceof Error?e.message:'文章加载失败。')}finally{setLoading(false)}},[id]);
  useEffect(()=>{queueMicrotask(()=>{void load()})},[load]);
  useEffect(()=>{if(has(access,'article_categories:view'))void categoryList().then(result=>setCategories(result.items)).catch(()=>{})},[access]);
  const act=async(fn:()=>Promise<Article|void>,message:string)=>{setBusy(true);setError('');setNotice('');try{const result=await fn();if(result){setArticle(result);setForm({title:result.title,slug:result.slug,summary:result.summary,body:result.body,category_id:result.category_id})}setNotice(message)}catch(e){setError(e instanceof Error?e.message:'操作失败。')}finally{setBusy(false)}};
  const save=(e:FormEvent)=>{e.preventDefault();void act(async()=>{const result=await adminRequest<Article>(id?`articles/${id}`:'articles',id?'PUT':'POST',form);if(!id)router.replace(`/admin/articles/${result.id}`);return result},id?'文章已保存。':'草稿已创建。')};
  const transition=(kind:'publish'|'unpublish')=>void act(()=>adminRequest<Article>(`articles/${id}/${kind}`,'POST'),kind==='publish'?'文章已发布。':'文章已恢复为草稿。');
  const remove=()=>{if(!id||!window.confirm('确认删除这篇草稿？此操作不可撤销。'))return;void act(async()=>{await adminRequest(`articles/${id}`,'DELETE');router.replace('/admin/articles')},'草稿已删除。')};
  return <div className="max-w-4xl space-y-5"><Link href="/admin/articles" className="inline-flex items-center gap-2 text-sm text-muted-foreground hover:text-foreground"><ArrowLeft size={16}/>返回文章列表</Link>
    {error&&<p role="alert" className="text-sm text-[var(--bad)]">{error}{id&&<Button variant="outline" className="ml-3" onClick={()=>void load()}>重试</Button>}</p>}{notice&&<p role="status" className="text-sm text-[var(--good)]">{notice}</p>}
    {loading?<p role="status">正在加载文章…</p>:(!id||article)&&<><div className="flex flex-wrap items-center gap-3 text-sm"><span className="font-medium">{article?.status==='published'?'已发布':'草稿'}</span>{article?.published_at&&<span className="text-muted-foreground">发布于 {new Date(article.published_at).toLocaleString()}</span>}</div>
    <form onSubmit={save} className="space-y-4"><div className="grid gap-4 sm:grid-cols-2"><label><span className="admin-label">标题</span><input className="auth-input" required maxLength={200} value={form.title} onChange={e=>setForm({...form,title:e.target.value})}/></label><label><span className="admin-label">Slug</span><input className="auth-input" required maxLength={220} pattern="[A-Za-z0-9]+(-[A-Za-z0-9]+)*" value={form.slug} onChange={e=>setForm({...form,slug:e.target.value})}/></label></div><label className="block"><span className="admin-label">摘要</span><textarea className="auth-input min-h-20" maxLength={500} value={form.summary} onChange={e=>setForm({...form,summary:e.target.value})}/></label><label className="block"><span className="admin-label">分类</span><select className="auth-input" value={form.category_id??''} onChange={e=>setForm({...form,category_id:e.target.value||null})}><option value="">未分类</option>{categories.map(c=><option key={c.id} value={c.id}>{c.name}</option>)}</select></label><label className="block"><span className="admin-label">正文</span><textarea className="auth-input min-h-64" required maxLength={100000} value={form.body} onChange={e=>setForm({...form,body:e.target.value})}/></label><Button type="submit" disabled={busy||!!id&&!has(access,'articles:update')}><Save size={16}/>{busy?'处理中…':'保存文章'}</Button></form>
    {id&&article&&<div className="flex flex-wrap gap-2 border-t border-border pt-4">{has(access,'articles:publish')&&<Button variant="outline" disabled={busy} onClick={()=>transition(article.status==='draft'?'publish':'unpublish')}><Send size={16}/>{article.status==='draft'?'发布':'取消发布'}</Button>}{has(access,'articles:delete')&&article.status==='draft'&&<Button variant="outline" disabled={busy} onClick={remove}><Trash2 size={16}/>删除草稿</Button>}</div>}</>}
  </div>
}
