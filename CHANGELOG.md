# Changelog

## [0.1.2] - 2026-11-16

- Resolve the latest stable template through GitHub's stable-release redirect,
  avoiding dependence on the unauthenticated GitHub API rate limit.
- Validate that the redirect ends at this repository's stable semantic-version tag.

## [0.1.1] - 2026-11-16

- Accept GitHub release tarballs containing a PAX global metadata header while
  continuing to reject links, special files and unsafe archive paths.
- Validate the generator against the actual GitHub release archive.

## [0.1.0] - 2026-11-16

- Initial public GoAdmin Kit starter baseline.
- Added Go + Gin backend, PostgreSQL migrations 1–13 and Next.js App Router frontend.
- Included authentication, sessions, RBAC, dynamic menus, settings, media,
  audit/login logs, profile, articles demo, notifications, API keys, jobs and
  the trusted local plugin system.
- Added the `go-admin` project generator and environment doctor.

The initial release does not include an online plugin marketplace, arbitrary
code execution, or a production deployment approval.
