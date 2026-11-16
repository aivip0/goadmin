BEGIN;

DO $$
DECLARE
    current_version integer;
BEGIN
    SELECT schema_version INTO current_version
    FROM app_metadata WHERE component='core' FOR UPDATE;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'articles migration requires core metadata row';
    END IF;
    IF current_version IS DISTINCT FROM 8 THEN
        RAISE EXCEPTION 'articles migration requires schema version 8';
    END IF;
END $$;

CREATE TABLE article_categories (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name varchar(120) NOT NULL CHECK (length(btrim(name)) > 0),
    slug varchar(120) NOT NULL,
    description text NOT NULL DEFAULT '' CHECK (length(description) <= 2000),
    status varchar(16) NOT NULL DEFAULT 'active' CHECK (status IN ('active','disabled')),
    created_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT article_categories_slug_format_check CHECK (slug ~ '^[a-z0-9]+(-[a-z0-9]+)*$'),
    CONSTRAINT article_categories_slug_key UNIQUE (slug)
);

CREATE TABLE articles (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    category_id uuid,
    author_user_id uuid NOT NULL REFERENCES admin_users(id) ON DELETE RESTRICT,
    title varchar(200) NOT NULL CHECK (length(btrim(title)) > 0),
    slug varchar(220) NOT NULL,
    summary varchar(500) NOT NULL DEFAULT '',
    body text NOT NULL CHECK (length(btrim(body)) BETWEEN 1 AND 100000),
    status varchar(16) NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','published')),
    published_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT articles_category_id_fkey FOREIGN KEY (category_id) REFERENCES article_categories(id) ON DELETE RESTRICT,
    CONSTRAINT articles_slug_format_check CHECK (slug ~ '^[a-z0-9]+(-[a-z0-9]+)*$'),
    CONSTRAINT articles_slug_key UNIQUE (slug),
    CONSTRAINT articles_publication_check CHECK (
        (status='draft' AND published_at IS NULL) OR
        (status='published' AND published_at IS NOT NULL)
    )
);
CREATE INDEX articles_status_published_idx ON articles(status,published_at DESC,id DESC);
CREATE INDEX articles_category_idx ON articles(category_id,id);
CREATE INDEX articles_author_idx ON articles(author_user_id,id);
CREATE INDEX articles_updated_idx ON articles(updated_at DESC,id DESC);

ALTER TABLE security_events DROP CONSTRAINT security_events_target_type_check;
ALTER TABLE security_events ADD CONSTRAINT security_events_target_type_check
    CHECK (target_type IN ('user','role','menu','setting','media','article','article_category'));
ALTER TABLE menus DROP CONSTRAINT menus_component_key_check;
ALTER TABLE menus ADD CONSTRAINT menus_component_key_check
    CHECK (component_key IS NULL OR component_key IN
        ('dashboard','users','roles','permissions','menus','settings','media','audit_logs','login_logs','articles','article_categories'));

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM permissions WHERE code IN
        ('articles:view','articles:create','articles:update','articles:delete','articles:publish',
         'article_categories:view','article_categories:create','article_categories:update','article_categories:delete')) THEN
        RAISE EXCEPTION 'articles permission code already exists; refusing ambiguous seed ownership';
    END IF;
    IF EXISTS (SELECT 1 FROM menus WHERE code IN
        ('demo_articles_directory','demo_articles_page','demo_article_categories_page')) THEN
        RAISE EXCEPTION 'articles menu code collision';
    END IF;
END $$;

INSERT INTO permissions(code,name,description) VALUES
    ('articles:view','View articles','Read articles'),
    ('articles:create','Create articles','Create article drafts'),
    ('articles:update','Update articles','Edit article content'),
    ('articles:delete','Delete articles','Delete article drafts'),
    ('articles:publish','Publish articles','Publish and unpublish articles'),
    ('article_categories:view','View article categories','Read article categories'),
    ('article_categories:create','Create article categories','Create article categories'),
    ('article_categories:update','Update article categories','Edit article categories'),
    ('article_categories:delete','Delete article categories','Delete unused article categories');

INSERT INTO menus(code,name,path,icon,sort_order,visible,status,menu_type,component_key,is_system,permission_id)
VALUES ('demo_articles_directory','Articles','','Folder',100,true,'active','directory',NULL,true,NULL);
INSERT INTO menus(code,name,path,icon,sort_order,visible,status,menu_type,component_key,is_system,parent_id,permission_id)
VALUES
    ('demo_articles_page','Articles','/admin/articles','FileText',10,true,'active','page','articles',true,
     (SELECT id FROM menus WHERE code='demo_articles_directory'),(SELECT id FROM permissions WHERE code='articles:view')),
    ('demo_article_categories_page','Categories','/admin/articles/categories','Folder',20,true,'active','page','article_categories',true,
     (SELECT id FROM menus WHERE code='demo_articles_directory'),(SELECT id FROM permissions WHERE code='article_categories:view'));

DO $$
DECLARE
    updated_rows integer;
BEGIN
    UPDATE app_metadata SET schema_version=9,updated_at=CURRENT_TIMESTAMP
    WHERE component='core' AND schema_version=8;
    GET DIAGNOSTICS updated_rows = ROW_COUNT;
    IF updated_rows <> 1 THEN
        RAISE EXCEPTION 'articles migration failed to update exactly one core metadata row';
    END IF;
END $$;
COMMIT;
