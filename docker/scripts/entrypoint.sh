#!/bin/sh
set -e

# Clean environment variables by removing comments
# Docker Compose sometimes includes comments from .env file
clean_env_var() {
    var_name=$1
    var_value=$(eval echo \$$var_name)
    # Remove everything after the first # (comment)
    cleaned_value=$(echo "$var_value" | sed 's/#.*//' | sed 's/[[:space:]]*$//')
    export $var_name="$cleaned_value"
}

# Clean all potentially affected environment variables
clean_env_var DEBUG
clean_env_var LOG_LEVEL
clean_env_var DEBUG_SQL
clean_env_var DEBUG_HTTP
clean_env_var DEBUG_WEBSOCKET
clean_env_var DEBUG_AUTH
clean_env_var DEBUG_JOBS
clean_env_var JWT_EXPIRATION
clean_env_var KH_TLS_MODE

# Set default PUID/PGID if not provided
PUID=${PUID:-1000}
PGID=${PGID:-1000}

echo "Starting with UID: $PUID and GID: $PGID"
echo "Cleaned environment variables:"
echo "  DEBUG=$DEBUG"
echo "  LOG_LEVEL=$LOG_LEVEL"

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

# Configure nginx based on TLS mode
echo "Configuring nginx for TLS mode: ${KH_TLS_MODE:-self-signed}"

# Ensure nginx conf.d directory exists
mkdir -p /etc/nginx/conf.d

# Remove any existing default configs
rm -f /etc/nginx/conf.d/default*.conf

# Set nginx error log level
NGINX_ERROR_LOG_LEVEL=${NGINX_ERROR_LOG_LEVEL:-warn}

# Select and configure nginx based on TLS mode
case "${KH_TLS_MODE:-self-signed}" in
    "certbot")
        if [ -n "${KH_CERTBOT_DOMAIN}" ]; then
            # Create webroot directory for ACME challenges
            mkdir -p /var/www/certbot
            chown -R www-data:www-data /var/www/certbot
            chmod 755 /var/www/certbot
            echo "Created webroot directory: /var/www/certbot"

            # Check if certificates already exist
            CERT_PATH="/etc/krakenhashes/certs/live/${KH_CERTBOT_DOMAIN}/fullchain.pem"
            if [ -f "$CERT_PATH" ]; then
                echo "Certificates already exist at: $CERT_PATH"
                echo "Using full certbot nginx configuration with HTTPS"
                sed -e "s/CERTBOT_DOMAIN/${KH_CERTBOT_DOMAIN}/g" \
                    -e "s/\${NGINX_ERROR_LOG_LEVEL}/${NGINX_ERROR_LOG_LEVEL}/g" \
                    /etc/nginx/templates/certbot.conf > /etc/nginx/conf.d/default.conf
            else
                echo "Certificates do not exist yet (expected on first boot)"
                echo "Using HTTP-only nginx configuration for certbot initialization"
                echo "Nginx will reload with HTTPS after backend obtains certificates"
                sed "s/\${NGINX_ERROR_LOG_LEVEL}/${NGINX_ERROR_LOG_LEVEL}/g" \
                    /etc/nginx/templates/certbot-init.conf > /etc/nginx/conf.d/default.conf
            fi
        else
            echo "WARNING: KH_CERTBOT_DOMAIN not set for certbot mode, using self-signed"
            sed "s/\${NGINX_ERROR_LOG_LEVEL}/${NGINX_ERROR_LOG_LEVEL}/g" \
                /etc/nginx/templates/self-signed.conf > /etc/nginx/conf.d/default.conf
        fi
        ;;
    "provided")
        echo "Using provided certificates nginx configuration"
        sed "s/\${NGINX_ERROR_LOG_LEVEL}/${NGINX_ERROR_LOG_LEVEL}/g" \
            /etc/nginx/templates/provided.conf > /etc/nginx/conf.d/default.conf
        ;;
    *)
        # Default to self-signed
        echo "Using self-signed certificates nginx configuration"
        sed "s/\${NGINX_ERROR_LOG_LEVEL}/${NGINX_ERROR_LOG_LEVEL}/g" \
            /etc/nginx/templates/self-signed.conf > /etc/nginx/conf.d/default.conf
        ;;
