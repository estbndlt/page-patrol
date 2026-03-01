#!/usr/bin/env bash
set -euo pipefail

cleanup_local=0
cleanup_prod=0

if [[ ! -f .env.local ]]; then
  cp .env.local.example .env.local
  cleanup_local=1
fi

if [[ ! -f .env.prod ]]; then
  cp .env.prod.example .env.prod
  cleanup_prod=1
fi

cleanup() {
  if [[ "${cleanup_local}" -eq 1 ]]; then
    rm -f .env.local
  fi
  if [[ "${cleanup_prod}" -eq 1 ]]; then
    rm -f .env.prod
  fi
}
trap cleanup EXIT

docker compose \
  --env-file .env.local \
  -f docker-compose.yml \
  -f docker-compose.local.yml \
  config >/dev/null
echo "Local compose config is valid."

docker compose \
  --env-file .env.prod \
  -f docker-compose.yml \
  -f docker-compose.prod.yml \
  config >/dev/null
echo "Production compose config is valid."
