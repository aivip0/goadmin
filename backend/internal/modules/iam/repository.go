package iam

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	auditlog "goadminkit/backend/internal/core/audit"
)

type Repository struct{ db *sql.DB }

func NewRepository(db *sql.DB) *Repository { return &Repository{db: db} }

// Every authorization query reads current membership and role status, never a session snapshot.
func (r *Repository) Rights(ctx context.Context, userID string) ([]string, bool, error) {
	return rights(ctx, r.db, userID)
}

type querier interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func rights(ctx context.Context, q querier, id string) ([]string, bool, error) {
	rows, err := q.QueryContext(ctx, `SELECT DISTINCT r.is_super_admin, p.code FROM admin_users u
JOIN user_roles ur ON ur.user_id=u.id JOIN roles r ON r.id=ur.role_id AND r.status='active'
LEFT JOIN role_permissions rp ON rp.role_id=r.id LEFT JOIN permissions p ON p.id=rp.permission_id
WHERE u.id=$1 AND u.status='active'`, id)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	seen := map[string]bool{}
	super := false
	for rows.Next() {
		var special bool
		var code sql.NullString
		if err = rows.Scan(&special, &code); err != nil {
			return nil, false, err
		}
		super = super || special
		if code.Valid {
			seen[code.String] = true
		}
	}
	if err = rows.Err(); err != nil {
		return nil, false, err
	}
	list := make([]string, 0, len(seen))
	for _, code := range catalog {
		if super || seen[code] {
			list = append(list, code)
		}
	}
	return list, super, nil
}

func (r *Repository) write(ctx context.Context, actor, required string, fn func(*sql.Tx, bool) error) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// All RBAC writes share this lock before touching users or role associations.
	var guard string
	if err = tx.QueryRowContext(ctx, "SELECT id FROM app_metadata WHERE component='core' FOR UPDATE").Scan(&guard); err != nil {
		return err
	}
	perms, super, err := rights(ctx, tx, actor)
	if err != nil {
		return err
	}
	if !super {
		allowed := false
		for _, p := range perms {
			if p == required {
				allowed = true
				break
			}
		}
		if !allowed {
			return ErrForbidden
		}
	}
	if err = fn(tx, super); err != nil {
		return err
	}
	var active int
	if err = tx.QueryRowContext(ctx, `SELECT count(DISTINCT u.id) FROM admin_users u
JOIN user_roles ur ON ur.user_id=u.id JOIN roles r ON r.id=ur.role_id
WHERE u.status='active' AND r.status='active' AND r.is_super_admin`).Scan(&active); err != nil {
		return err
	}
	if active == 0 {
		return ErrConflict
	}
	return tx.Commit()
}
func audit(ctx context.Context, tx *sql.Tx, actor, action, kind, id string) error {
	return auditlog.Insert(ctx, tx, actor, action, kind, id)
}
func userSuper(ctx context.Context, q querier, id string) (bool, error) {
	var super bool
	err := q.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM user_roles ur JOIN roles r ON r.id=ur.role_id AND r.is_super_admin WHERE ur.user_id=$1)`, id).Scan(&super)
	return super, err
}
func targetUser(ctx context.Context, tx *sql.Tx, id string) (string, error) {
	var status string
	err := tx.QueryRowContext(ctx, "SELECT status FROM admin_users WHERE id=$1", id).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return status, err
}
func targetRole(ctx context.Context, tx *sql.Tx, id string) (bool, bool, error) {
	var system, super bool
	err := tx.QueryRowContext(ctx, "SELECT is_system,is_super_admin FROM roles WHERE id=$1", id).Scan(&system, &super)
	if errors.Is(err, sql.ErrNoRows) {
		return false, false, ErrNotFound
	}
	return system, super, err
}

func (r *Repository) ListUsers(ctx context.Context, search, status string, page, size int) (Page[User], error) {
	result := Page[User]{Items: []User{}, Page: page, PageSize: size}
	term := "%" + strings.ToLower(search) + "%"
	err := r.db.QueryRowContext(ctx, "SELECT count(*) FROM admin_users WHERE lower(username) LIKE $1 AND ($2='' OR status=$2)", term, status).Scan(&result.Total)
	if err != nil {
		return result, err
	}
	rows, err := r.db.QueryContext(ctx, `SELECT id,username,email,display_name,status,created_at,updated_at FROM admin_users