esac

echo "Nginx configuration complete"

# Create required log directories
for dir in backend nginx; do
    mkdir -p "/var/log/krakenhashes/$dir"
done

# Make nginx log directory owned by krakenhashes for purge capability
# supervisord (root) writes the log files via stdout/stderr capture
# krakenhashes can delete them for purge functionality
chown krakenhashes:krakenhashes /var/log/krakenhashes/nginx
chmod 755 /var/log/krakenhashes/nginx

# Pre-create log files with correct ownership before supervisord starts
# This ensures supervisord doesn't create them as root
echo "Creating log files with correct ownership (PUID=$PUID, PGID=$PGID)..."
touch /var/log/krakenhashes/backend/backend.log
touch /var/log/krakenhashes/nginx/access.log
touch /var/log/krakenhashes/nginx/error.log
touch /var/log/krakenhashes/supervisord.log
touch /var/log/krakenhashes/logrotate.log
touch /var/log/krakenhashes/logrotate.err

# Set ownership based on PUID/PGID
chown "$PUID:$PGID" /var/log/krakenhashes/backend/backend.log
# nginx logs owned by krakenhashes so purge can delete them
# supervisord (root) can write regardless of ownership
chown krakenhashes:krakenhashes /var/log/krakenhashes/nginx/access.log
chown krakenhashes:krakenhashes /var/log/krakenhashes/nginx/error.log
chown "$PUID:$PGID" /var/log/krakenhashes/supervisord.log
chown "$PUID:$PGID" /var/log/krakenhashes/logrotate.log
chown "$PUID:$PGID" /var/log/krakenhashes/logrotate.err

# Set permissions
chmod 644 /var/log/krakenhashes/backend/backend.log
chmod 644 /var/log/krakenhashes/nginx/access.log
chmod 644 /var/log/krakenhashes/nginx/error.log
chmod 644 /var/log/krakenhashes/supervisord.log
chmod 644 /var/log/krakenhashes/logrotate.log
chmod 644 /var/log/krakenhashes/logrotate.err

# Set directory permissions
chmod -R 755 "/var/log/krakenhashes"

# Create backend .env file
cat > /etc/krakenhashes/.env << EOF
# Server Configuration
KH_HTTP_PORT=${KH_HTTP_PORT:-1337}
KH_HOST=${KH_HOST:-0.0.0.0}
KH_HTTPS_PORT=${KH_HTTPS_PORT:-31337}
KH_IN_DOCKER=TRUE

# Get container's hostname and IP
# For certbot mode, exclude localhost as it cannot be validated by any CA
# For self-signed and provided modes, include localhost for local development
if [ "${KH_TLS_MODE}" = "certbot" ]; then
    KH_ADDITIONAL_DNS_NAMES=$(hostname)
else
    KH_ADDITIONAL_DNS_NAMES=localhost,$(hostname)
fi
KH_ADDITIONAL_IP_ADDRESSES=127.0.0.1,0.0.0.0,$(hostname -i)

# Database Configuration
DB_HOST=${DB_HOST:-postgres}
DB_PORT=${DB_PORT:-5432}
DB_NAME=${DB_NAME}
DB_USER=${DB_USER}
DB_PASSWORD=${DB_PASSWORD}

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

# Wait for PostgreSQL to be ready (PostgreSQL runs in a separate container)
echo "Waiting for PostgreSQL at ${DB_HOST}:${DB_PORT}..."
until nc -z ${DB_HOST} ${DB_PORT} 2>/dev/null; do
    echo "PostgreSQL is unavailable - sleeping"
    sleep 2
done
echo "PostgreSQL is up - continuing with startup"

# Print environment variables for debugging
echo "Environment variables:"
echo "KH_IN_DOCKER=${KH_IN_DOCKER}"
echo "KH_HOST=${KH_HOST}"
echo "KH_HTTP_PORT=${KH_HTTP_PORT}"
echo "KH_HTTPS_PORT=${KH_HTTPS_PORT}"


echo "Starting supervisord..."
exec "$@" 