'use client';
import { useParams } from 'next/navigation';
import { ArticleEditor } from '@/components/article-editor';
export default function ArticleDetailPage(){const params=useParams<{id:string}>();return <ArticleEditor id={params.id}/>}
