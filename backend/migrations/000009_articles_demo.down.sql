BEGIN;

DO $$
DECLARE
    current_version integer;
BEGIN
    SELECT schema_version INTO current_version
    FROM app_metadata WHERE component='core' FOR UPDATE;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'articles rollback requires core metadata row';
    END IF;
    IF current_version IS DISTINCT FROM 9 THEN
        RAISE EXCEPTION 'articles rollback requires schema version 9';
    END IF;
END $$;

LOCK TABLE articles, article_categories, security_events, menus, role_menus, role_permissions, permissions IN ACCESS EXCLUSIVE MODE;

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM articles) OR EXISTS (SELECT 1 FROM article_categories)
       OR EXISTS (SELECT 1 FROM security_events WHERE target_type IN ('article','article_category'))
       OR EXISTS (SELECT 1 FROM role_permissions rp JOIN permissions p ON p.id=rp.permission_id
                  WHERE p.code IN ('articles:view','articles:create','articles:update','articles:delete','articles:publish',
                                   'article_categories:view','article_categories:create','article_categories:update','article_categories:delete'))
       OR EXISTS (SELECT 1 FROM role_menus rm JOIN menus m ON m.id=rm.menu_id
                  WHERE m.code IN ('demo_articles_directory','demo_articles_page','demo_article_categories_page'))
       OR EXISTS (SELECT 1 FROM menus m JOIN permissions p ON p.id=m.permission_id
                  WHERE p.code IN ('articles:view','articles:create','articles:update','articles:delete','articles:publish',
                                   'article_categories:view','article_categories:create','article_categories:update','article_categories:delete')
                    AND m.code NOT IN ('demo_articles_page','demo_article_categories_page'))
       OR EXISTS (SELECT 1 FROM menus child JOIN menus parent ON parent.id=child.parent_id
                  WHERE parent.code='demo_articles_directory'
                    AND child.code NOT IN ('demo_articles_page','demo_article_categories_page')) THEN
        RAISE EXCEPTION 'articles data, audit history or grants exist; refusing migration rollback';
    END IF;
    IF EXISTS (
        SELECT 1 FROM (VALUES
            ('articles:view','View articles','Read articles'),
            ('articles:create','Create articles','Create article drafts'),
            ('articles:update','Update articles','Edit article content'),
            ('articles:delete','Delete articles','Delete article drafts'),
            ('articles:publish','Publish articles','Publish and unpublish articles'),
            ('article_categories:view','View article categories','Read article categories'),
            ('article_categories:create','Create article categories','Create article categories'),
            ('article_categories:update','Update article categories','Edit article categories'),
            ('article_categories:delete','Delete article categories','Delete unused article categories')
        ) AS seed(code,name,description)
        LEFT JOIN permissions p ON p.code=seed.code
        WHERE p.id IS NULL OR p.name IS DISTINCT FROM seed.name
           OR p.description IS DISTINCT FROM seed.description
    ) THEN
        RAISE EXCEPTION 'articles permission seed changed; refusing migration rollback';
    END IF;
    IF (SELECT count(*) FROM menus WHERE code IN
        ('demo_articles_directory','demo_articles_page','demo_article_categories_page')) <> 3
       OR EXISTS (SELECT 1 FROM menus m LEFT JOIN menus parent ON parent.id=m.parent_id
                  LEFT JOIN permissions p ON p.id=m.permission_id
                  WHERE m.code IN ('demo_articles_directory','demo_articles_page','demo_article_categories_page')
                    AND ((
                      m.is_system AND m.visible AND m.status='active' AND
                      ((m.code='demo_articles_directory' AND m.parent_id IS NULL AND m.name='Articles'
                        AND m.path='' AND m.icon='Folder' AND m.sort_order=100 AND m.menu_type='directory'
                        AND m.component_key IS NULL AND m.permission_id IS NULL)
                       OR (m.code='demo_articles_page' AND parent.code='demo_articles_directory'
                        AND m.name='Articles' AND m.path='/admin/articles' AND m.icon='FileText'
                        AND m.sort_order=10 AND m.menu_type='page' AND m.component_key='articles' AND p.code='articles:view')
                       OR (m.code='demo_article_categories_page' AND parent.code='demo_articles_directory'
                        AND m.name='Categories' AND m.path='/admin/articles/categories' AND m.icon='Folder'
                        AND m.sort_order=20 AND m.menu_type='page' AND m.component_key='article_categories'
                        AND p.code='article_categories:view'))
                    ) IS NOT TRUE)) THEN
        RAISE EXCEPTION 'articles menu seed changed; refusing migration rollback';
    END IF;
END $$;

DELETE FROM menus WHERE code IN ('demo_articles_page','demo_article_categories_page');
DELETE FROM menus WHERE code='demo_articles_directory';
DELETE FROM permissions WHERE code IN
    ('articles:view','articles:create','articles:update','articles:delete','articles:publish',
     'article_categories:view','article_categories:create','article_categories:update','article_categories:delete');
ALTER TABLE menus DROP CONSTRAINT menus_component_key_check;
ALTER TABLE menus ADD CONSTRAINT menus_component_key_check
    CHECK (component_key IS NULL OR component_key IN
        ('dashboard','users','roles','permissions','menus','settings','media','audit_logs','login_logs'));
ALTER TABLE security_events DROP CONSTRAINT security_events_target_type_check;
ALTER TABLE security_events ADD CONSTRAINT security_events_target_type_check
    CHECK (target_type IN ('user','role','menu','setting','media'));
DROP TABLE articles;
DROP TABLE article_categories;
DO $$
DECLARE
    updated_rows integer;
BEGIN
    UPDATE app_metadata SET schema_version=8,updated_at=CURRENT_TIMESTAMP
    WHERE component='core' AND schema_version=9;
    GET DIAGNOSTICS updated_rows = ROW_COUNT;
    IF updated_rows <> 1 THEN
        RAISE EXCEPTION 'articles rollback failed to update exactly one core metadata row';
    END IF;
END $$;
COMMIT;
