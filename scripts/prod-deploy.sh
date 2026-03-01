#!/usr/bin/env bash
set -euo pipefail

PROD_ENV_FILE=${PROD_ENV_FILE:-.env.prod}
if [[ ! -f "${PROD_ENV_FILE}" ]]; then
  echo "Missing production env file: ${PROD_ENV_FILE}" >&2
  exit 1
fi

export PROD_ENV_FILE

docker compose \
  --env-file "${PROD_ENV_FILE}" \
  -f docker-compose.yml \
  -f docker-compose.prod.yml \
  pull

docker compose \
  --env-file "${PROD_ENV_FILE}" \
  -f docker-compose.yml \
  -f docker-compose.prod.yml \
  up -d --remove-orphans
