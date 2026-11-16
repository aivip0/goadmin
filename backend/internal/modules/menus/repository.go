package menus

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	auditlog "goadminkit/backend/internal/core/audit"
)

type Repository struct{ db *sql.DB }

func NewRepository(db *sql.DB) *Repository { return &Repository{db: db} }

type queryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func scanMenu(rows interface{ Scan(...any) error }) (Menu, error) {
	var m Menu
	var parent, component, permission sql.NullString
	err := rows.Scan(&m.ID, &parent, &m.Code, &m.Name, &m.Type, &m.Path, &component, &m.Icon, &m.SortOrder, &m.Visible, &m.Status, &permission, &m.IsSystem)
	if parent.Valid {
		m.ParentID = &parent.String
	}
	if component.Valid {
		m.ComponentKey = component.String
	}
	if permission.Valid {
		m.PermissionCode = permission.String
	}
	return m, err
}

const menuSelect = `SELECT m.id::text,m.parent_id::text,m.code,m.name,m.menu_type,m.path,m.component_key,m.icon,m.sort_order,m.visible,m.status,p.code,m.is_system
FROM menus m LEFT JOIN permissions p ON p.id=m.permission_id`

func (r *Repository) List(ctx context.Context) ([]Menu, error) {
	rows, err := r.db.QueryContext(ctx, menuSelect+` ORDER BY m.parent_id NULLS FIRST,m.sort_order,m.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := make([]Menu, 0)
	for rows.Next() {
		m, e := scanMenu(rows)
		if e != nil {
			return nil, e
		}
		m.RoleIDs = []string{}
		list = append(list, m)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	roleRows, err := r.db.QueryContext(ctx, `SELECT menu_id::text,role_id::text FROM role_menus ORDER BY role_id,menu_id`)
	if err != nil {
		return nil, err
	}
	defer roleRows.Close()
	byID := make(map[string]int, len(list))
	for i := range list {
		byID[list[i].ID] = i
	}
	for roleRows.Next() {
		var menuID, roleID string
		if err = roleRows.Scan(&menuID, &roleID); err != nil {
			return nil, err
		}
		if i, ok := byID[menuID]; ok {
			list[i].RoleIDs = append(list[i].RoleIDs, roleID)
		}
	}
	return list, roleRows.Err()
}

func (r *Repository) Has(ctx context.Context, userID, permission string) (bool, error) {
	var allowed bool
	err := r.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM user_roles ur JOIN roles r ON r.id=ur.role_id AND r.status='active' AND r.is_super_admin JOIN admin_users u ON u.id=ur.user_id AND u.status='active' WHERE u.id=$1)
OR EXISTS(SELECT 1 FROM user_roles ur JOIN roles r ON r.id=ur.role_id AND r.status='active' JOIN role_permissions rp ON rp.role_id=r.id JOIN permissions p ON p.id=rp.permission_id JOIN admin_users u ON u.id=ur.user_id AND u.status='active' WHERE u.id=$1 AND p.code=$2)`, userID, permission).Scan(&allowed)
	return allowed, err
}

func (r *Repository) Get(ctx context.Context, id string) (Menu, error) {
	var m Menu
	var parent, component, permission sql.NullString
	err := r.db.QueryRowContext(ctx, menuSelect+` WHERE m.id=$1`, id).Scan(&m.ID, &parent, &m.Code, &m.Name, &m.Type, &m.Path, &component, &m.Icon, &m.SortOrder, &m.Visible, &m.Status, &permission, &m.IsSystem)
	if errors.Is(err, sql.ErrNoRows) {
		return m, ErrNotFound
	}
	if err != nil {
		return m, err
	}
	if parent.Valid {
		m.ParentID = &parent.String
	}
	if component.Valid {
		m.ComponentKey = component.String
	}
	if permission.Valid {
		m.PermissionCode = permission.String
	}
	m.RoleIDs = []string{}
	rows, err := r.db.QueryContext(ctx, `SELECT role_id::text FROM role_menus WHERE menu_id=$1 ORDER BY role_id`, id)
	if err != nil {
		return m, err
	}
	defer rows.Close()
	for rows.Next() {
		var role string
		if err = rows.Scan(&role); err != nil {
			return m, err
		}
		m.RoleIDs = append(m.RoleIDs, role)
	}
	return m, rows.Err()
}

