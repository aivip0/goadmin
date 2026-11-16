package articles

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/lib/pq"
	"goadminkit/backend/internal/core/audit"
)

type Repository struct{ db *sql.DB }

func NewRepository(db *sql.DB) *Repository { return &Repository{db: db} }

func mapDB(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	code, constraint := "", ""
	var pg *pq.Error
	if errors.As(err, &pg) {
		code, constraint = string(pg.Code), pg.Constraint
	}
	var pgx *pgconn.PgError
	if errors.As(err, &pgx) {
		code, constraint = pgx.Code, pgx.ConstraintName
	}
	if code == "23505" && (constraint == "articles_slug_key" || constraint == "article_categories_slug_key") {
		return ErrConflict
	}
	if code == "23503" && constraint == "articles_category_id_fkey" {
		return ErrTransition
	}
	return err
}

func (r *Repository) write(ctx context.Context, actor, permission string, fn func(*sql.Tx) error) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var core string
	if err = tx.QueryRowContext(ctx, `SELECT id::text FROM app_metadata WHERE component='core' FOR UPDATE`).Scan(&core); err != nil {
		return err
	}
	var allowed bool
	err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM admin_users u JOIN user_roles ur ON ur.user_id=u.id
JOIN roles r ON r.id=ur.role_id AND r.status='active' AND r.is_super_admin
WHERE u.id=$1 AND u.status='active') OR EXISTS(SELECT 1 FROM admin_users u
JOIN user_roles ur ON ur.user_id=u.id JOIN roles r ON r.id=ur.role_id AND r.status='active'
JOIN role_permissions rp ON rp.role_id=r.id JOIN permissions p ON p.id=rp.permission_id
WHERE u.id=$1 AND u.status='active' AND p.code=$2)`, actor, permission).Scan(&allowed)
	if err != nil {
		return err
	}
	if !allowed {
		return ErrForbidden
	}
	if err = fn(tx); err != nil {
		return mapDB(err)
	}
	return mapDB(tx.Commit())
}

type scanner interface{ Scan(...any) error }

const articleColumns = `id::text,category_id::text,author_user_id::text,title,slug,summary,body,status,published_at,created_at,updated_at`
const categoryColumns = `id::text,name,slug,description,status,created_at,updated_at`

func scanArticle(row scanner) (Article, error) {
	var a Article
	err := row.Scan(&a.ID, &a.CategoryID, &a.AuthorUserID, &a.Title, &a.Slug, &a.Summary, &a.Body, &a.Status, &a.PublishedAt, &a.CreatedAt, &a.UpdatedAt)
	return a, mapDB(err)
}
func scanCategory(row scanner) (Category, error) {
	var c Category
	err := row.Scan(&c.ID, &c.Name, &c.Slug, &c.Description, &c.Status, &c.CreatedAt, &c.UpdatedAt)
	return c, mapDB(err)
}
func (r *Repository) Get(ctx context.Context, id string) (Article, error) {
	return scanArticle(r.db.QueryRowContext(ctx, `SELECT `+articleColumns+` FROM articles WHERE id=$1`, id))
}

func escapeLike(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `%`, `\%`)
	return strings.ReplaceAll(value, `_`, `\_`)
}
func (r *Repository) List(ctx context.Context, f Filter) (Page[Article], error) {
	result := Page[Article]{Items: []Article{}, Page: f.Page, PageSize: f.PageSize}
	query := ` FROM articles WHERE ($1='' OR title ILIKE $1 ESCAPE '\' OR slug ILIKE $1 ESCAPE '\')
AND ($2='' OR status=$2) AND ($3='' OR category_id::text=$3)`
	term := ""
	if f.Query != "" {
		term = "%" + escapeLike(strings.TrimSpace(f.Query)) + "%"
	}
	if err := r.db.QueryRowContext(ctx, `SELECT count(*)`+query, term, f.Status, f.CategoryID).Scan(&result.Total); err != nil {
		return result, err
	}
	rows, err := r.db.QueryContext(ctx, `SELECT `+articleColumns+query+` ORDER BY updated_at DESC,id DESC LIMIT $4 OFFSET $5`, term, f.Status, f.CategoryID, f.PageSize, (f.Page-1)*f.PageSize)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		a, e := scanArticle(rows)
		if e != nil {
			return result, e
		}
		result.Items = append(result.Items, a)
	}
	return result, rows.Err()
}
func (r *Repository) Categories(ctx context.Context, page, size int) (Page[Category], error) {
	result := Page[Category]{Items: []Category{}, Page: page, PageSize: size}
	if err := r.db.QueryRowContext(ctx, `SELECT count(*) FROM article_categories`).Scan(&result.Total); err != nil {
		return result, err
	}
	rows, err := r.db.QueryContext(ctx, `SELECT `+categoryColumns+` FROM article_categories ORDER BY name,id LIMIT $1 OFFSET $2`, size, (page-1)*size)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		c, e := scanCategory(rows)
		if e != nil {
			return result, e
		}
		result.Items = append(result.Items, c)
	}
	return result, rows.Err()
}
func (r *Repository) Create(ctx context.Context, actor string, in ArticleInput) (Article, error) {
	var result Article
	err := r.write(ctx, actor, "articles:create", func(tx *sql.Tx) error {
		var e error
		result, e = scanArticle(tx.QueryRowContext(ctx, `INSERT INTO articles(category_id,author_user_id,title,slug,summary,body)
VALUES($1,$2,$3,$4,$5,$6) RETURNING `+articleColumns, in.CategoryID, actor, in.Title, in.Slug, in.Summary, in.Body))
		if e != nil {
			return e
		}
		return audit.Insert(ctx, tx, actor, "article_created", "article", result.ID)
	})
	return result, err
}
func (r *Repository) Update(ctx context.Context, actor, id string, in ArticleInput) (Article, error) {
	var result Article
	err := r.write(ctx, actor, "articles:update", func(tx *sql.Tx) error {
		var e error
		result, e = scanArticle(tx.QueryRowContext(ctx, `UPDATE articles SET category_id=$2,title=$3,slug=$4,summary=$5,body=$6,
updated_at=clock_timestamp() WHERE id=$1 RETURNING `+articleColumns, id, in.CategoryID, in.Title, in.Slug, in.Summary, in.Body))
		if e != nil {
			return e
		}
		return audit.Insert(ctx, tx, actor, "article_updated", "article", id)
	})
	return result, err
}
func (r *Repository) Transition(ctx context.Context, actor, id, action string) (Article, error) {
	var result Article
	err := r.write(ctx, actor, "articles:publish", func(tx *sql.Tx) error {
		var status string
		if e := tx.QueryRowContext(ctx, `SELECT status FROM articles WHERE id=$1 FOR UPDATE`, id).Scan(&status); e != nil {
			return mapDB(e)
		}
		if (action == "publish" && status != "draft") || (action == "unpublish" && status != "published") {
			return ErrTransition
		}
		var e error
		if action == "publish" {
			result, e = scanArticle(tx.QueryRowContext(ctx, `UPDATE articles SET status='published',published_at=clock_timestamp(),updated_at=clock_timestamp() WHERE id=$1 RETURNING `+articleColumns, id))
		} else {
			result, e = scanArticle(tx.QueryRowContext(ctx, `UPDATE articles SET status='draft',published_at=NULL,updated_at=clock_timestamp() WHERE id=$1 RETURNING `+articleColumns, id))
		}
		if e != nil {
			return e
		}
		return audit.Insert(ctx, tx, actor, "article_"+action+"ed", "article", id)
	})
	return result, err
}
func (r *Repository) Delete(ctx context.Context, actor, id string) error {
	return r.write(ctx, actor, "articles:delete", func(tx *sql.Tx) error {
		var status string
		if e := tx.QueryRowContext(ctx, `SELECT status FROM articles WHERE id=$1 FOR UPDATE`, id).Scan(&status); e != nil {
			return mapDB(e)
		}
		if status != "draft" {
			return ErrTransition
		}
		if _, e := tx.ExecContext(ctx, `DELETE FROM articles WHERE id=$1`, id); e != nil {
			return e
		}
		return audit.Insert(ctx, tx, actor, "article_deleted", "article", id)
	})
}
func (r *Repository) CreateCategory(ctx context.Context, actor string, in CategoryInput) (Category, error) {
	var result Category
	err := r.write(ctx, actor, "article_categories:create", func(tx *sql.Tx) error {
		var e error
		result, e = scanCategory(tx.QueryRowContext(ctx, `INSERT INTO article_categories(name,slug,description,status)
VALUES($1,$2,$3,$4) RETURNING `+categoryColumns, in.Name, in.Slug, in.Description, in.Status))
		if e != nil {
			return e
		}
		return audit.Insert(ctx, tx, actor, "article_category_created", "article_category", result.ID)
	})
	return result, err
}
func (r *Repository) UpdateCategory(ctx context.Context, actor, id string, in CategoryInput) (Category, error) {
	var result Category
	err := r.write(ctx, actor, "article_categories:update", func(tx *sql.Tx) error {
		var e error
		result, e = scanCategory(tx.QueryRowContext(ctx, `UPDATE article_categories SET name=$2,slug=$3,description=$4,status=$5,
updated_at=clock_timestamp() WHERE id=$1 RETURNING `+categoryColumns, id, in.Name, in.Slug, in.Description, in.Status))
		if e != nil {
			return e
		}
		return audit.Insert(ctx, tx, actor, "article_category_updated", "article_category", id)
	})
	return result, err
}
func (r *Repository) DeleteCategory(ctx context.Context, actor, id string) error {
	return r.write(ctx, actor, "article_categories:delete", func(tx *sql.Tx) error {
		result, e := tx.ExecContext(ctx, `DELETE FROM article_categories WHERE id=$1`, id)
		if e != nil {
			if errors.Is(mapDB(e), ErrTransition) {
				return ErrConflict
			}
			return e
		}
		count, e := result.RowsAffected()
		if e != nil {
			return e
		}
		if count == 0 {
			return ErrNotFound
		}
		return audit.Insert(ctx, tx, actor, "article_category_deleted", "article_category", id)
	})
}
