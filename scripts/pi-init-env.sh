#!/usr/bin/env bash
set -euo pipefail

PI_ENV_FILE=${PI_ENV_FILE:-$HOME/.config/page-patrol/env.pi}
PI_ENV_DIR=$(dirname "${PI_ENV_FILE}")

if [[ -e "${PI_ENV_FILE}" ]]; then
  echo "Refusing to overwrite existing env file: ${PI_ENV_FILE}" >&2
  exit 1
fi

if ! command -v openssl >/dev/null 2>&1; then
  echo "openssl is required to generate a database password." >&2
  exit 1
fi

mkdir -p "${PI_ENV_DIR}"
chmod 700 "${PI_ENV_DIR}"

db_password=$(openssl rand -hex 24)
sed "s/^POSTGRES_PASSWORD=.*/POSTGRES_PASSWORD=${db_password}/" .env.pi.example >"${PI_ENV_FILE}"
chmod 600 "${PI_ENV_FILE}"

cat <<EOF
Created ${PI_ENV_FILE}

Next steps:
1. Edit APP_IMAGE, APP_TAG, TUNNEL_TOKEN, COORDINATOR_EMAIL, and SMTP_*.
2. Rotate any SMTP credential that was previously stored inside the repo checkout.
3. Run ./scripts/pi-deploy.sh
EOF
