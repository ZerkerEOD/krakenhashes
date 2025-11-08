#!/bin/sh
set -e

# Set default PUID/PGID if not provided
PUID=${PUID:-1000}
PGID=${PGID:-1000}

echo "Starting with UID: $PUID and GID: $PGID"

# Update krakenhashes user and group IDs
if [ "$PUID" != "1000" ] || [ "$PGID" != "1000" ]; then
    echo "Updating krakenhashes user/group to UID: $PUID, GID: $PGID"
    
    # Update group ID
    if [ "$PGID" != "1000" ]; then
        groupmod -g "$PGID" krakenhashes
    fi
    
    # Update user ID
    if [ "$PUID" != "1000" ]; then
        usermod -u "$PUID" krakenhashes
    fi
fi

# Fix ownership of directories that krakenhashes user needs to access
echo "Fixing ownership of directories..."
chown -R krakenhashes:krakenhashes /var/lib/krakenhashes || true
chown -R krakenhashes:krakenhashes /var/log/krakenhashes/backend || true
chown -R krakenhashes:krakenhashes /home/krakenhashes || true

# Fix ownership of config directory but keep certs readable
chown -R krakenhashes:krakenhashes /etc/krakenhashes || true
# Ensure the certificates directory exists and has proper permissions
mkdir -p /etc/krakenhashes/certs
chown krakenhashes:krakenhashes /etc/krakenhashes/certs
chmod 755 /etc/krakenhashes/certs

# If certificates already exist, ensure they are readable
if [ -n "$(ls -A /etc/krakenhashes/certs 2>/dev/null)" ]; then
    echo "Making existing certificates readable..."
    find /etc/krakenhashes/certs -type f -exec chmod 644 {} \;
    find /etc/krakenhashes/certs -type f -exec chown krakenhashes:krakenhashes {} \;
fi

# Install custom CA certificate if specified (for internal ACME servers)
if [ -n "$KH_CERTBOT_CUSTOM_CA_CERT" ] && [ -f "$KH_CERTBOT_CUSTOM_CA_CERT" ]; then
    echo "Installing custom CA certificate from: $KH_CERTBOT_CUSTOM_CA_CERT"
    cp "$KH_CERTBOT_CUSTOM_CA_CERT" /usr/local/share/ca-certificates/krakenhashes-custom-ca.crt
    chmod 644 /usr/local/share/ca-certificates/krakenhashes-custom-ca.crt
    update-ca-certificates
    echo "Custom CA certificate installed successfully"
elif [ -n "$KH_CERTBOT_CUSTOM_CA_CERT" ]; then
    echo "WARNING: KH_CERTBOT_CUSTOM_CA_CERT is set but file not found: $KH_CERTBOT_CUSTOM_CA_CERT"
fi

# Create required log directories
for dir in backend nginx; do
    mkdir -p "/var/log/krakenhashes/$dir"
done

# Set permissions for log paths
chmod -R 755 "/var/log/krakenhashes"
chown -R nginx:nginx "/var/log/krakenhashes/nginx"
# Backend logs should remain owned by krakenhashes (already set above)

# Create backend .env file
cat > /etc/krakenhashes/.env << EOF
# Server Configuration
KH_HTTP_PORT=${KH_HTTP_PORT:-1337}
KH_HOST=${KH_HOST:-0.0.0.0}
KH_HTTPS_PORT=${KH_HTTPS_PORT:-31337}
KH_IN_DOCKER=TRUE

# Get container's hostname and IP
KH_ADDITIONAL_DNS_NAMES=localhost,$(hostname)
KH_ADDITIONAL_IP_ADDRESSES=127.0.0.1,0.0.0.0,$(hostname -i)

# Database Configuration - External PostgreSQL
DB_HOST=${DB_HOST:-postgres}
DB_PORT=${DB_PORT:-5432}
DB_NAME=${DB_NAME}
DB_USER=${DB_USER}
DB_PASSWORD=${DB_PASSWORD}

# Build connection string for external database
DATABASE_URL=postgres://${DB_USER}:${DB_PASSWORD}@${DB_HOST}:${DB_PORT}/${DB_NAME}?sslmode=disable

