# Build stage for frontend
FROM node:20-alpine AS frontend-builder
WORKDIR /app/frontend
# Install jq for version extraction
RUN apk add --no-cache jq
# Copy versions.json for build
COPY versions.json ./
# Install dependencies
COPY frontend/package*.json ./
RUN npm install --force
COPY frontend/ ./
RUN VERSION=$(jq -r .frontend versions.json) && \
    echo "REACT_APP_VERSION=$VERSION" >> .env && \
    CI=false npm run build

# Build stage for agents (all platforms)
FROM golang:1.23.1-alpine AS agent-builder
RUN apk add --no-cache make jq

# Set up parent directory and copy versions.json first
WORKDIR /app
COPY versions.json ./

# Now set up agent directory and build
WORKDIR /app/agent
COPY agent/ ./

# Build agents for all platforms
RUN make clean && make build-all

# Build stage for backend
FROM golang:1.23.1-alpine AS backend-builder
WORKDIR /app/backend
# Install jq for version extraction
RUN apk add --no-cache jq
# Copy versions.json for build
COPY versions.json ./
COPY backend/go.* ./
RUN go mod download
COPY backend/ ./
RUN VERSION=$(jq -r .backend versions.json) && \
    CGO_ENABLED=0 GOOS=linux go build -ldflags="-X main.Version=$VERSION" -o krakenhashes ./cmd/server

# Final stage
FROM debian:bookworm-slim

# Install required packages
RUN apt-get update && apt-get install -y --no-install-recommends \
    nginx \
    supervisor \
    certbot \
    logrotate \
    tzdata \
    jq \
    openssl \
    p7zip-full \
    libcap2-bin \
    gosu \
    curl \
    ca-certificates \
    procps \
    netcat-openbsd \
    && rm -rf /var/lib/apt/lists/*

# Create krakenhashes user and group with default UID/GID 1000
RUN groupadd -g 1000 krakenhashes && \
    useradd -u 1000 -g krakenhashes -d /home/krakenhashes -s /bin/sh -m krakenhashes

# Create necessary directories and set permissions
RUN set -ex && \
    # nginx user is already created by nginx package on Debian
    # Create base directories
    mkdir -p /var/log/krakenhashes && \
    mkdir -p /etc/krakenhashes && \
    mkdir -p /usr/share/nginx/html && \
    mkdir -p /var/cache/nginx && \
    mkdir -p /var/run && \
    # Create log directories with proper ownership
    install -d -m 755 -o krakenhashes -g krakenhashes /var/log/krakenhashes/backend && \
    install -d -m 755 -o root -g root /var/log/krakenhashes/frontend && \
    install -d -m 755 -o www-data -g www-data /var/log/krakenhashes/nginx && \
    # Create log files with proper ownership
    install -m 644 -o krakenhashes -g krakenhashes /dev/null /var/log/krakenhashes/backend/backend.log && \
    install -m 644 -o www-data -g www-data /dev/null /var/log/krakenhashes/nginx/access.log && \
    install -m 644 -o root -g root /dev/null /var/log/krakenhashes/logrotate.log && \
    install -m 644 -o root -g root /dev/null /var/log/krakenhashes/logrotate.err && \
    install -m 644 -o root -g root /dev/null /var/log/krakenhashes/supervisord.log

# Copy and protect versions.json in a non-persistent location
COPY versions.json /usr/local/share/krakenhashes/versions.json
RUN chown root:root /usr/local/share/krakenhashes/versions.json && \
    chmod 644 /usr/local/share/krakenhashes/versions.json

# Extract version for labels
ARG VERSION
RUN VERSION=$(jq -r .backend /usr/local/share/krakenhashes/versions.json)

# Add version labels
LABEL org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.description="KrakenHashes - Password Cracking Management System" \
      org.opencontainers.image.source="https://github.com/ZerkerEOD/krakenhashes"

# Copy Nginx configuration
COPY docker/nginx/nginx.conf /etc/nginx/nginx.conf
COPY docker/nginx/default.conf /etc/nginx/conf.d/default.conf
COPY docker/nginx/templates /etc/nginx/templates

# Copy logrotate configuration
COPY docker/logrotate/krakenhashes /etc/logrotate.d/krakenhashes

# Copy supervisord configuration
COPY docker/supervisord/supervisord.conf /etc/supervisord.conf

# Copy built artifacts
COPY --from=frontend-builder /app/frontend/build /usr/share/nginx/html
COPY --from=backend-builder /app/backend/krakenhashes /usr/local/bin/

# Copy migrations
COPY backend/db/migrations /usr/local/share/krakenhashes/migrations

# Copy built agents from agent-builder stage
COPY --from=agent-builder /app/bin/agent /usr/share/krakenhashes/agents

# Create data directory with proper ownership
RUN mkdir -p /var/lib/krakenhashes && \
    chown -R krakenhashes:krakenhashes /var/lib/krakenhashes && \
    chmod 750 /var/lib/krakenhashes

# Give nginx capability to bind to port 443
RUN setcap 'cap_net_bind_service=+ep' /usr/sbin/nginx

# Copy startup scripts
COPY docker/scripts/entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh

# Set environment variables
ENV KH_IN_DOCKER=TRUE \
    KH_HOST=0.0.0.0

# Expose ports
EXPOSE 443 1337 31337 80

# Set entrypoint
ENTRYPOINT ["/entrypoint.sh"]
CMD ["supervisord", "-c", "/etc/supervisord.conf"] 