import { backendResponse } from '@/lib/backend';
export const dynamic = 'force-dynamic';
export function GET(request: Request) { return backendResponse('/api/v1/health', request); }
