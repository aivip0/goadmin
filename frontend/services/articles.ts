import { adminRequest, type Page } from '@/services/admin';

export type Article = { id:string; category_id:string|null; author_user_id:string; title:string; slug:string; summary:string; body:string; status:'draft'|'published'; published_at:string|null; created_at:string; updated_at:string };
export type Category = { id:string; name:string; slug:string; description:string; status:'active'|'disabled'; created_at:string; updated_at:string };
export type ArticleInput = Pick<Article,'title'|'slug'|'summary'|'body'|'category_id'>;
export type CategoryInput = Pick<Category,'name'|'slug'|'description'|'status'>;
export const articleList = (page:number, q:string, status:string, categoryID:string) => adminRequest<Page<Article>>(`articles?page=${page}&page_size=20&q=${encodeURIComponent(q)}&status=${encodeURIComponent(status)}&category_id=${encodeURIComponent(categoryID)}`);
export const categoryList = (page=1) => adminRequest<Page<Category>>(`article-categories?page=${page}&page_size=50`);
