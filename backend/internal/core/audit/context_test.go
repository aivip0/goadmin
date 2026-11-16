package audit

import (
	"context"
	"testing"
)

func TestRequestRedaction(t *testing.T) {
	ctx := WithRequest(context.Background(), "POST", "/api/v1/admin/users/:id", "203.0.113.5", "Mozilla/5.0 Chrome/123 secret")
	v := ctx.Value(requestKey{}).(Request)
	if v.Path != "/api/v1/admin/users/:id" || v.IP != "203.0.113.5" || v.Agent != "Chrome" {
		t.Fatal(v)
	}
	v = WithRequest(ctx, "POST", "/api/v1/admin/users?token=secret", "not-an-ip", "sensitive").Value(requestKey{}).(Request)
	if v.Path != "" || v.IP != "" || v.Agent != "Other" {
		t.Fatal(v)
	}
}