func (r *Repository) My(ctx context.Context, userID string) ([]Menu, error) {
	query := `WITH RECURSIVE permitted AS (
SELECT m.id,m.parent_id,m.menu_type FROM menus m
WHERE m.visible=true AND m.status='active' AND (m.menu_type='directory' OR m.permission_id IS NOT NULL)
AND (EXISTS(SELECT 1 FROM user_roles ur JOIN roles r ON r.id=ur.role_id AND r.status='active' AND r.is_super_admin WHERE ur.user_id=$1)
 OR EXISTS(SELECT 1 FROM user_roles ur JOIN roles r ON r.id=ur.role_id AND r.status='active' JOIN role_menus rm ON rm.role_id=r.id WHERE ur.user_id=$1 AND rm.menu_id=m.id)
 OR (m.code LIKE 'plugin!_%' ESCAPE '!' AND EXISTS(SELECT 1 FROM user_roles ur JOIN roles r ON r.id=ur.role_id AND r.status='active' JOIN role_permissions rp ON rp.role_id=r.id WHERE ur.user_id=$1 AND rp.permission_id=m.permission_id)))
AND (m.permission_id IS NULL OR EXISTS(SELECT 1 FROM user_roles ur JOIN roles r ON r.id=ur.role_id AND r.status='active' JOIN role_permissions rp ON rp.role_id=r.id WHERE ur.user_id=$1 AND rp.permission_id=m.permission_id)
 OR EXISTS(SELECT 1 FROM user_roles ur JOIN roles r ON r.id=ur.role_id AND r.status='active' AND r.is_super_admin WHERE ur.user_id=$1))),
visible_ids(id,parent_id,menu_type) AS (
SELECT id,parent_id,menu_type FROM permitted WHERE menu_type<>'directory'
UNION
SELECT parent.id,parent.parent_id,parent.menu_type FROM visible_ids child
JOIN menus parent ON parent.id=child.parent_id AND parent.menu_type='directory' AND parent.visible=true AND parent.status='active'
)
	` + menuSelect + ` WHERE m.id IN (SELECT id FROM visible_ids) ORDER BY m.parent_id NULLS FIRST,m.sort_order,m.id`
	rows, err := r.db.QueryContext(ctx, query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := make([]Menu, 0)
	for rows.Next() {
		m, e := scanMenu(rows)
		if e != nil {
			return nil, e
		}
		list = append(list, m)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return list, nil
}

func (r *Repository) write(ctx context.Context, actor, permission string, fn func(*sql.Tx) error) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var guard string
	if err = tx.QueryRowContext(ctx, `SELECT id::text FROM app_metadata WHERE component='core' FOR UPDATE`).Scan(&guard); err != nil {
		return err
	}
	var allowed bool
	err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM user_roles ur JOIN roles r ON r.id=ur.role_id AND r.status='active' AND r.is_super_admin JOIN admin_users u ON u.id=ur.user_id AND u.status='active' WHERE u.id=$1)
OR EXISTS(SELECT 1 FROM user_roles ur JOIN roles r ON r.id=ur.role_id AND r.status='active' JOIN role_permissions rp ON rp.role_id=r.id JOIN permissions p ON p.id=rp.permission_id JOIN admin_users u ON u.id=ur.user_id AND u.status='active' WHERE u.id=$1 AND p.code=$2)`, actor, permission).Scan(&allowed)
	if err != nil {
		return err
	}
	if !allowed {
		return ErrForbidden
	}
	if err = fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}

func audit(ctx context.Context, tx *sql.Tx, actor, action, id string) error {
	return auditlog.Insert(ctx, tx, actor, action, "menu", id)
}

func (r *Repository) validateRefs(ctx context.Context, tx *sql.Tx, id string, in Input) error {
	if in.ParentID != nil {
		if !validID(*in.ParentID) || *in.ParentID == id {
			return ErrInvalid
		}
		var kind string
		err := tx.QueryRowContext(ctx, `SELECT menu_type FROM menus WHERE id=$1`, *in.ParentID).Scan(&kind)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrInvalid
		}
		if err != nil {
			return err
		}
		if kind != "directory" {
			return ErrInvalid
		}
		if id != "" {
			var cycle bool
			err = tx.QueryRowContext(ctx, `WITH RECURSIVE ancestors(id,parent_id) AS (SELECT id,parent_id FROM menus WHERE id=$1 UNION ALL SELECT m.id,m.parent_id FROM menus m JOIN ancestors a ON m.id=a.parent_id) SELECT EXISTS(SELECT 1 FROM ancestors WHERE id=$2)`, *in.ParentID, id).Scan(&cycle)
			if err != nil {
				return err
			}
			if cycle {
				return ErrInvalid
			}
		}
	}
	if in.Type != "directory" {
		var exists bool
		err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM permissions WHERE code=$1)`, in.PermissionCode).Scan(&exists)
		if err != nil {
			return err
		}
		if !exists {
			return ErrInvalid
		}
	}
	return nil
}

