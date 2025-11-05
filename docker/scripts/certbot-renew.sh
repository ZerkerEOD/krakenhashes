#!/bin/sh
# Certificate renewal script for KrakenHashes with certbot

set -e

# Load environment variables
. /etc/krakenhashes/.env

# Check if certbot mode is enabled
if [ "${KH_TLS_MODE}" != "certbot" ]; then
    echo "Certbot mode is not enabled (current mode: ${KH_TLS_MODE})"
    exit 0
fi

# Check required variables
if [ -z "${KH_CERTBOT_DOMAIN}" ] || [ -z "${KH_CERTBOT_EMAIL}" ]; then
    echo "ERROR: KH_CERTBOT_DOMAIN and KH_CERTBOT_EMAIL must be set"
    exit 1
fi

# Check if Cloudflare token is set
if [ -z "${CLOUDFLARE_API_TOKEN}" ]; then
    echo "ERROR: CLOUDFLARE_API_TOKEN must be set"
    exit 1
fi

echo "Checking certificates for renewal..."

# Prepare certbot arguments
CERTBOT_ARGS="renew --non-interactive --dns-cloudflare"
CERTBOT_ARGS="${CERTBOT_ARGS} --dns-cloudflare-credentials /etc/krakenhashes/certs/cloudflare.ini"
CERTBOT_ARGS="${CERTBOT_ARGS} --config-dir /etc/krakenhashes/certs"
CERTBOT_ARGS="${CERTBOT_ARGS} --work-dir /etc/krakenhashes/certs/work"
CERTBOT_ARGS="${CERTBOT_ARGS} --logs-dir /etc/krakenhashes/certs/logs"

# Add custom ACME server if specified
if [ -n "${KH_CERTBOT_SERVER}" ]; then
    CERTBOT_ARGS="${CERTBOT_ARGS} --server ${KH_CERTBOT_SERVER}"
fi

# Add staging flag if configured (only if no custom server specified)
if [ "${KH_CERTBOT_STAGING}" = "true" ] && [ -z "${KH_CERTBOT_SERVER}" ]; then
    CERTBOT_ARGS="${CERTBOT_ARGS} --staging"
fi

# Add extra arguments if specified
if [ -n "${KH_CERTBOT_EXTRA_ARGS}" ]; then
    CERTBOT_ARGS="${CERTBOT_ARGS} ${KH_CERTBOT_EXTRA_ARGS}"
fi

# Add post-hook to reload services
CERTBOT_ARGS="${CERTBOT_ARGS} --deploy-hook '/usr/local/bin/certbot-deploy-hook.sh'"

# Run renewal
certbot ${CERTBOT_ARGS}

echo "Certificate renewal check completed"