#!/usr/bin/env bash
set -euo pipefail

docker compose \
  --env-file .env.prod \
  -f docker-compose.yml \
  -f docker-compose.prod.yml \
  pull

docker compose \
  --env-file .env.prod \
  -f docker-compose.yml \
  -f docker-compose.prod.yml \
  up -d --remove-orphans
