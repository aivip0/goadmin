import { adminRequest, type Page } from '@/services/admin';

export interface LogEntry {
  id: string;
  user_id?: string | null;
  actor: string;
  action?: string;
  target_type?: string;
  target_id?: string;
  account_kind?: string;
  outcome: string;
  request_method?: string;
  request_path?: string;
  client_ip: string;
  user_agent: string;
  created_at: string;
}

export type LogKind = 'audit-logs' | 'login-logs';

export function loadLogs(kind: LogKind, params: URLSearchParams): Promise<Page<LogEntry>> {
  return adminRequest<Page<LogEntry>>(`${kind}?${params.toString()}`);
}

export function loadLogDetail(kind: LogKind, id: string): Promise<LogEntry> {
  return adminRequest<LogEntry>(`${kind}/${id}`);
}
