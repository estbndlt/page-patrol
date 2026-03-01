#!/usr/bin/env bash
set -euo pipefail

PI_ENV_FILE=${PI_ENV_FILE:-$HOME/.config/page-patrol/env.pi}
if [[ ! -f "${PI_ENV_FILE}" ]]; then
  echo "Missing Pi env file: ${PI_ENV_FILE}" >&2
  exit 1
fi

read_env_var() {
  local key=$1
  local line
  line=$(grep -E "^${key}=" "${PI_ENV_FILE}" | tail -n 1 || true)
  if [[ -z "${line}" ]]; then
    return 1
  fi

  printf '%s\n' "${line#*=}"
}

POSTGRES_DB=$(read_env_var POSTGRES_DB || true)
POSTGRES_USER=$(read_env_var POSTGRES_USER || true)
POSTGRES_PASSWORD=$(read_env_var POSTGRES_PASSWORD || true)

if [[ -z "${POSTGRES_DB:-}" || -z "${POSTGRES_USER:-}" || -z "${POSTGRES_PASSWORD:-}" ]]; then
  echo "POSTGRES_DB, POSTGRES_USER, and POSTGRES_PASSWORD must be set in ${PI_ENV_FILE}." >&2
  exit 1
fi

escaped_password=${POSTGRES_PASSWORD//\'/\'\'}
export TUNNEL_TOKEN=${TUNNEL_TOKEN:-unused}

docker compose \
  --env-file "${PI_ENV_FILE}" \
  -f docker-compose.yml \
  -f docker-compose.pi.yml \
  exec -T postgres \
  psql -U "${POSTGRES_USER}" -d "${POSTGRES_DB}" \
  -c "ALTER ROLE \"${POSTGRES_USER}\" WITH PASSWORD '${escaped_password}';"
