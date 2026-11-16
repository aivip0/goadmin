package logs

import (
	"context"
	"database/sql"
	"errors"
)

type Repository struct{ db *sql.DB }

func NewRepository(db *sql.DB) *Repository { return &Repository{db: db} }

func (r *Repository) List(ctx context.Context, kind string, f Filter) (Page, error) {
	result := Page{Items: []Entry{}, Page: f.Page, PageSize: f.PageSize}
	if kind == "audit" {
		err := r.db.QueryRowContext(ctx, `SELECT count(*) FROM security_events WHERE created_at >= $1 AND created_at < $2 AND ($3='' OR actor_user_id=NULLIF($3,'')::uuid) AND ($4='' OR action=$4)`, f.From, f.To, f.UserID, f.Action).Scan(&result.Total)
		if err != nil {
			return result, err
		}
		rows, err := r.db.QueryContext(ctx, `SELECT e.id::text,e.actor_user_id::text,COALESCE(u.username,'Former administrator'),e.action,e.target_type,e.target_id::text,COALESCE(e.outcome,'success'),COALESCE(e.request_method,''),COALESCE(e.request_path,''),COALESCE(e.client_ip::text,''),COALESCE(e.user_agent,''),e.created_at
FROM security_events e LEFT JOIN admin_users u ON u.id=e.actor_user_id
WHERE e.created_at >= $1 AND e.created_at < $2 AND ($3='' OR e.actor_user_id=NULLIF($3,'')::uuid) AND ($4='' OR e.action=$4)
ORDER BY e.created_at DESC,e.id DESC LIMIT $5 OFFSET $6`, f.From, f.To, f.UserID, f.Action, f.PageSize, (f.Page-1)*f.PageSize)
		if err != nil {
			return result, err
		}
		defer rows.Close()
		for rows.Next() {
			var item Entry
			var user string
			if err = rows.Scan(&item.ID, &user, &item.Actor, &item.Action, &item.TargetType, &item.TargetID, &item.Outcome, &item.RequestMethod, &item.RequestPath, &item.ClientIP, &item.UserAgent, &item.CreatedAt); err != nil {
				return result, err
			}
			item.UserID = &user
			result.Items = append(result.Items, item)
		}
		return result, rows.Err()
	}
	err := r.db.QueryRowContext(ctx, `SELECT count(*) FROM login_events WHERE created_at >= $1 AND created_at < $2 AND ($3='' OR user_id=NULLIF($3,'')::uuid) AND ($4='' OR outcome=$4)`, f.From, f.To, f.UserID, f.Outcome).Scan(&result.Total)
	if err != nil {
		return result, err
	}
	rows, err := r.db.QueryContext(ctx, `SELECT e.id::text,e.user_id::text,COALESCE(u.username,'Unknown account'),e.account_kind,e.outcome,COALESCE(e.client_ip::text,''),COALESCE(e.user_agent,''),e.created_at
FROM login_events e LEFT JOIN admin_users u ON u.id=e.user_id
WHERE e.created_at >= $1 AND e.created_at < $2 AND ($3='' OR e.user_id=NULLIF($3,'')::uuid) AND ($4='' OR e.outcome=$4)
ORDER BY e.created_at DESC,e.id DESC LIMIT $5 OFFSET $6`, f.From, f.To, f.UserID, f.Outcome, f.PageSize, (f.Page-1)*f.PageSize)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		var item Entry
		var user sql.NullString
		if err = rows.Scan(&item.ID, &user, &item.Actor, &item.AccountKind, &item.Outcome, &item.ClientIP, &item.UserAgent, &item.CreatedAt); err != nil {
			return result, err
		}
		if user.Valid {
			item.UserID = &user.String
		}
		result.Items = append(result.Items, item)
	}
	return result, rows.Err()
}

func (r *Repository) Get(ctx context.Context, kind, id string) (Entry, error) {
	var item Entry
	if kind == "audit" {
		var user string
		err := r.db.QueryRowContext(ctx, `SELECT e.id::text,e.actor_user_id::text,COALESCE(u.username,'Former administrator'),e.action,e.target_type,e.target_id::text,COALESCE(e.outcome,'success'),COALESCE(e.request_method,''),COALESCE(e.request_path,''),COALESCE(e.client_ip::text,''),COALESCE(e.user_agent,''),e.created_at
FROM security_events e LEFT JOIN admin_users u ON u.id=e.actor_user_id WHERE e.id=$1`, id).Scan(&item.ID, &user, &item.Actor, &item.Action, &item.TargetType, &item.TargetID, &item.Outcome, &item.RequestMethod, &item.RequestPath, &item.ClientIP, &item.UserAgent, &item.CreatedAt)
		item.UserID = &user
		if errors.Is(err, sql.ErrNoRows) {
			return Entry{}, ErrNotFound
		}
		return item, err
	}
	var user sql.NullString
	err := r.db.QueryRowContext(ctx, `SELECT e.id::text,e.user_id::text,COALESCE(u.username,'Unknown account'),e.account_kind,e.outcome,COALESCE(e.client_ip::text,''),COALESCE(e.user_agent,''),e.created_at
FROM login_events e LEFT JOIN admin_users u ON u.id=e.user_id WHERE e.id=$1`, id).Scan(&item.ID, &user, &item.Actor, &item.AccountKind, &item.Outcome, &item.ClientIP, &item.UserAgent, &item.CreatedAt)
	if user.Valid {
		item.UserID = &user.String
	}
	if errors.Is(err, sql.ErrNoRows) {
		return Entry{}, ErrNotFound
	}
	return item, err
}
