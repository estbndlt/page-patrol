#!/usr/bin/env bash
set -euo pipefail

PI_ENV_FILE=${PI_ENV_FILE:-$HOME/.config/page-patrol/env.pi}
SKIP_MANAGED_CLOUDFLARED=${SKIP_MANAGED_CLOUDFLARED:-no}
LOCAL_BUILD=${LOCAL_BUILD:-no}
LOCAL_APP_IMAGE=${LOCAL_APP_IMAGE:-page-patrol-local}
LOCAL_APP_TAG=${LOCAL_APP_TAG:-latest}
if [[ ! -f "${PI_ENV_FILE}" ]]; then
  echo "Missing Pi env file: ${PI_ENV_FILE}" >&2
  echo "Create it with ./scripts/pi-init-env.sh or set PI_ENV_FILE to an existing file." >&2
  exit 1
fi

export PI_ENV_FILE

services=()
if [[ "${SKIP_MANAGED_CLOUDFLARED}" == "yes" ]]; then
  export TUNNEL_TOKEN=${TUNNEL_TOKEN:-unused}
  services=(postgres web worker)
fi

if [[ "${LOCAL_BUILD}" == "yes" ]]; then
  docker build -t "${LOCAL_APP_IMAGE}:${LOCAL_APP_TAG}" .

  APP_IMAGE="${LOCAL_APP_IMAGE}" APP_TAG="${LOCAL_APP_TAG}" docker compose \
    --env-file "${PI_ENV_FILE}" \
    -f docker-compose.yml \
    -f docker-compose.pi.yml \
    up -d --remove-orphans --pull never "${services[@]}"
  exit 0
fi

docker compose \
  --env-file "${PI_ENV_FILE}" \
  -f docker-compose.yml \
  -f docker-compose.pi.yml \
  pull "${services[@]}"

docker compose \
  --env-file "${PI_ENV_FILE}" \
  -f docker-compose.yml \
  -f docker-compose.pi.yml \
  up -d --remove-orphans "${services[@]}"
