import type { Envelope } from '@/types/api';
import { AuthError } from '@/services/auth';

export interface MediaFile { id: string; original_name: string; mime_type: string; byte_size: number; sha256: string; width?: number; height?: number; created_at: string }

export async function csrfToken(): Promise<string> {
  const response = await fetch('/api/v1/auth/csrf', { credentials: 'same-origin', cache: 'no-store' });
  if (!response.ok) throw new AuthError(response.status, '安全校验暂时不可用。');
  const payload = await response.json() as Envelope<{ csrf_token: string }>;
  if (!payload.data?.csrf_token) throw new AuthError(503, '安全校验暂时不可用。');
  return payload.data.csrf_token;
}

export async function uploadMedia(file: File, progress: (percentage: number) => void): Promise<MediaFile> {
  const token = await csrfToken();
  return new Promise((resolve, reject) => {
    const xhr = new XMLHttpRequest();
    xhr.open('POST', '/api/v1/admin/media');
    xhr.withCredentials = true;
    xhr.timeout = 65000;
    xhr.setRequestHeader('X-CSRF-Token', token);
    xhr.upload.onprogress = event => { if (event.lengthComputable) progress(Math.round(event.loaded / event.total * 100)); };
    xhr.onerror = () => reject(new Error('上传连接失败。'));
    xhr.ontimeout = () => reject(new Error('上传超时。'));
    xhr.onload = () => {
      try {
        const body = JSON.parse(xhr.responseText) as Envelope<MediaFile>;
        if (xhr.status < 200 || xhr.status >= 300 || body.code !== 0 || !body.data) throw new AuthError(xhr.status, body.message || '上传失败。');
        resolve(body.data);
      } catch (error) { reject(error); }
    };
    const form = new FormData(); form.append('file', file); xhr.send(form);
  });
}

export async function mediaBlob(id: string, download = false): Promise<Blob> {
  const response = await fetch(`/api/v1/admin/media/${id}/content${download ? '?download=1' : ''}`, { credentials: 'same-origin', cache: 'no-store' });
  if (!response.ok) throw new AuthError(response.status, response.status === 404 ? '文件已不存在。' : '文件暂时不可用。');
  return response.blob();
}
