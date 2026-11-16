import { adminRequest } from '@/services/admin';

export interface DashboardSummary {
  administrators: number;
  active_administrators: number;
  roles: number;
  active_roles: number;
  security_events: number;
  login_successes: number;
  login_failures: number;
  service: 'ready' | 'unavailable';
}
export interface DashboardActivity {
  id: string;
  actor: string;
  action: string;
  target_type: string;
  created_at: string;
}
export interface ActivityPage {
  items: DashboardActivity[];
  total: number;
  page: number;
  page_size: number;
}
export interface DashboardTrend {
  date: string;
  count: number;
}
export interface DashboardTrends {
  range: '7d' | '30d';
  points: DashboardTrend[];
  login_successes: DashboardTrend[];
  login_failures: DashboardTrend[];
}

export function loadDashboard(range: '7d' | '30d', page: number) {
  return Promise.all([
    adminRequest<DashboardSummary>('dashboard/summary'),
    adminRequest<ActivityPage>(`dashboard/activity?page=${page}&page_size=10`),
    adminRequest<DashboardTrends>(`dashboard/trends?range=${range}`),
  ]);
}
