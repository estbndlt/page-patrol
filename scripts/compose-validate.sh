#!/usr/bin/env bash
set -euo pipefail

cleanup_local=0
cleanup_prod=0
cleanup_pi=0
pi_env_file=""

if [[ ! -f .env.local ]]; then
  cp .env.local.example .env.local
  cleanup_local=1
fi

if [[ ! -f .env.prod ]]; then
  cp .env.prod.example .env.prod
  cleanup_prod=1
fi

pi_env_file="$(mktemp "${TMPDIR:-/tmp}/page-patrol-pi-env.XXXXXX")"
cp .env.pi.example "${pi_env_file}"
cleanup_pi=1

cleanup() {
  if [[ "${cleanup_local}" -eq 1 ]]; then
    rm -f .env.local
  fi
  if [[ "${cleanup_prod}" -eq 1 ]]; then
    rm -f .env.prod
  fi
  if [[ "${cleanup_pi}" -eq 1 ]]; then
    rm -f "${pi_env_file}"
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

PI_ENV_FILE="${pi_env_file}" docker compose \
  --env-file "${pi_env_file}" \
  -f docker-compose.yml \
  -f docker-compose.pi.yml \
  config >/dev/null
echo "Pi compose config is valid."