WHERE lower(username) LIKE $1 AND ($2='' OR status=$2) ORDER BY created_at DESC,id DESC LIMIT $3 OFFSET $4`, term, status, size, (page-1)*size)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		var u User
		if err = rows.Scan(&u.ID, &u.Username, &u.Email, &u.DisplayName, &u.Status, &u.CreatedAt, &u.UpdatedAt); err != nil {
			return result, err
		}
		result.Items = append(result.Items, u)
	}
	return result, rows.Err()
}
func (r *Repository) User(ctx context.Context, id string) (User, error) {
	var u User
	err := r.db.QueryRowContext(ctx, "SELECT id,username,email,display_name,status,created_at,updated_at FROM admin_users WHERE id=$1", id).Scan(&u.ID, &u.Username, &u.Email, &u.DisplayName, &u.Status, &u.CreatedAt, &u.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return u, ErrNotFound
	}
	if err != nil {
		return u, err
	}
	u.Roles = []Role{}
	rows, err := r.db.QueryContext(ctx, "SELECT r.id,r.code,r.name,r.description,r.status,r.is_system,r.is_super_admin,r.created_at,r.updated_at FROM roles r JOIN user_roles ur ON ur.role_id=r.id WHERE ur.user_id=$1 ORDER BY r.code", id)
	if err != nil {
		return u, err
	}
	for rows.Next() {
		var role Role
		if err = scanRole(rows, &role); err != nil {
			break
		}
		u.Roles = append(u.Roles, role)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return u, err
	}
	u.Permissions, _, err = r.Rights(ctx, id)
	return u, err
}
func scanRole(rows *sql.Rows, r *Role) error {
	return rows.Scan(&r.ID, &r.Code, &r.Name, &r.Description, &r.Status, &r.IsSystem, &r.IsSuperAdmin, &r.CreatedAt, &r.UpdatedAt)
}
func (r *Repository) ListRoles(ctx context.Context) ([]Role, error) {
	roles := []Role{}
	rows, err := r.db.QueryContext(ctx, "SELECT id,code,name,description,status,is_system,is_super_admin,created_at,updated_at FROM roles ORDER BY is_system DESC,code")
	if err != nil {
		return roles, err
	}
	defer rows.Close()
	for rows.Next() {
		var role Role
		if err = scanRole(rows, &role); err != nil {
			return roles, err
		}
		roles = append(roles, role)
	}
	return roles, rows.Err()
}
func (r *Repository) Role(ctx context.Context, id string) (Role, error) {
	var role Role
	err := r.db.QueryRowContext(ctx, "SELECT id,code,name,description,status,is_system,is_super_admin,created_at,updated_at FROM roles WHERE id=$1", id).Scan(&role.ID, &role.Code, &role.Name, &role.Description, &role.Status, &role.IsSystem, &role.IsSuperAdmin, &role.CreatedAt, &role.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return role, ErrNotFound
	}
	if err != nil {
		return role, err
	}
	role.Permissions = []Permission{}
	role.Users = []UserSummary{}
	rows, err := r.db.QueryContext(ctx, "SELECT p.id,p.code,p.name,p.description FROM permissions p JOIN role_permissions rp ON rp.permission_id=p.id WHERE rp.role_id=$1 ORDER BY p.code", id)
	if err != nil {
		return role, err
	}
	for rows.Next() {
		var p Permission
		if err = rows.Scan(&p.ID, &p.Code, &p.Name, &p.Description); err != nil {
			break
		}
		role.Permissions = append(role.Permissions, p)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return role, err
	}
	rows, err = r.db.QueryContext(ctx, "SELECT u.id,u.username,u.status FROM admin_users u JOIN user_roles ur ON ur.user_id=u.id WHERE ur.role_id=$1 ORDER BY u.username", id)
	if err != nil {
		return role, err
	}
	defer rows.Close()
	for rows.Next() {
		var u UserSummary
		if err = rows.Scan(&u.ID, &u.Username, &u.Status); err != nil {
			return role, err
		}
		role.Users = append(role.Users, u)
	}
	return role, rows.Err()
}
func (r *Repository) Permissions(ctx context.Context) ([]Permission, error) {
	list := []Permission{}
	rows, err := r.db.QueryContext(ctx, "SELECT id,code,name,description FROM permissions ORDER BY code")
	if err != nil {
		return list, err
	}
	defer rows.Close()
	for rows.Next() {
		var p Permission
		if err = rows.Scan(&p.ID, &p.Code, &p.Name, &p.Description); err != nil {
			return list, err
		}
		list = append(list, p)
	}
	return list, rows.Err()
}
func (r *Repository) UserCreate(ctx context.Context, actor string, input UserInput, hash string) (string, error) {
	var id string
	err := r.write(ctx, actor, "users:create", func(tx *sql.Tx, _ bool) error {
		if err := tx.QueryRowContext(ctx, "INSERT INTO admin_users(username,email,display_name,password_hash) VALUES($1,$2,$3,$4) RETURNING id", input.Username, input.Email, input.DisplayName, hash).Scan(&id); err != nil {
			return err
		}
		return audit(ctx, tx, actor, "user_created", "user", id)
	})
	return id, err
}
func (r *Repository) UserUpdate(ctx context.Context, actor, id string, input UserInput) error {
	return r.write(ctx, actor, "users:update", func(tx *sql.Tx, super bool) error {
		if _, err := targetUser(ctx, tx, id); err != nil {
			return err
		}
		special, err := userSuper(ctx, tx, id)
		if err != nil {
			return err
		}
		if special && !super {
			return ErrForbidden
		}
		_, err = tx.ExecContext(ctx, "UPDATE admin_users SET username=$2,email=$3,display_name=$4,updated_at=clock_timestamp() WHERE id=$1", id, input.Username, input.Email, input.DisplayName)
		return err
	})
}
func (r *Repository) UserStatus(ctx context.Context, actor, id, status string) error {
	return r.write(ctx, actor, "users:update", func(tx *sql.Tx, super bool) error {
		old, err := targetUser(ctx, tx, id)
		if err != nil {
			return err
		}
		special, err := userSuper(ctx, tx, id)
		if err != nil {
			return err
		}
		if special && !super {
			return ErrForbidden
		}
		if old == status {
			return nil
		}
		_, err = tx.ExecContext(ctx, `UPDATE admin_users SET status=$2,auth_version=auth_version+1,updated_at=clock_timestamp() WHERE id=$1`, id, status)
		if err != nil {
			return err
		}
		if status == "disabled" {
			_, err = tx.ExecContext(ctx, "UPDATE admin_sessions SET revoked_at=clock_timestamp(),updated_at=clock_timestamp() WHERE user_id=$1 AND revoked_at IS NULL", id)
			if err != nil {
				return err
			}
		}
		if err = audit(ctx, tx, actor, "user_"+status, "user", id); err != nil {
			return err
		}
		if special {
			return audit(ctx, tx, actor, "super_status_changed", "user", id)
		}
		return nil
	})
}
func (r *Repository) UserDelete(ctx context.Context, actor, id string) error {
	return r.write(ctx, actor, "users:delete", func(tx *sql.Tx, super bool) error {
		if id == actor {
			return ErrForbidden
		}
		if _, err := targetUser(ctx, tx, id); err != nil {
			return err
		}
		special, err := userSuper(ctx, tx, id)
		if err != nil {
			return err
		}
		if special && !super {
			return ErrForbidden
		}
		if err = audit(ctx, tx, actor, "user_deleted", "user", id); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, "DELETE FROM admin_users WHERE id=$1", id)
		return err
	})
}
func (r *Repository) UserRoles(ctx context.Context, actor, id string, ids []string) error {
	return r.write(ctx, actor, "users:assign_roles", func(tx *sql.Tx, super bool) error {
		if id == actor && !super {
			return ErrForbidden
		}
		if _, err := targetUser(ctx, tx, id); err != nil {
			return err
		}
		was, err := userSuper(ctx, tx, id)
		if err != nil {
			return err
		}
		if was && !super {
			return ErrForbidden
		}
		for _, roleID := range ids {
			var system, special bool
			err = tx.QueryRowContext(ctx, "SELECT is_system,is_super_admin FROM roles WHERE id=$1 AND status='active'", roleID).Scan(&system, &special)
			if errors.Is(err, sql.ErrNoRows) {
				return ErrInvalid
			}
			if err != nil {
				return err
			}
			if (system || special) && !super {
				return ErrForbidden
			}
			if !super {
				var exceeds bool
				err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM role_permissions rp JOIN permissions p ON p.id=rp.permission_id WHERE rp.role_id=$1 AND NOT EXISTS(
SELECT 1 FROM user_roles ur JOIN roles r ON r.id=ur.role_id AND r.status='active' JOIN role_permissions own ON own.role_id=r.id WHERE ur.user_id=$2 AND own.permission_id=p.id))`, roleID, actor).Scan(&exceeds)
				if err != nil {
					return err
				}
				if exceeds {
					return ErrForbidden
				}
			}
		}
		_, err = tx.ExecContext(ctx, "DELETE FROM user_roles WHERE user_id=$1", id)
		if err != nil {
			return err
		}
		for _, roleID := range ids {
			if _, err = tx.ExecContext(ctx, "INSERT INTO user_roles(user_id,role_id) VALUES($1,$2)", id, roleID); err != nil {
				return err
			}
		}
		if err = audit(ctx, tx, actor, "user_roles_changed", "user", id); err != nil {
			return err
		}
		nowSuper, err := userSuper(ctx, tx, id)
		if err != nil {
			return err
		}
		if was != nowSuper {
			return audit(ctx, tx, actor, "super_membership_changed", "user", id)
		}
		return nil
	})
}
func (r *Repository) RoleCreate(ctx context.Context, actor string, input RoleInput) (string, error) {
	var id string
	err := r.write(ctx, actor, "roles:create", func(tx *sql.Tx, _ bool) error {
		if err := tx.QueryRowContext(ctx, "INSERT INTO roles(code,name,description) VALUES($1,$2,$3) RETURNING id", input.Code, input.Name, input.Description).Scan(&id); err != nil {
			return err
		}
		return audit(ctx, tx, actor, "role_created", "role", id)
	})
	return id, err
}
func (r *Repository) RoleUpdate(ctx context.Context, actor, id string, input RoleInput) error {
	return r.write(ctx, actor, "roles:update", func(tx *sql.Tx, _ bool) error {
		system, _, err := targetRole(ctx, tx, id)
		if err != nil {
			return err
		}
		if system {
			return ErrForbidden
		}
		_, err = tx.ExecContext(ctx, "UPDATE roles SET code=$2,name=$3,description=$4,updated_at=clock_timestamp() WHERE id=$1", id, input.Code, input.Name, input.Description)
		return err
	})
}
func (r *Repository) RoleDelete(ctx context.Context, actor, id string) error {
	return r.write(ctx, actor, "roles:delete", func(tx *sql.Tx, _ bool) error {
		system, _, err := targetRole(ctx, tx, id)
		if err != nil {
			return err
		}
		if system {
			return ErrForbidden
		}
		var members int
		err = tx.QueryRowContext(ctx, "SELECT count(*) FROM user_roles WHERE role_id=$1", id).Scan(&members)
		if err != nil {
			return err
		}
		if members > 0 {
			return ErrConflict
		}
		if err = audit(ctx, tx, actor, "role_deleted", "role", id); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, "DELETE FROM roles WHERE id=$1", id)
		return err
	})
}
func (r *Repository) RolePermissions(ctx context.Context, actor, id string, ids []string) error {
	return r.write(ctx, actor, "roles:assign_permissions", func(tx *sql.Tx, super bool) error {
		if !super {
			return ErrForbidden
		}
		_, special, err := targetRole(ctx, tx, id)
		if err != nil {
			return err
		}
		if special {
			return ErrForbidden
		}
		for _, permissionID := range ids {
			var exists bool
			err = tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM permissions WHERE id=$1)", permissionID).Scan(&exists)
			if err != nil {
				return err
			}
			if !exists {
				return ErrInvalid
			}
		}
		_, err = tx.ExecContext(ctx, "DELETE FROM role_permissions WHERE role_id=$1", id)
		if err != nil {
			return err
		}
		for _, permissionID := range ids {
			if _, err = tx.ExecContext(ctx, "INSERT INTO role_permissions(role_id,permission_id) VALUES($1,$2)", id, permissionID); err != nil {
				return err
			}
		}
		_, err = tx.ExecContext(ctx, "UPDATE roles SET permissions_version=permissions_version+1,updated_at=clock_timestamp() WHERE id=$1", id)
		if err != nil {
			return err
		}
		return audit(ctx, tx, actor, "role_permissions_changed", "role", id)
	})
}
func (r *Repository) SetPassword(ctx context.Context, actor, id, hash string) error {
	return r.write(ctx, actor, "users:update", func(tx *sql.Tx, super bool) error {
		if _, err := targetUser(ctx, tx, id); err != nil {
			return err
		}
		special, err := userSuper(ctx, tx, id)
		if err != nil {
			return err
		}
		if special && !super {
			return ErrForbidden
		}
		if _, err = tx.ExecContext(ctx, "UPDATE admin_users SET password_hash=$2,password_changed_at=clock_timestamp(),auth_version=auth_version+1,updated_at=clock_timestamp() WHERE id=$1", id, hash); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, "UPDATE admin_sessions SET revoked_at=clock_timestamp(),updated_at=clock_timestamp() WHERE user_id=$1 AND revoked_at IS NULL", id); err != nil {
			return err
		}
		return audit(ctx, tx, actor, "user_password_changed", "user", id)
	})
}
func dbError(err error) error {
	if err == nil {
		return nil
	}
	var coded interface{ SQLState() string }
	if errors.As(err, &coded) {
		switch coded.SQLState() {
		case "23505", "23503":
			return ErrConflict
		case "23514", "22001", "22P02":
			return ErrInvalid
		}
	}
	return fmt.Errorf("database operation failed: %w", err)
}