func (r *Repository) Create(ctx context.Context, actor string, in Input) (string, error) {
	var id string
	err := r.write(ctx, actor, "menus:create", func(tx *sql.Tx) error {
		if err := r.validateRefs(ctx, tx, "", in); err != nil {
			return err
		}
		var permissionID any
		if in.PermissionCode != "" {
			permissionID = in.PermissionCode
		}
		err := tx.QueryRowContext(ctx, `INSERT INTO menus(parent_id,permission_id,code,name,path,icon,sort_order,visible,status,menu_type,component_key)
VALUES($1,(SELECT id FROM permissions WHERE code=$2),$3,$4,$5,$6,$7,$8,$9,$10,NULLIF($11,'')) RETURNING id::text`, in.ParentID, permissionID, in.Code, in.Name, in.Path, in.Icon, in.SortOrder, in.Visible, in.Status, in.Type, in.ComponentKey).Scan(&id)
		if err != nil {
			return classify(err)
		}
		return audit(ctx, tx, actor, "menu_created", id)
	})
	return id, err
}

func (r *Repository) Update(ctx context.Context, actor, id string, in Input) error {
	return r.write(ctx, actor, "menus:update", func(tx *sql.Tx) error {
		if err := r.validateRefs(ctx, tx, id, in); err != nil {
			return err
		}
		var oldParent, oldPermission sql.NullString
		var oldVisible bool
		if err := tx.QueryRowContext(ctx, `SELECT parent_id::text,permission_id::text,visible FROM menus WHERE id=$1 FOR UPDATE`, id).Scan(&oldParent, &oldPermission, &oldVisible); errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		} else if err != nil {
			return err
		}
		var permissionID any
		if in.PermissionCode != "" {
			permissionID = in.PermissionCode
		}
		var incompatibleRoles bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(
SELECT 1 FROM role_menus rm JOIN roles r ON r.id=rm.role_id
WHERE rm.menu_id=$1 AND NOT r.is_super_admin AND $2<>''
AND NOT EXISTS(SELECT 1 FROM role_permissions rp JOIN permissions p ON p.id=rp.permission_id WHERE rp.role_id=r.id AND p.code=$2))`, id, in.PermissionCode).Scan(&incompatibleRoles); err != nil {
			return err
		}
		if incompatibleRoles {
			return ErrConflict
		}
		_, err := tx.ExecContext(ctx, `UPDATE menus SET parent_id=$2,permission_id=(SELECT id FROM permissions WHERE code=$3),code=$4,name=$5,path=$6,icon=$7,sort_order=$8,visible=$9,status=$10,menu_type=$11,component_key=NULLIF($12,''),updated_at=clock_timestamp() WHERE id=$1`, id, in.ParentID, permissionID, in.Code, in.Name, in.Path, in.Icon, in.SortOrder, in.Visible, in.Status, in.Type, in.ComponentKey)
		if err != nil {
			return classify(err)
		}
		if err = audit(ctx, tx, actor, "menu_updated", id); err != nil {
			return err
		}
		if oldParent.String != stringValue(in.ParentID) {
			if err = audit(ctx, tx, actor, "menu_hierarchy_changed", id); err != nil {
				return err
			}
		}
		if oldPermission.String != in.PermissionCode {
			if err = audit(ctx, tx, actor, "menu_permission_changed", id); err != nil {
				return err
			}
		}
		if oldVisible != in.Visible {
			if err = audit(ctx, tx, actor, "menu_visibility_changed", id); err != nil {
				return err
			}
		}
		return nil
	})
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func (r *Repository) Delete(ctx context.Context, actor, id string) error {
	return r.write(ctx, actor, "menus:delete", func(tx *sql.Tx) error {
		var system bool
		err := tx.QueryRowContext(ctx, `SELECT is_system FROM menus WHERE id=$1 FOR UPDATE`, id).Scan(&system)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if system {
			return ErrForbidden
		}
		var children int
		if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM menus WHERE parent_id=$1`, id).Scan(&children); err != nil {
			return err
		}
		if children > 0 {
			return ErrConflict
		}
		if err = audit(ctx, tx, actor, "menu_deleted", id); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `DELETE FROM menus WHERE id=$1`, id)
		return err
	})
}

