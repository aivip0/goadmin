package dashboard

import (
	"context"
	"database/sql"
	"time"
)

type Repository struct{ db *sql.DB }

func NewRepository(db *sql.DB) *Repository { return &Repository{db: db} }

func (r *Repository) Summary(ctx context.Context) (Summary, error) {
	var v Summary
	err := r.db.QueryRowContext(ctx, `SELECT
	 (SELECT count(*) FROM admin_users),
	 (SELECT count(*) FROM admin_users WHERE status='active'),
	 (SELECT count(*) FROM roles),
	 (SELECT count(*) FROM roles WHERE status='active'),
	 (SELECT count(*) FROM security_events),
	 (SELECT count(*) FROM login_events WHERE outcome='success'),
	 (SELECT count(*) FROM login_events WHERE outcome IN ('failure','rate_limited'))`).Scan(&v.Administrators, &v.ActiveAdministrators, &v.Roles, &v.ActiveRoles, &v.SecurityEvents, &v.LoginSuccesses, &v.LoginFailures)
	return v, err
}

func (r *Repository) Activity(ctx context.Context, page, size int) (ActivityPage, error) {
	v := ActivityPage{Items: []Activity{}, Page: page, PageSize: size}
	if err := r.db.QueryRowContext(ctx, `SELECT count(*) FROM security_events`).Scan(&v.Total); err != nil {
		return v, err
	}
	rows, err := r.db.QueryContext(ctx, `SELECT e.id::text,coalesce(u.username,'former administrator'),e.action,e.target_type,e.created_at
	 FROM security_events e LEFT JOIN admin_users u ON u.id=e.actor_user_id
	 ORDER BY e.created_at DESC,e.id DESC LIMIT $1 OFFSET $2`, size, (page-1)*size)
	if err != nil {
		return v, err
	}
	defer rows.Close()
	for rows.Next() {
		var item Activity
		if err = rows.Scan(&item.ID, &item.Actor, &item.Action, &item.TargetType, &item.CreatedAt); err != nil {
			return v, err
		}
		v.Items = append(v.Items, item)
	}
	return v, rows.Err()
}

func (r *Repository) Trends(ctx context.Context, start, end time.Time) (map[string]int64, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT to_char(created_at AT TIME ZONE 'UTC','YYYY-MM-DD'),count(*)
	 FROM security_events WHERE created_at >= $1 AND created_at < $2
	 GROUP BY 1 ORDER BY 1`, start, end)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	counts := make(map[string]int64)
	for rows.Next() {
		var day string
		var count int64
		if err = rows.Scan(&day, &count); err != nil {
			return nil, err
		}
		counts[day] = count
	}
	return counts, rows.Err()
}

func (r *Repository) LoginTrends(ctx context.Context, start, end time.Time) (map[string]int64, map[string]int64, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT to_char(created_at AT TIME ZONE 'UTC','YYYY-MM-DD'),
	 count(*) FILTER (WHERE outcome='success'),count(*) FILTER (WHERE outcome IN ('failure','rate_limited'))
	 FROM login_events WHERE created_at >= $1 AND created_at < $2 GROUP BY 1 ORDER BY 1`, start, end)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	success, failure := make(map[string]int64), make(map[string]int64)
	for rows.Next() {
		var day string
		var yes, no int64
		if err = rows.Scan(&day, &yes, &no); err != nil {
			return nil, nil, err
		}
		success[day], failure[day] = yes, no
	}
	return success, failure, rows.Err()
}
