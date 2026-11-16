package dashboard

import (
	"context"
	"errors"
	"time"
)

var ErrInvalid = errors.New("invalid dashboard request")

type Service struct{ repo *Repository }

func NewService(repo *Repository) *Service                      { return &Service{repo: repo} }
func (s *Service) Summary(ctx context.Context) (Summary, error) { return s.repo.Summary(ctx) }
func (s *Service) Activity(ctx context.Context, page, size int) (ActivityPage, error) {
	if page < 1 || page > 1000 || size < 1 || size > 50 {
		return ActivityPage{}, ErrInvalid
	}
	return s.repo.Activity(ctx, page, size)
}

func dayBounds(now time.Time, days int) (time.Time, time.Time) {
	end := now.UTC().Truncate(24 * time.Hour).Add(24 * time.Hour)
	return end.AddDate(0, 0, -days), end
}

func buildTrends(start time.Time, days int, counts map[string]int64) []TrendPoint {
	points := make([]TrendPoint, 0, days)
	for i := 0; i < days; i++ {
		date := start.AddDate(0, 0, i).Format("2006-01-02")
		points = append(points, TrendPoint{Date: date, Count: counts[date]})
	}
	return points
}

func (s *Service) Trends(ctx context.Context, interval string, now time.Time) (Trends, error) {
	days := 7
	if interval == "30d" {
		days = 30
	} else if interval != "7d" {
		return Trends{}, ErrInvalid
	}
	start, end := dayBounds(now, days)
	counts, err := s.repo.Trends(ctx, start, end)
	if err != nil {
		return Trends{}, err
	}
	success, failure, err := s.repo.LoginTrends(ctx, start, end)
	if err != nil {
		return Trends{}, err
	}
	return Trends{Range: interval, Points: buildTrends(start, days, counts), LoginSuccesses: buildTrends(start, days, success), LoginFailures: buildTrends(start, days, failure)}, nil
}
