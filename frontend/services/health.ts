import type { Envelope, Health } from '@/types/api';
export async function getHealth(signal?: AbortSignal): Promise<Health> {
  const response = await fetch('/api/v1/health', { cache: 'no-store', signal });
  const body: Envelope<Health> = await response.json();
  if (!response.ok || body.code !== 0 || body.data?.status !== 'ok' || body.data.database !== 'connected' || body.data.migration !== 'current' || !Number.isFinite(Date.parse(body.data.checked_at))) {
    throw new Error('Service not ready');
  }
  return body.data;
}
