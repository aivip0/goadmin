package dashboard

import "time"

type Summary struct {
	Administrators       int64  `json:"administrators"`
	ActiveAdministrators int64  `json:"active_administrators"`
	Roles                int64  `json:"roles"`
	ActiveRoles          int64  `json:"active_roles"`
	SecurityEvents       int64  `json:"security_events"`
	LoginSuccesses       int64  `json:"login_successes"`
	LoginFailures        int64  `json:"login_failures"`
	Service              string `json:"service"`
}

type Activity struct {
	ID         string    `json:"id"`
	Actor      string    `json:"actor"`
	Action     string    `json:"action"`
	TargetType string    `json:"target_type"`
	CreatedAt  time.Time `json:"created_at"`
}

type ActivityPage struct {
	Items    []Activity `json:"items"`
	Total    int64      `json:"total"`
	Page     int        `json:"page"`
	PageSize int        `json:"page_size"`
}

type TrendPoint struct {
	Date  string `json:"date"`
	Count int64  `json:"count"`
}

type Trends struct {
	Range          string       `json:"range"`
	Points         []TrendPoint `json:"points"`
	LoginSuccesses []TrendPoint `json:"login_successes"`
	LoginFailures  []TrendPoint `json:"login_failures"`
}
