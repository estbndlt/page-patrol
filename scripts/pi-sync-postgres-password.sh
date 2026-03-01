#!/usr/bin/env bash
set -euo pipefail

PI_ENV_FILE=${PI_ENV_FILE:-$HOME/.config/page-patrol/env.pi}
if [[ ! -f "${PI_ENV_FILE}" ]]; then
  echo "Missing Pi env file: ${PI_ENV_FILE}" >&2
  exit 1
fi

# shellcheck disable=SC1090
set -a
. "${PI_ENV_FILE}"
set +a

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
