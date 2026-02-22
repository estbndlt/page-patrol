# Page Patrol

Page Patrol is a lightweight team book-club tracker with passwordless email login.

## Features

- Passwordless magic-link login (no passwords)
- Invite-only allow list
- One active weekly target at a time
- Member read/unread toggle ("I read!")
- Instant email notifications to all members except actor
- Live activity feed via SSE (with polling fallback)
- Coordinator dashboard for target publishing, trends, and allow-list management

## Stack

- Go + HTMX (server-rendered)
- PostgreSQL
- SMTP relay
- Docker Compose + Caddy (HTTPS)

## Quick Start (Docker Compose)

1. Create and edit environment file.

```bash
cp .env.example .env
```

2. Update `.env` values:
- `APP_DOMAIN`
- `APP_BASE_URL`
- `COORDINATOR_EMAIL`
- SMTP settings

3. Start services.

```bash
docker compose up --build -d
```

4. Open `https://<APP_DOMAIN>` and request a magic link with an allow-listed email.

## Coordinator Setup

- `COORDINATOR_EMAIL` becomes/keeps `coordinator` role on sign-in.
- Coordinator can manage the allow list at `/coordinator/members`.
- Coordinator can publish weekly targets at `/coordinator`.

## Core Routes

- `GET /login`
- `POST /auth/request-link`
- `GET /auth/verify?token=...`
- `POST /auth/logout`
- `GET /`
- `POST /status/toggle`
- `GET /feed/events`
- `POST /coordinator/targets`
- `POST /coordinator/members`
- `POST /coordinator/members/:id/deactivate`
- `POST /coordinator/members/:id/reactivate`
- `GET /coordinator/trends`

## Raspberry Pi Notes

- The Dockerfile uses `TARGETARCH=arm64` by default.
- On Raspberry Pi OS 64-bit, Docker Compose should run directly.
- Persist volumes (`pg_data`, `caddy_data`) and include them in backups.

## Security Defaults

- Session and magic-link tokens are stored hashed only.
- Magic links expire (default 15 minutes) and are single-use.
- Session cookie is `HttpOnly`, `SameSite=Lax`, and `Secure` when `COOKIE_SECURE=true`.
- CSRF token is required for mutating POST routes.

## Backup and Restore

### Backup database

```bash
docker compose exec -T postgres pg_dump -U page_patrol page_patrol > backup.sql
```

### Restore database

```bash
cat backup.sql | docker compose exec -T postgres psql -U page_patrol -d page_patrol
```
