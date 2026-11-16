BEGIN;
DROP TABLE auth_login_limits;
UPDATE app_metadata SET schema_version=2, updated_at=CURRENT_TIMESTAMP WHERE component='core';
COMMIT;
