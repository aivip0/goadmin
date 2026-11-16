import { expect, it } from 'vitest';
import Ajv from 'ajv-draft-04';
import { openapiV3 } from '@apidevtools/openapi-schemas';
import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';

type OpenAPIDocument = { paths?: Record<string, { get?: { responses?: Record<string, unknown> } }> };

it('validates the complete OpenAPI contract', () => {
  const document = JSON.parse(readFileSync(fileURLToPath(new URL('../../backend/api/openapi.json', import.meta.url)), 'utf8')) as OpenAPIDocument;
  const validate = new Ajv({ allErrors: true, strict: false }).compile(openapiV3);
  expect(validate(document), JSON.stringify(validate.errors)).toBe(true);
  expect(Object.keys(document.paths ?? {}).sort()).toEqual([
    '/api/v1/admin/api-keys',
    '/api/v1/admin/api-keys/{id}/revoke',
    '/api/v1/admin/article-categories',
    '/api/v1/admin/article-categories/{id}',
    '/api/v1/admin/articles',
    '/api/v1/admin/articles/{id}',
    '/api/v1/admin/articles/{id}/publish',
    '/api/v1/admin/articles/{id}/unpublish',
    '/api/v1/admin/audit-logs',
    '/api/v1/admin/audit-logs/{id}',
    '/api/v1/admin/dashboard/activity',
    '/api/v1/admin/dashboard/summary',
    '/api/v1/admin/dashboard/trends',
    '/api/v1/admin/jobs',
    '/api/v1/admin/jobs/{id}',
    '/api/v1/admin/jobs/{id}/retry',
    '/api/v1/admin/login-logs',
    '/api/v1/admin/login-logs/{id}',
    '/api/v1/admin/media',
    '/api/v1/admin/media/{id}',
    '/api/v1/admin/media/{id}/content',
    '/api/v1/admin/notifications',
    '/api/v1/admin/notifications/{id}/read',
    '/api/v1/admin/plugins',
    '/api/v1/admin/plugins/events',
    '/api/v1/admin/plugins/install',
    '/api/v1/admin/plugins/packages',
    '/api/v1/admin/plugins/{id}/config',
    '/api/v1/admin/plugins/{id}/disable',
    '/api/v1/admin/plugins/{id}/enable',
    '/api/v1/admin/plugins/{id}/routes/{key}',
    '/api/v1/admin/plugins/{id}/uninstall',
    '/api/v1/admin/plugins/{id}/upgrade',
    '/api/v1/admin/profile',
    '/api/v1/admin/profile/password',
    '/api/v1/admin/profile/sessions',
    '/api/v1/admin/profile/sessions/revoke-others',
    '/api/v1/admin/profile/sessions/{id}',
    '/api/v1/admin/settings',
    '/api/v1/auth/csrf',
    '/api/v1/auth/login',
    '/api/v1/auth/logout',
    '/api/v1/auth/logout-all',
    '/api/v1/auth/me',
    '/api/v1/health',
    '/api/v1/health/live',
    '/api/v1/health/ready',
    '/api/v1/key/dashboard/summary',
    '/api/v1/openapi.json',
  ]);
  expect(document.paths?.['/api/v1/auth/csrf']?.get?.responses).toHaveProperty('200');
});
