package articles

import (
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/lib/pq"
)

func TestSlugContract(t *testing.T) {
	for _, tc := range []struct{ input, want string }{{" News-Item ", "news-item"}, {"ABC", "abc"}} {
		got, err := canonicalSlug(tc.input, 120)
		if err != nil || got != tc.want {
			t.Fatalf("%q => %q, %v", tc.input, got, err)
		}
	}
	for _, input := range []string{"", "bad slug", "a--b", "-bad", "bad-", "K", "uñicode", "a_b"} {
		if _, err := canonicalSlug(input, 120); !errors.Is(err, ErrInvalid) {
			t.Fatalf("accepted %q", input)
		}
	}
}

func TestPostgreSQLErrorMappingBothDrivers(t *testing.T) {
	for _, tc := range []struct {
		code, constraint string
		want             error
	}{
		{"23505", "articles_slug_key", ErrConflict},
		{"23505", "article_categories_slug_key", ErrConflict},
		{"23503", "articles_category_id_fkey", ErrTransition},
	} {
		if !errors.Is(mapDB(&pq.Error{Code: pq.ErrorCode(tc.code), Constraint: tc.constraint}), tc.want) ||
			!errors.Is(mapDB(&pgconn.PgError{Code: tc.code, ConstraintName: tc.constraint}), tc.want) {
			t.Fatalf("driver mapping failed for %s/%s", tc.code, tc.constraint)
		}
	}
	for _, err := range []error{
		&pq.Error{Code: "23505", Constraint: "other_unique_key"},
		&pgconn.PgError{Code: "23503", ConstraintName: "other_fkey"},
	} {
		if errors.Is(mapDB(err), ErrConflict) || errors.Is(mapDB(err), ErrTransition) {
			t.Fatal("unrelated database constraint treated as Articles domain error")
		}
	}
}

func TestValidation(t *testing.T) {
	a := ArticleInput{Title: " Demo ", Slug: " New-Post ", Body: " Body ", Summary: " Summary "}
	if err := normalizeArticle(&a); err != nil || a.Slug != "new-post" || a.Title != "Demo" {
		t.Fatal(a, err)
	}
	a.Body = " "
	if err := normalizeArticle(&a); !errors.Is(err, ErrInvalid) {
		t.Fatal("empty body accepted")
	}
	c := CategoryInput{Name: " Blog ", Slug: " BLOG "}
	if err := normalizeCategory(&c); err != nil || c.Slug != "blog" || c.Status != "active" {
		t.Fatal(c, err)
	}
	for _, f := range []Filter{{Page: 0, PageSize: 20}, {Page: 1, PageSize: 51}, {Page: 1, PageSize: 20, Status: "hidden"}, {Page: 1, PageSize: 20, CategoryID: "bad"}} {
		if err := validateFilter(f); !errors.Is(err, ErrInvalid) {
			t.Fatal(f)
		}
	}
}
