package tests

import (
	"context"
	"errors"
	"goadminkit/backend/internal/modules/apikeys"
	"goadminkit/backend/internal/modules/auth"
	"strings"
	"testing"
	"time"
)

func TestPostgreSQLAPIKeys(t *testing.T) {
	db, _ := authDatabase(t)
	ctx := context.Background()
	identity, err := auth.NewService(auth.NewRepository(db.SQL), time.Hour, 30*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	user, err := identity.Initialize(ctx, auth.InitialAdmin{Username: "key_root", Email: "key_root@example.test", Password: "isolated API key test password"})
	if err != nil {
		t.Fatal(err)
	}
	svc := apikeys.NewService(apikeys.NewRepository(db.SQL))
	key, err := svc.Create(ctx, user.ID, apikeys.Input{Name: " automation ", ExpiresInDays: 7})
	if err != nil || key.Name != "automation" {
		t.Fatal(key, err)
	}
	owner, err := svc.Resolve(ctx, key.Token)
	if err != nil || owner != user.ID {
		t.Fatal(owner, err)
	}
	var hash []byte
	var count int
	if err = db.SQL.QueryRowContext(ctx, `SELECT token_hash FROM admin_api_keys WHERE id=$1`, key.ID).Scan(&hash); err != nil || len(hash) != 32 || strings.Contains(string(hash), key.Token) {
		t.Fatal("raw token stored", err)
	}
	list, err := svc.List(ctx, user.ID, 1, 20)
	if err != nil || list.Total != 1 || len(list.Items) != 1 {
		t.Fatal(list, err)
	}
	if err = db.SQL.QueryRowContext(ctx, `SELECT count(*) FROM security_events WHERE target_type='api_key' AND target_id=$1`, key.ID).Scan(&count); err != nil || count != 1 {
		t.Fatal("creation audit", count, err)
	}
	if err = svc.Revoke(ctx, "00000000-0000-4000-8000-000000000002", key.ID); !errors.Is(err, apikeys.ErrForbidden) {
		t.Fatal("cross-owner revoke", err)
	}
	if err = svc.Revoke(ctx, user.ID, key.ID); err != nil {
		t.Fatal(err)
	}
	if err = svc.Revoke(ctx, user.ID, key.ID); err != nil {
		t.Fatal("idempotent revoke", err)
	}
	if _, err = svc.Resolve(ctx, key.Token); !errors.Is(err, apikeys.ErrUnauthorized) {
		t.Fatal("revoked key accepted", err)
	}
	if err = db.SQL.QueryRowContext(ctx, `SELECT count(*) FROM security_events WHERE target_type='api_key' AND target_id=$1`, key.ID).Scan(&count); err != nil || count != 2 {
		t.Fatal("revocation audit", count, err)
	}
	second, err := svc.Create(ctx, user.ID, apikeys.Input{Name: "expires", ExpiresInDays: 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.SQL.ExecContext(ctx, `UPDATE admin_api_keys SET created_at=clock_timestamp()-interval '2 days',expires_at=clock_timestamp()-interval '1 day' WHERE id=$1`, second.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = svc.Resolve(ctx, second.Token); !errors.Is(err, apikeys.ErrUnauthorized) {
		t.Fatal("expired key accepted", err)
	}
	third, err := svc.Create(ctx, user.ID, apikeys.Input{Name: "disabled", ExpiresInDays: 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.SQL.ExecContext(ctx, `UPDATE admin_users SET status='disabled' WHERE id=$1`, user.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = svc.Resolve(ctx, third.Token); !errors.Is(err, apikeys.ErrUnauthorized) {
		t.Fatal("disabled owner key accepted", err)
	}
}
