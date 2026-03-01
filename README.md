# Page Patrol

Page Patrol is a lightweight team book-club tracker with passwordless magic-link login.

## Features

- Passwordless magic-link login (no passwords)
- Invite-only allow list
- One active weekly target at a time
- Member read/unread toggle ("I read!")
- Email notifications to all members except actor
- Live activity feed via SSE (with polling fallback)
- Coordinator dashboard for target publishing, trends, and allow-list management

## Stack

- Go + self-hosted HTMX (server-rendered)
- PostgreSQL
- SMTP relay (Mailpit locally, Resend in production)
- Docker Compose overlays for local and production
- Cloudflare Tunnel for production ingress

## Deployment Modes

### Local mode (localhost + Mailpit)

1. Create local env file.

```bash
cp .env.local.example .env.local
```

2. Start local stack.

```bash
./scripts/local-up.sh
```

3. Open:
- App: http://localhost:3000
- Mailpit UI: http://localhost:8025

4. Stop local stack.

```bash
./scripts/local-down.sh
```

Local mode uses:
- `docker-compose.yml` (shared services)
- `docker-compose.local.yml` (build from source, Caddy, Mailpit)

### Production mode (custom domain + Resend + Cloudflare Tunnel)

1. Create production env file.

```bash
cp .env.prod.example .env.prod
```

2. Update at least these values in `.env.prod`:
- `APP_IMAGE`
- `APP_TAG`
- `TUNNEL_TOKEN`
- `APP_BASE_URL`
- `COORDINATOR_EMAIL`
- Resend SMTP settings (`SMTP_*`)

3. Deploy pulled image tag.

```bash
./scripts/prod-deploy.sh
```

Production mode uses:
- `docker-compose.yml` (shared services)
- `docker-compose.prod.yml` (pulled images + cloudflared)

### Raspberry Pi production mode

1. Create the Pi env file outside the repo checkout.

```bash
./scripts/pi-init-env.sh
```

2. Edit `$HOME/.config/page-patrol/env.pi` and update at least:
- `APP_IMAGE`
- `APP_TAG`
- `TUNNEL_TOKEN`
- `APP_BASE_URL`
- `COORDINATOR_EMAIL`
- Resend SMTP settings (`SMTP_*`)

3. Deploy the pulled image tag on the Pi.

```bash
./scripts/pi-deploy.sh
```

Pi mode uses:
- `docker-compose.yml` (shared services)
- `docker-compose.pi.yml` (pulled images + host-network cloudflared)

## Coordinator Setup

- `COORDINATOR_EMAIL` becomes/keeps `coordinator` role on sign-in.
- Coordinator can manage the allow list at `/coordinator/members`.
- Coordinator can publish weekly targets at `/coordinator`.

## Command Matrix

- Validate both compose overlays: `./scripts/compose-validate.sh`
- Start local: `./scripts/local-up.sh`
- Stop local: `./scripts/local-down.sh`
- Deploy production tag: `./scripts/prod-deploy.sh`
- Initialize Pi env file: `./scripts/pi-init-env.sh`
- Deploy on Raspberry Pi: `./scripts/pi-deploy.sh`
- Prune old Docker build cache on the Pi after secret rotation: `./scripts/pi-prune-builder-cache.sh`
- Apply Pi host hardening (requires `sudo` and `SSH_ALLOW_CIDR`): `./scripts/pi-host-harden.sh`
- Run tests: `go test ./...`

## Easy Build/Release Flow

A GitHub Actions workflow publishes multi-arch images to GHCR:
- Workflow: `.github/workflows/publish-image.yml`
- Triggers: pushes to `main`, any tag push, and manual dispatch
- Platforms: `linux/amd64`, `linux/arm64`
- Tags: `main`, commit SHA, and release tag (when applicable)

Production updates are done by setting `APP_TAG` in `.env.prod` and re-running `./scripts/prod-deploy.sh`.

## Rollback

1. Set `APP_TAG` in `.env.prod` to a previously known-good tag.
2. Re-run:

```bash
./scripts/prod-deploy.sh
```

3. Verify health:

```bash
curl -fsS https://<your-domain>/healthz
```

## Environment Files

- `.env.local.example`: local defaults for localhost + Mailpit
- `.env.prod.example`: production defaults for cloudflared + Resend SMTP
- `.env.pi.example`: Pi production defaults for an external host env file
- `.env.example`: pointer file showing which mode-specific env file to copy

## Backup and Restore

### Local mode

```bash
docker compose --env-file .env.local -f docker-compose.yml -f docker-compose.local.yml exec -T postgres \
  pg_dump -U page_patrol page_patrol > backup.sql
```

```bash
cat backup.sql | docker compose --env-file .env.local -f docker-compose.yml -f docker-compose.local.yml exec -T postgres \
  psql -U page_patrol -d page_patrol
```

### Production mode

```bash
docker compose --env-file .env.prod -f docker-compose.yml -f docker-compose.prod.yml exec -T postgres \
  pg_dump -U page_patrol page_patrol > backup.sql
```

```bash
cat backup.sql | docker compose --env-file .env.prod -f docker-compose.yml -f docker-compose.prod.yml exec -T postgres \
  psql -U page_patrol -d page_patrol
```

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

## Security Defaults

- Session and magic-link tokens are stored hashed only.
- Magic links expire (default 15 minutes) and are single-use.
- Session cookie is `HttpOnly`, `SameSite=Lax`, and `Secure` when `COOKIE_SECURE=true`.
- CSRF token is required for mutating POST routes.
- Browser responses include CSP, `X-Frame-Options: DENY`, `Referrer-Policy`, `Permissions-Policy`, and HSTS when `APP_BASE_URL` is HTTPS.
- Set `CSP_ALLOW_UNSAFE_INLINE=true` only when an upstream service such as Cloudflare Bot Fight Mode injects inline JavaScript that must be allowed.
- Magic-link requests are rate-limited in-process by IP and email with a resend cooldown.
- `TRUST_PROXY_HEADERS=true` is required only when the app is behind a trusted proxy or Cloudflare Tunnel.

## Cloudflare Follow-Up

- Disable Cloudflare JavaScript challenge injection for the app hostname before enforcing the strict CSP in production.
- Add a Cloudflare rate-limiting rule for `POST /auth/request-link` to match the in-app throttling.
