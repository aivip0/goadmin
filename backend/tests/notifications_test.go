package tests

import (
	"context"
	"errors"
	"goadminkit/backend/internal/modules/auth"
	"goadminkit/backend/internal/modules/notifications"
	"testing"
	"time"
)

func TestPostgreSQLNotifications(t *testing.T) {
	db, _ := authDatabase(t)
	ctx := context.Background()
	identity, err := auth.NewService(auth.NewRepository(db.SQL), time.Hour, 30*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	root, err := identity.Initialize(ctx, auth.InitialAdmin{Username: "notice_root", Email: "notice_root@example.test", Password: "isolated notification test password"})
	if err != nil {
		t.Fatal(err)
	}
	if len(root.ID) != 36 { t.Fatalf("initializer returned invalid ID length %d",len(root.ID)) }
	repo := notifications.NewRepository(db.SQL)
	svc := notifications.NewService(repo)
	if _, err = svc.Create(ctx, root.ID, notifications.Input{RecipientID: "00000000-0000-4000-8000-000000000001", Title: "Hello", Body: "World"}); !errors.Is(err, notifications.ErrRecipient) {
		t.Fatalf("unknown recipient: %v", err)
	}
	item, err := svc.Create(ctx, root.ID, notifications.Input{RecipientID: root.ID, Title: " Hello ", Body: " World "})
	if err != nil || item.Title != "Hello" || item.ReadAt != nil {
		t.Fatal(item, err)
	}
	page, err := svc.List(ctx, root.ID, 1, 20)
	if err != nil || page.Total != 1 || page.Unread != 1 || len(page.Items) != 1 {
		t.Fatal(page, err)
	}
	if _, err = svc.Read(ctx, "00000000-0000-4000-8000-000000000002", item.ID); !errors.Is(err, notifications.ErrNotFound) {
		t.Fatalf("cross-user read: %v", err)
	}
	seen, err := svc.Read(ctx, root.ID, item.ID)
	if err != nil || seen.ReadAt == nil {
		t.Fatal(seen, err)
	}
	again, err := svc.Read(ctx, root.ID, item.ID)
	if err != nil || !again.ReadAt.Equal(*seen.ReadAt) {
		t.Fatal("read must be idempotent", err)
	}
	page, err = svc.List(ctx, root.ID, 1, 20)
	if err != nil || page.Unread != 0 {
		t.Fatal(page, err)
	}
	var count int
	if err = db.SQL.QueryRowContext(ctx, `SELECT count(*) FROM security_events WHERE target_type='notification' AND target_id=$1`, item.ID).Scan(&count); err != nil || count != 1 {
		t.Fatal("audit transaction missing", count, err)
	}
}
