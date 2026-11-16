import { backendResponse } from '@/lib/backend';
export const dynamic = 'force-dynamic';
export function GET() { return backendResponse('/api/v1/openapi.json'); }
