package audit

import (
	"context"
	"database/sql"
	"net"
	"strings"
)

type requestKey struct{}
type Request struct{ Method, Path, IP, Agent string }

// Path is the Gin route template, never the raw URL or query string.
func WithRequest(ctx context.Context, method, route, ip, agent string) context.Context {
	if !strings.HasPrefix(route, "/api/v1/") || strings.ContainsAny(route, "?#") {
		route = ""
	}
	if net.ParseIP(ip) == nil {
		ip = ""
	}
	return context.WithValue(ctx, requestKey{}, Request{method, route, ip, BrowserFamily(agent)})
}

func BrowserFamily(agent string) string {
	switch {
	case strings.Contains(agent, "Edg/"):
		return "Edge"
	case strings.Contains(agent, "Chrome/"):
		return "Chrome"
	case strings.Contains(agent, "Firefox/"):
		return "Firefox"
	case strings.Contains(agent, "Safari/"):
		return "Safari"
	default:
		return "Other"
	}
}

func Insert(ctx context.Context, tx *sql.Tx, actor, action, kind, id string) error {
	v, _ := ctx.Value(requestKey{}).(Request)
	var method, path, ip, agent any
	if v.Path != "" {
		method, path, agent = v.Method, v.Path, v.Agent
	}
	if v.IP != "" {
		ip = v.IP
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO security_events(actor_user_id,action,target_type,target_id,request_method,request_path,client_ip,user_agent,outcome)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,'success')`, actor, action, kind, id, method, path, ip, agent)
	return err
}
