package notifications

import (
	"context"
	"database/sql"
	"errors"
	"goadminkit/backend/internal/core/audit"
)

type Repository struct{ db *sql.DB }

func NewRepository(db *sql.DB) *Repository { return &Repository{db: db} }

const columns = `id::text,recipient_user_id::text,sender_user_id::text,title,body,read_at,created_at`

func scan(row interface{ Scan(...any) error }) (Item, error) {
	var i Item
	err := row.Scan(&i.ID, &i.RecipientID, &i.SenderID, &i.Title, &i.Body, &i.ReadAt, &i.CreatedAt)
	return i, err
}
func (r *Repository) List(ctx context.Context, user string, page, size int) (Page, error) {
	result := Page{Items: []Item{}, Page: page, PageSize: size}
	if err := r.db.QueryRowContext(ctx, `SELECT count(*),count(*) FILTER (WHERE read_at IS NULL) FROM admin_notifications WHERE recipient_user_id=$1`, user).Scan(&result.Total, &result.Unread); err != nil {
		return result, err
	}
	rows, err := r.db.QueryContext(ctx, `SELECT `+columns+` FROM admin_notifications WHERE recipient_user_id=$1 ORDER BY created_at DESC,id DESC LIMIT $2 OFFSET $3`, user, size, (page-1)*size)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		i, e := scan(rows)
		if e != nil {
			return result, e
		}
		result.Items = append(result.Items, i)
	}
	return result, rows.Err()
}
func (r *Repository) Create(ctx context.Context, sender string, in Input) (Item, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return Item{}, err
	}
	defer tx.Rollback()
	var guard string
	if err = tx.QueryRowContext(ctx, `SELECT id::text FROM app_metadata WHERE component='core' FOR UPDATE`).Scan(&guard); err != nil {
		return Item{}, err
	}
	var allowed bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM admin_users u JOIN user_roles ur ON ur.user_id=u.id
 JOIN roles r ON r.id=ur.role_id AND r.status='active' WHERE u.id=$1 AND u.status='active' AND r.is_super_admin)
 OR EXISTS(SELECT 1 FROM admin_users u JOIN user_roles ur ON ur.user_id=u.id JOIN roles r ON r.id=ur.role_id AND r.status='active'
 JOIN role_permissions rp ON rp.role_id=r.id JOIN permissions p ON p.id=rp.permission_id
 WHERE u.id=$1 AND u.status='active' AND p.code='notifications:create')`, sender).Scan(&allowed); err != nil {
		return Item{}, err
	}
	if !allowed {
		return Item{}, ErrForbidden
	}
	var active bool
	err = tx.QueryRowContext(ctx, `SELECT status='active' FROM admin_users WHERE id=$1`, in.RecipientID).Scan(&active)
	if errors.Is(err, sql.ErrNoRows) || !active && err == nil {
		return Item{}, ErrRecipient
	}
	if err != nil {
		return Item{}, err
	}
	item, err := scan(tx.QueryRowContext(ctx, `INSERT INTO admin_notifications(recipient_user_id,sender_user_id,title,body) VALUES($1,$2,$3,$4) RETURNING `+columns, in.RecipientID, sender, in.Title, in.Body))
	if err != nil {
		return Item{}, err
	}
	if err = audit.Insert(ctx, tx, sender, "notification_created", "notification", item.ID); err != nil {
		return Item{}, err
	}
	return item, tx.Commit()
}
func (r *Repository) Read(ctx context.Context, user, id string) (Item, error) {
	item, err := scan(r.db.QueryRowContext(ctx, `UPDATE admin_notifications SET read_at=COALESCE(read_at,clock_timestamp()) WHERE id=$1 AND recipient_user_id=$2 RETURNING `+columns, id, user))
	if errors.Is(err, sql.ErrNoRows) {
		return Item{}, ErrNotFound
	}
	return item, err
}