func (r *Repository) SetRoles(ctx context.Context, actor, id string, roleIDs []string) error {
	return r.write(ctx, actor, "menus:assign_roles", func(tx *sql.Tx) error {
		var permission sql.NullString
		err := tx.QueryRowContext(ctx, `SELECT p.code FROM menus m LEFT JOIN permissions p ON p.id=m.permission_id WHERE m.id=$1 FOR UPDATE OF m`, id).Scan(&permission)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		seen := map[string]bool{}
		for _, role := range roleIDs {
			if !validID(role) || seen[role] {
				return ErrInvalid
			}
			seen[role] = true
			var valid bool
			err = tx.QueryRowContext(ctx, `SELECT r.status='active' AND (r.is_super_admin OR $2='' OR EXISTS(SELECT 1 FROM role_permissions rp JOIN permissions p ON p.id=rp.permission_id WHERE rp.role_id=r.id AND p.code=$2)) FROM roles r WHERE r.id=$1`, role, permission.String).Scan(&valid)
			if errors.Is(err, sql.ErrNoRows) || !valid {
				return ErrInvalid
			}
			if err != nil {
				return err
			}
		}
		if _, err = tx.ExecContext(ctx, `DELETE FROM role_menus WHERE menu_id=$1`, id); err != nil {
			return err
		}
		for _, role := range roleIDs {
			if _, err = tx.ExecContext(ctx, `INSERT INTO role_menus(role_id,menu_id) VALUES($1,$2)`, role, id); err != nil {
				return classify(err)
			}
		}
		return audit(ctx, tx, actor, "menu_roles_changed", id)
	})
}

func (r *Repository) Reorder(ctx context.Context, actor string, items []ReorderItem) error {
	return r.write(ctx, actor, "menus:update", func(tx *sql.Tx) error {
		seen := map[string]bool{}
		for _, item := range items {
			if !validID(item.ID) || seen[item.ID] {
				return ErrInvalid
			}
			seen[item.ID] = true
			res, err := tx.ExecContext(ctx, `UPDATE menus SET sort_order=$2,updated_at=clock_timestamp() WHERE id=$1`, item.ID, item.SortOrder)
			if err != nil {
				return err
			}
			n, err := res.RowsAffected()
			if err != nil {
				return err
			}
			if n != 1 {
				return ErrNotFound
			}
			if err = audit(ctx, tx, actor, "menu_reordered", item.ID); err != nil {
				return err
			}
		}
		return nil
	})
}

func (r *Repository) Roles(ctx context.Context) ([]Role, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id::text,code,name FROM roles WHERE status='active' ORDER BY code`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := make([]Role, 0)
	for rows.Next() {
		var v Role
		if err = rows.Scan(&v.ID, &v.Code, &v.Name); err != nil {
			return nil, err
		}
		list = append(list, v)
	}
	return list, rows.Err()
}

func (r *Repository) Permissions(ctx context.Context) ([]Permission, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id::text,code,name,description FROM permissions ORDER BY code`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := make([]Permission, 0)
	for rows.Next() {
		var v Permission
		if err = rows.Scan(&v.ID, &v.Code, &v.Name, &v.Description); err != nil {
			return nil, err
		}
		list = append(list, v)
	}
	return list, rows.Err()
}