# JWT Configuration
JWT_SECRET=${JWT_SECRET:-default_jwt_secret_replace_in_production}
JWT_EXPIRATION=${JWT_EXPIRATION:-24h}

# TLS Configuration
TLS_MODE=${TLS_MODE:-self-signed}
KH_TLS_MODE=${KH_TLS_MODE:-self-signed}
TLS_CERT_FILE=${TLS_CERT_FILE:-/etc/krakenhashes/certs/cert.pem}
TLS_KEY_FILE=${TLS_KEY_FILE:-/etc/krakenhashes/certs/key.pem}

# CORS Configuration
ALLOWED_ORIGINS=${ALLOWED_ORIGINS:-*}

# Logging Configuration
DEBUG=${DEBUG:-true}
LOG_LEVEL=${LOG_LEVEL:-DEBUG}

# Version Information
VERSION=${VERSION}
EOF

# Make sure the .env file is readable by krakenhashes
chown krakenhashes:krakenhashes /etc/krakenhashes/.env
chmod 644 /etc/krakenhashes/.env

# Wait for PostgreSQL to be ready
echo "Waiting for PostgreSQL at ${DB_HOST}:${DB_PORT}..."
until curl -s --connect-timeout 2 ${DB_HOST}:${DB_PORT} >/dev/null 2>&1 || [ $? -eq 52 ]; do
    echo "PostgreSQL is unavailable - sleeping"
    sleep 2
done
echo "PostgreSQL is up - executing command"

# Print environment variables for debugging
echo "Environment variables:"
echo "KH_IN_DOCKER=${KH_IN_DOCKER}"
echo "KH_HOST=${KH_HOST}"
echo "KH_HTTP_PORT=${KH_HTTP_PORT}"
echo "KH_HTTPS_PORT=${KH_HTTPS_PORT}"
echo "DB_HOST=${DB_HOST}"
echo "DB_PORT=${DB_PORT}"
echo "DB_NAME=${DB_NAME}"
echo "KH_TLS_MODE=${KH_TLS_MODE}"

# Configure nginx based on TLS mode
echo "Configuring nginx for TLS mode: ${KH_TLS_MODE}"

# Ensure nginx conf.d directory exists
mkdir -p /etc/nginx/conf.d

# Remove any existing configs
rm -f /etc/nginx/conf.d/default*.conf

# Set default nginx error log level if not provided
NGINX_ERROR_LOG_LEVEL=${NGINX_ERROR_LOG_LEVEL:-warn}
echo "Setting nginx error log level to: ${NGINX_ERROR_LOG_LEVEL}"

case "${KH_TLS_MODE}" in
    "certbot")
        if [ -n "${KH_CERTBOT_DOMAIN}" ]; then
            echo "Configuring nginx for certbot mode with domain: ${KH_CERTBOT_DOMAIN}"
            sed -e "s/CERTBOT_DOMAIN/${KH_CERTBOT_DOMAIN}/g" \
                -e "s/\${NGINX_ERROR_LOG_LEVEL}/${NGINX_ERROR_LOG_LEVEL}/g" \
                /etc/nginx/templates/certbot.conf > /etc/nginx/conf.d/default.conf

            # Create webroot directory for ACME challenges
            mkdir -p /var/www/certbot
            chown -R www-data:www-data /var/www/certbot
            chmod 755 /var/www/certbot
            echo "Created webroot directory: /var/www/certbot"
        else
            echo "ERROR: KH_CERTBOT_DOMAIN not set for certbot mode"
            exit 1
        fi
        ;;
    "provided")
        echo "Configuring nginx for user-provided certificates"
        sed "s/\${NGINX_ERROR_LOG_LEVEL}/${NGINX_ERROR_LOG_LEVEL}/g" \
            /etc/nginx/templates/provided.conf > /etc/nginx/conf.d/default.conf
        ;;
    *)
        # Default to self-signed
        echo "Configuring nginx for self-signed certificates"
        sed "s/\${NGINX_ERROR_LOG_LEVEL}/${NGINX_ERROR_LOG_LEVEL}/g" \
            /etc/nginx/templates/self-signed.conf > /etc/nginx/conf.d/default.conf
        ;;
esac

echo "Nginx configuration complete"

echo "Starting supervisord..."
exec "$@"