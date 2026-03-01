#!/usr/bin/env bash
set -euo pipefail

docker compose \
  --env-file .env.pi \
  -f docker-compose.yml \
  -f docker-compose.pi.yml \
  up -d --build --remove-orphans