var defaults = []Input{
	{Code: "system_dashboard", Name: "Dashboard", Type: "page", Path: "/admin", ComponentKey: "dashboard", Icon: "LayoutDashboard", SortOrder: 10, Visible: true, Status: "active", PermissionCode: "dashboard:view"},
	{Code: "system_users", Name: "Users", Type: "page", Path: "/admin/users", ComponentKey: "users", Icon: "Users", SortOrder: 20, Visible: true, Status: "active", PermissionCode: "users:view"},
	{Code: "system_roles", Name: "Roles", Type: "page", Path: "/admin/roles", ComponentKey: "roles", Icon: "Shield", SortOrder: 30, Visible: true, Status: "active", PermissionCode: "roles:view"},
	{Code: "system_permissions", Name: "Permissions", Type: "page", Path: "/admin/permissions", ComponentKey: "permissions", Icon: "KeyRound", SortOrder: 40, Visible: true, Status: "active", PermissionCode: "permissions:view"},
	{Code: "system_menus", Name: "Menus", Type: "page", Path: "/admin/menus", ComponentKey: "menus", Icon: "ListTree", SortOrder: 50, Visible: true, Status: "active", PermissionCode: "menus:view"},
	{Code: "system_settings", Name: "Settings", Type: "page", Path: "/admin/settings", ComponentKey: "settings", Icon: "Settings", SortOrder: 60, Visible: true, Status: "active", PermissionCode: "settings:view"},
	{Code: "system_media", Name: "Media", Type: "page", Path: "/admin/media", ComponentKey: "media", Icon: "Images", SortOrder: 70, Visible: true, Status: "active", PermissionCode: "media:view"},
	{Code: "system_audit_logs", Name: "Audit logs", Type: "page", Path: "/admin/audit-logs", ComponentKey: "audit_logs", Icon: "ScrollText", SortOrder: 80, Visible: true, Status: "active", PermissionCode: "audit:view"},
	{Code: "system_login_logs", Name: "Login logs", Type: "page", Path: "/admin/login-logs", ComponentKey: "login_logs", Icon: "LogIn", SortOrder: 90, Visible: true, Status: "active", PermissionCode: "login_logs:view"},
}

func (r *Repository) Initialize(ctx context.Context, actor string) error {
	return r.write(ctx, actor, "menus:create", func(tx *sql.Tx) error {
		for _, seed := range defaults {
			var id string
			err := tx.QueryRowContext(ctx, `INSERT INTO menus(code,name,path,icon,sort_order,visible,status,menu_type,component_key,is_system,permission_id)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,true,(SELECT id FROM permissions WHERE code=$10)) ON CONFLICT(code) DO NOTHING RETURNING id::text`, seed.Code, seed.Name, seed.Path, seed.Icon, seed.SortOrder, seed.Visible, seed.Status, seed.Type, seed.ComponentKey, seed.PermissionCode).Scan(&id)
			if errors.Is(err, sql.ErrNoRows) {
				var system bool
				if err = tx.QueryRowContext(ctx, `SELECT id::text,is_system FROM menus WHERE code=$1`, seed.Code).Scan(&id, &system); err != nil {
					return err
				}
				if !system {
					return ErrConflict
				}
				continue
			}
			if err != nil {
				return err
			}
			if err = audit(ctx, tx, actor, "menu_initialized", id); err != nil {
				return err
			}
		}
		return nil
	})
}

func (r *Repository) BootstrapActor(ctx context.Context) (string, error) {
	var id string
	err := r.db.QueryRowContext(ctx, `SELECT u.id::text FROM admin_users u JOIN user_roles ur ON ur.user_id=u.id JOIN roles r ON r.id=ur.role_id AND r.is_super_admin AND r.status='active' WHERE u.status='active' ORDER BY u.created_at,u.id LIMIT 1`).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return id, err
}

func classify(err error) error {
	var state interface{ SQLState() string }
	if errors.As(err, &state) {
		switch state.SQLState() {
		case "23505":
			return fmt.Errorf("%w", ErrConflict)
		case "23503", "23514":
			return fmt.Errorf("%w", ErrInvalid)
		}
	}
	return err
}
