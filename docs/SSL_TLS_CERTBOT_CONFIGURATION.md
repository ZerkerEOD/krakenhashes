# Certbot SSL/TLS Configuration Guide

This guide covers how to configure KrakenHashes to use Let's Encrypt or custom ACME servers for SSL/TLS certificates using Certbot.

## Table of Contents

- [Overview](#overview)
- [Challenge Types](#challenge-types)
- [Configuration Options](#configuration-options)
- [Scenario 1: Let's Encrypt with DNS-01 (Cloudflare)](#scenario-1-lets-encrypt-with-dns-01-cloudflare)
- [Scenario 2: Let's Encrypt with HTTP-01](#scenario-2-lets-encrypt-with-http-01)
- [Scenario 3: Internal ACME Server with HTTP-01](#scenario-3-internal-acme-server-with-http-01)
- [Troubleshooting](#troubleshooting)
- [Certificate Renewal](#certificate-renewal)

## Overview

KrakenHashes supports automatic SSL/TLS certificate management using Certbot, which can obtain certificates from:
- Let's Encrypt (public ACME server)
- Custom ACME servers (e.g., internal CA, step-ca, smallstep)
- Self-hosted ACME services

The system automatically detects the challenge type based on your configuration and handles certificate issuance and renewal.

## Challenge Types

### DNS-01 Challenge

**Use when:**
- You cannot expose port 80 to the internet
- You need wildcard certificates
- Your infrastructure is behind a firewall/NAT

**Requirements:**
- DNS provider API credentials (currently supports Cloudflare)
- DNS records must be publicly resolvable

**Advantages:**
- Works behind firewalls
- Supports wildcard certificates
- No need to expose HTTP port

**Disadvantages:**
- Requires DNS provider API access
- Slower (DNS propagation time)
- Limited to supported DNS providers

### HTTP-01 Challenge

**Use when:**
- Port 80 is accessible from the internet (or ACME server)
- You don't need wildcard certificates
- You're using an internal ACME server

**Requirements:**
- Port 80 must be accessible to the ACME server
- Domain must resolve to your server

**Advantages:**
- No API credentials needed
- Fast validation
- Works with any ACME server
- Ideal for internal deployments

**Disadvantages:**
- Requires port 80 exposure
- Cannot issue wildcard certificates
- One certificate per domain

## Configuration Options

### Required Environment Variables

```bash
# Basic Certbot Configuration
KH_TLS_MODE=certbot                           # Enable certbot mode
KH_CERTBOT_DOMAIN=your-domain.com             # Domain for certificate
KH_CERTBOT_EMAIL=admin@your-domain.com        # Email for ACME account
```

### Optional Environment Variables

```bash
# Challenge Type
KH_CERTBOT_CHALLENGE_TYPE=http-01             # Options: dns-01, http-01
                                               # Leave empty for auto-detect (see Auto-Detection section below)

# Custom ACME Server
KH_CERTBOT_SERVER=https://acme-v02.api.letsencrypt.org/directory
                                               # Default: Let's Encrypt production

# Let's Encrypt Staging (for testing)
KH_CERTBOT_STAGING=false                      # Use Let's Encrypt staging server

# Auto-Renewal
KH_CERTBOT_AUTO_RENEW=true                    # Enable automatic renewal checks
KH_CERTBOT_RENEW_HOOK=/path/to/script.sh      # Script to run after renewal

# Advanced Arguments
KH_CERTBOT_EXTRA_ARGS=--webroot --webroot-path=/var/www/certbot --preferred-challenges http-01
                                               # Additional certbot arguments
                                               # NOTE: Use --webroot for HTTP-01, NOT --standalone
                                               # (nginx uses port 80, standalone would conflict)

# Custom CA Trust (for internal ACME servers)
KH_CERTBOT_CUSTOM_CA_CERT=/etc/krakenhashes/custom-ca/root-ca.crt
                                               # Path to custom CA certificate
```

### DNS Provider Credentials

For DNS-01 challenge with Cloudflare:

```bash
CLOUDFLARE_API_TOKEN=your_cloudflare_api_token
```

## Challenge Type Auto-Detection

If `KH_CERTBOT_CHALLENGE_TYPE` is not specified (empty), the system automatically detects the challenge type using the following priority:

1. **Parse EXTRA_ARGS** for challenge indicators:
   - Contains `--dns-*` → **dns-01**
   - Contains `--webroot` or `--preferred-challenges http` → **http-01**
   - Contains `--standalone` → **http-01** (⚠️ not recommended - see note below)

2. **Check for Cloudflare token** (backward compatibility):
   - If `CLOUDFLARE_API_TOKEN` environment variable exists → **dns-01**

3. **Default fallback**:
   - No indicators found → **http-01** (most common, requires no API credentials)

### Auto-Detection Examples

**Example 1: Automatic DNS-01 detection**
```bash
# These configurations auto-detect dns-01:
CLOUDFLARE_API_TOKEN=your_token
KH_CERTBOT_CHALLENGE_TYPE=  # empty, will auto-detect

# Or:
KH_CERTBOT_EXTRA_ARGS=--dns-cloudflare --dns-cloudflare-credentials /path/to/creds
KH_CERTBOT_CHALLENGE_TYPE=  # empty, will auto-detect
```

**Example 2: Automatic HTTP-01 detection**
```bash
# These configurations auto-detect http-01:
KH_CERTBOT_EXTRA_ARGS=--standalone --preferred-challenges http-01
KH_CERTBOT_CHALLENGE_TYPE=  # empty, will auto-detect

# Or just leave everything empty (default http-01):
KH_CERTBOT_CHALLENGE_TYPE=  # empty, defaults to http-01
```

**Example 3: Explicit override (recommended for clarity)**
```bash
# Explicitly set challenge type (bypasses auto-detection):
KH_CERTBOT_CHALLENGE_TYPE=http-01  # Always use http-01
KH_CERTBOT_EXTRA_ARGS=--standalone --preferred-challenges http-01
```

### When to Use Auto-Detection vs Explicit

**Use auto-detection (empty value):**
- When migrating from older configurations
- When using standard patterns in EXTRA_ARGS
- For backward compatibility with existing setups

**Use explicit value (recommended):**
- For production deployments (clearer configuration)
- To avoid ambiguity in complex setups
- When you want predictable behavior

## Scenario 1: Let's Encrypt with DNS-01 (Cloudflare)

**When to use:** Public internet deployment, need wildcard certificates, or port 80 unavailable.

### Configuration

```bash
# .env file
KH_TLS_MODE=certbot
KH_CERTBOT_DOMAIN=krakenhashes.example.com
KH_CERTBOT_EMAIL=admin@example.com
KH_CERTBOT_CHALLENGE_TYPE=dns-01
KH_CERTBOT_AUTO_RENEW=true

# Cloudflare credentials
CLOUDFLARE_API_TOKEN=your_cloudflare_api_token
```

### Setup Steps

1. **Create Cloudflare API Token:**
   - Go to Cloudflare Dashboard → My Profile → API Tokens
   - Create token with `Zone:DNS:Edit` permission
   - Copy the token

2. **Configure environment variables** as shown above

3. **Deploy:**
   ```bash
   docker-compose up -d --build
   ```

4. **Verify certificate:**
   ```bash
   docker exec krakenhashes-app ls -la /etc/krakenhashes/certs/live/
   ```

### Challenge Type Detection

See [Challenge Type Auto-Detection](#challenge-type-auto-detection) section above for complete details on how the system detects challenge types when `KH_CERTBOT_CHALLENGE_TYPE` is not explicitly set.

## Scenario 2: Let's Encrypt with HTTP-01

**When to use:** Public internet deployment with port 80 accessible, simple single-domain certificate.

### Configuration

```bash
# .env file
KH_TLS_MODE=certbot
KH_CERTBOT_DOMAIN=krakenhashes.example.com
KH_CERTBOT_EMAIL=admin@example.com
KH_CERTBOT_CHALLENGE_TYPE=http-01
KH_CERTBOT_EXTRA_ARGS=--webroot --webroot-path=/var/www/certbot --preferred-challenges http-01
KH_CERTBOT_AUTO_RENEW=true
```

### Setup Steps

1. **Ensure DNS is configured:**
   ```bash
   dig +short krakenhashes.example.com
   # Should return your server's public IP
   ```

2. **Ensure port 80 is accessible:**
   ```bash
   # Test from external network
   curl http://krakenhashes.example.com
   ```

3. **Configure environment variables** as shown above

4. **Deploy:**
   ```bash
   docker-compose up -d --build
   ```

### Firewall Configuration

Ensure port 80 is open:

```bash
# UFW
sudo ufw allow 80/tcp

# iptables
sudo iptables -A INPUT -p tcp --dport 80 -j ACCEPT
```

## Scenario 3: Internal ACME Server with HTTP-01

**When to use:** Corporate/internal deployment with internal PKI and ACME server (e.g., step-ca, smallstep).

### Configuration

```bash
# .env file
KH_TLS_MODE=certbot
KH_CERTBOT_DOMAIN=krakenhashes.internal.corp
KH_CERTBOT_EMAIL=krakenhashes@internal.corp
KH_CERTBOT_CHALLENGE_TYPE=http-01
KH_CERTBOT_SERVER=https://ca.internal.corp/acme/acme/directory
KH_CERTBOT_EXTRA_ARGS=--webroot --webroot-path=/var/www/certbot --preferred-challenges http-01
KH_CERTBOT_AUTO_RENEW=true

# Path to internal CA certificate (must be mounted as volume)
KH_CERTBOT_CUSTOM_CA_CERT=/etc/krakenhashes/custom-ca/root-ca.crt
```

### Setup Steps

1. **Obtain your internal root CA certificate:**
   ```bash
   # Example: download from your CA server
   curl https://ca.internal.corp/root-ca.crt -o root-ca.crt
   ```

2. **Create directory for custom CA:**
   ```bash
   mkdir -p ./config/custom-ca
   cp root-ca.crt ./config/custom-ca/
   ```

3. **Update docker-compose to mount CA certificate:**
   ```yaml
   volumes:
     - ${KH_CONFIG_DIR_HOST:-./config}:/etc/krakenhashes
   ```

4. **Configure environment variables** as shown above

5. **Deploy:**
   ```bash
   docker-compose up -d --build
   ```

6. **Verify custom CA installation:**
   ```bash
   docker exec krakenhashes-app cat /etc/ssl/certs/ca-certificates.crt | grep "krakenhashes-custom-ca"
   ```

### Internal Network Requirements

- DNS: `krakenhashes.internal.corp` must resolve to your server
- Port 80: Must be accessible from ACME server
- HTTPS: ACME server must be accessible (with custom CA trust)

### Testing Internal ACME Server

```bash
# Test ACME server accessibility
docker exec krakenhashes-app curl -v https://ca.internal.corp/acme/acme/directory

# Should return JSON directory listing without SSL errors
```

## Troubleshooting

### Issue: "CLOUDFLARE_API_TOKEN environment variable is required"

**Cause:** System detected DNS-01 challenge but no Cloudflare token provided.

**Solutions:**
1. Set `KH_CERTBOT_CHALLENGE_TYPE=http-01` explicitly
2. Or provide `CLOUDFLARE_API_TOKEN` if you want DNS-01
3. Or remove any `--dns-` flags from `KH_CERTBOT_EXTRA_ARGS`

### Issue: "Connection refused" on port 80

**Cause:** Port 80 is not accessible for HTTP-01 challenge.

**Solutions:**
1. Check firewall rules: `sudo ufw status`
2. Check port binding: `docker ps` and look for `0.0.0.0:80->80/tcp`
3. Check if another service is using port 80: `sudo lsof -i :80`
4. Ensure Docker port mapping includes `- "80:80"`

### Issue: "Certbot standalone mode conflicts with nginx"

**Cause:** Both nginx and certbot standalone mode try to bind to port 80.

**Solution:**
Use `--webroot` mode instead of `--standalone`:
```bash
KH_CERTBOT_EXTRA_ARGS=--webroot --webroot-path=/var/www/certbot --preferred-challenges http-01
```
The entrypoint script automatically creates `/var/www/certbot` and nginx serves ACME challenges from this directory.

### Issue: "SSL certificate verify failed" with internal ACME

**Cause:** Internal ACME server uses custom CA that's not trusted.

**Solutions:**
1. Set `KH_CERTBOT_CUSTOM_CA_CERT` to your root CA certificate path
2. Ensure the certificate file is mounted in the container
3. Verify the certificate is valid: `openssl x509 -in root-ca.crt -text -noout`

### Issue: Certificate not renewing automatically

**Cause:** Auto-renewal disabled or renewal fails.

**Solutions:**
1. Check `KH_CERTBOT_AUTO_RENEW=true` is set
2. View certbot logs: `docker logs krakenhashes-app | grep certbot`
3. Manually trigger renewal:
   ```bash
   docker exec krakenhashes-app certbot renew --config-dir /etc/krakenhashes/certs
   ```

### Issue: Challenge type not detected correctly

**Cause:** Conflicting configuration or ambiguous settings.

**Solutions:**
1. Explicitly set `KH_CERTBOT_CHALLENGE_TYPE=http-01` or `dns-01`
2. Check for conflicting flags in `KH_CERTBOT_EXTRA_ARGS`
3. Review detection logic priority:
   - Explicit `KH_CERTBOT_CHALLENGE_TYPE` (highest)
   - Parse `KH_CERTBOT_EXTRA_ARGS`
   - Check `CLOUDFLARE_API_TOKEN` existence
   - Default to `http-01` (lowest)

## Certificate Renewal

### Automatic Renewal

When `KH_CERTBOT_AUTO_RENEW=true`:
- System checks certificates twice daily (every 12 hours)
- Certificates are renewed 30 days before expiry
- Same challenge method used for renewal as issuance

### Manual Renewal

Force certificate renewal:

```bash
docker exec krakenhashes-app certbot renew --force-renewal \
  --config-dir /etc/krakenhashes/certs \
  --work-dir /etc/krakenhashes/certs/work \
  --logs-dir /etc/krakenhashes/certs/logs
```

### Renewal Hooks

Execute custom actions after renewal:

```bash
KH_CERTBOT_RENEW_HOOK=/path/to/renewal-script.sh
```

Example renewal script:
```bash
#!/bin/bash
# Reload nginx or restart services
docker restart krakenhashes-app
```

## Certificate Locations

Certificates are stored in `/etc/krakenhashes/certs/`:

```
/etc/krakenhashes/certs/
├── live/
│   └── your-domain.com/
│       ├── fullchain.pem    # Certificate + intermediate chain
│       ├── privkey.pem      # Private key
│       ├── cert.pem         # Certificate only
│       └── chain.pem        # Intermediate certificate
├── archive/                 # Previous certificates
├── renewal/                 # Renewal configuration
└── logs/                    # Certbot logs
```

## Security Best Practices

1. **Protect API tokens:** Store `CLOUDFLARE_API_TOKEN` in `.env` file (not in version control)
2. **Use staging first:** Test with `KH_CERTBOT_STAGING=true` before production
3. **Monitor renewal:** Check logs regularly for renewal failures
4. **Custom CA security:** Ensure internal root CA is properly secured
5. **Certificate permissions:** Certbot automatically sets proper permissions (600 for keys)

## Advanced Configuration

### Multiple Domains (SAN)

Use additional DNS names:

```bash
KH_ADDITIONAL_DNS_NAMES=www.krakenhashes.com,api.krakenhashes.com
```

### Rate Limits

Let's Encrypt has rate limits:
- 50 certificates per registered domain per week
- 5 duplicate certificates per week

Use staging server for testing:
```bash
KH_CERTBOT_STAGING=true
```

### Custom ACME Server Examples

**Smallstep CA:**
```bash
KH_CERTBOT_SERVER=https://ca.example.com:9000/acme/acme/directory
```

**Boulder (Let's Encrypt testing):**
```bash
KH_CERTBOT_SERVER=http://localhost:4001/directory
```

## Support

For issues or questions:
- GitHub: https://github.com/ZerkerEOD/krakenhashes/issues
- Documentation: https://github.com/ZerkerEOD/krakenhashes/docs

## References

- [Certbot Documentation](https://eff-certbot.readthedocs.io/)
- [Let's Encrypt Documentation](https://letsencrypt.org/docs/)
- [ACME Protocol Specification](https://tools.ietf.org/html/rfc8555)
- [Cloudflare API Tokens](https://dash.cloudflare.com/profile/api-tokens)
