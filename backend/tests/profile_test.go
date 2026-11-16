package tests

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"goadminkit/backend/internal/modules/auth"
	"goadminkit/backend/internal/modules/iam"
	"goadminkit/backend/internal/modules/profile"
)

func TestPostgreSQLProfileSessionsAndPasswordLifecycle(t *testing.T) {
	db, _ := authDatabase(t)
	ctx := context.Background()
	identity, err := auth.NewService(auth.NewRepository(db.SQL), time.Hour, 30*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	root, err := identity.Initialize(ctx, auth.InitialAdmin{Username: "profile_root", Email: "profile_root@example.test", Password: "original profile integration password", DisplayName: "Profile Root"})
	if err != nil {
		t.Fatal(err)
	}
	_, first, err := identity.Login(ctx, root.Username, "original profile integration password", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	_, second, err := identity.Login(ctx, root.Username, "original profile integration password", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	repo := profile.NewRepository(db.SQL, 30*time.Minute)
	svc := profile.NewService(repo)
	rights := iam.NewService(iam.NewRepository(db.SQL))
	foreign, err := rights.CreateUser(ctx, root.ID, iam.UserInput{Username: "profile_foreign", Email: "profile_taken@example.test", Password: "dedicated foreign profile password"})
	if err != nil {
		t.Fatal(err)
	}
	_, foreignToken, err := identity.Login(ctx, foreign.Username, "dedicated foreign profile password", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	page, err := svc.Sessions(ctx, first, 1, 20)
	if err != nil || page.Total != 2 || len(page.Items) != 2 {
		t.Fatalf("session list: %#v %v", page, err)
	}
	var currentID string
	for _, item := range page.Items {
		if item.Current {
			currentID = item.ID
		}
	}
	if currentID == "" {
		t.Fatal("current session not identified")
	}
	foreignSessions, err := svc.Sessions(ctx, foreignToken, 1, 20)
	if err != nil || foreignSessions.Total != 1 {
		t.Fatalf("foreign user session: %#v %v", foreignSessions, err)
	}
	if _, err = svc.Revoke(ctx, first, foreignSessions.Items[0].ID, false); !errors.Is(err, profile.ErrNotFound) {
		t.Fatalf("foreign session was exposed or revocable: %v", err)
	}
	if _, err = svc.Revoke(ctx, first, "00000000-0000-4000-8000-000000000001", false); !errors.Is(err, profile.ErrNotFound) {
		t.Fatalf("missing session: %v", err)
	}
	if _, err = svc.Revoke(ctx, first, currentID, false); err != nil {
		t.Fatalf("owned session revoke: %v", err)
	}
	if _, err = identity.Me(ctx, second); err != nil {
		t.Fatalf("selected other session still valid unexpectedly: %v", err)
	}
	if _, err = identity.Me(ctx, first); !errors.Is(err, auth.ErrUnauthorized) {
		t.Fatalf("selected current session should be invalidated: %v", err)
	}
	_, first, err = identity.Login(ctx, root.Username, "original profile integration password", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = svc.Update(ctx, first, profile.Input{Email: "updated_profile@example.test", DisplayName: "Updated"}); err != nil {
		t.Fatal(err)
	}
	if _, err = svc.Update(ctx, first, profile.Input{Email: foreign.Email, DisplayName: "Conflict"}); !errors.Is(err, profile.ErrConflict) {
		t.Fatalf("duplicate email was not rejected: %v", err)
	}
	updated, err := svc.Get(ctx, first)
	if err != nil || updated.Email != "updated_profile@example.test" || updated.DisplayName != "Updated" {
		t.Fatalf("conflicting update was not atomic: %#v %v", updated, err)
	}
	if err = svc.Password(ctx, first, profile.PasswordInput{CurrentPassword: "incorrect current password", NewPassword: "new dedicated integration password"}); !errors.Is(err, profile.ErrPassword) {
		t.Fatalf("wrong current password: %v", err)
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for range 2 {
		wg.Go(func() {
			results <- svc.Password(ctx, first, profile.PasswordInput{CurrentPassword: "original profile integration password", NewPassword: "new dedicated integration password"})
		})
	}
	wg.Wait()
	close(results)
	committed := 0
	for e := range results {
		if e == nil {
			committed++
		} else if !errors.Is(e, auth.ErrUnauthorized) && !errors.Is(e, profile.ErrPassword) {
			t.Fatalf("concurrent password result: %v", e)
		}
	}
	if committed != 1 {
		t.Fatalf("expected one password commit, got %d", committed)
	}
	if _, err = identity.Me(ctx, first); !errors.Is(err, auth.ErrUnauthorized) {
		t.Fatalf("old session survived password change: %v", err)
	}
	if _, _, err = identity.Login(ctx, root.Username, "original profile integration password", "127.0.0.1"); !errors.Is(err, auth.ErrCredentials) {
		t.Fatalf("old password still valid: %v", err)
	}
	_, next, err := identity.Login(ctx, root.Username, "new dedicated integration password", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	_, third, err := identity.Login(ctx, root.Username, "new dedicated integration password", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = svc.Revoke(ctx, next, "", true); err != nil {
		t.Fatal(err)
	}
	if _, err = identity.Me(ctx, next); err != nil {
		t.Fatalf("revoke-others revoked current: %v", err)
	}
	if _, err = identity.Me(ctx, third); !errors.Is(err, auth.ErrUnauthorized) {
		t.Fatalf("revoke-others left other active: %v", err)
	}
	page, err = svc.Sessions(ctx, next, 1, 20)
	if err != nil || page.Total != 1 || !page.Items[0].Current {
		t.Fatalf("unexpected live sessions: %#v %v", page, err)
	}
	if revoked, err := svc.Revoke(ctx, next, page.Items[0].ID, false); err != nil || !revoked {
		t.Fatalf("revoke current: %v %v", revoked, err)
	}
	if _, err = identity.Me(ctx, next); !errors.Is(err, auth.ErrUnauthorized) {
		t.Fatalf("revoked current still valid: %v", err)
	}
	var actions int
	if err = db.SQL.QueryRow(`SELECT count(*) FROM security_events WHERE actor_user_id=$1 AND action IN ('profile_updated','password_changed','session_revoked','sessions_revoked')`, root.ID).Scan(&actions); err != nil || actions < 4 {
		t.Fatalf("audit writes: %d %v", actions, err)
	}
	var storedHash string
	if err = db.SQL.QueryRow(`SELECT password_hash FROM admin_users WHERE id=$1`, root.ID).Scan(&storedHash); err != nil || strings.Contains(storedHash, "new dedicated integration password") {
		t.Fatalf("raw password persisted: %v", err)
	}
}
