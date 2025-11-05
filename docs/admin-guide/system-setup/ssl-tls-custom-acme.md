# Custom ACME Server Guide

This guide explains how to use KrakenHashes with custom ACME servers for automated certificate management using internal PKI systems.

## Overview

KrakenHashes certbot mode now supports arbitrary ACME-compatible certificate authorities, not just Let's Encrypt. This enables:

- Using internal PKI systems (smallstep CA, HashiCorp Vault, etc.)
- Maintaining trusted certificates without public CAs
- Automated certificate lifecycle management
- Integration with existing corporate PKI infrastructure

## ACME Protocol

The Automatic Certificate Management Environment (ACME) protocol automates:
- Certificate issuance
- Domain validation
- Certificate renewal
- Revocation

Popular ACME servers:
- **Let's Encrypt** (default) - Public CA
- **Smallstep CA** - Open-source internal PKI
- **HashiCorp Vault** - Enterprise secrets management
- **Pebble** - ACME testing server
- **step-ca** - Lightweight CA for development

## Quick Start: Smallstep CA

Smallstep CA is a popular open-source ACME server for internal PKI.

### Prerequisites

1. Smallstep CA server running and accessible
2. ACME provisioner configured
3. DNS-01 challenge configured (typically Cloudflare)
4. Domain name pointing to your KrakenHashes server

### Configuration

```bash
KH_TLS_MODE=certbot
KH_CERTBOT_DOMAIN=kraken.internal.example.com
KH_CERTBOT_EMAIL=admin@example.com
KH_CERTBOT_SERVER=https://ca.internal.example.com/acme/acme/directory
KH_CERTBOT_AUTO_RENEW=true
CLOUDFLARE_API_TOKEN=your_cloudflare_token
```

### ACME Directory URL

The directory URL is the entry point for ACME. For smallstep, it typically follows this pattern:

```
https://<ca-server>:<port>/acme/<provisioner-name>/directory
```

Example:
```
https://ca.internal.example.com:9000/acme/acme/directory
```

Check your smallstep configuration for the exact URL:
```bash
step ca provisioner list
```

## Configuration Options

### Required Variables

```bash
KH_TLS_MODE=certbot
KH_CERTBOT_DOMAIN=your.domain.com
KH_CERTBOT_EMAIL=admin@example.com
KH_CERTBOT_SERVER=https://acme.server.com/directory
```

### Optional Variables

```bash
KH_CERTBOT_STAGING=false
KH_CERTBOT_AUTO_RENEW=true
KH_CERTBOT_RENEW_HOOK=/path/to/script.sh
KH_CERTBOT_EXTRA_ARGS=--preferred-challenges dns-01
```

### Advanced Configuration

For complex setups, use `KH_CERTBOT_EXTRA_ARGS`:

```bash
# Use specific DNS provider
KH_CERTBOT_EXTRA_ARGS=--dns-route53

# Change challenge type
KH_CERTBOT_EXTRA_ARGS=--preferred-challenges http-01

# Multiple arguments
KH_CERTBOT_EXTRA_ARGS=--dns-route53 --dns-route53-propagation-seconds 30
```

## DNS Challenge Providers

Currently, KrakenHashes ships with Cloudflare DNS support. For other providers, use `KH_CERTBOT_EXTRA_ARGS`.

### Cloudflare (Default)

```bash
KH_CERTBOT_EXTRA_ARGS=  # Not needed, built-in
CLOUDFLARE_API_TOKEN=your_token
```

### AWS Route53

```bash
KH_CERTBOT_EXTRA_ARGS=--dns-route53
AWS_ACCESS_KEY_ID=your_key
AWS_SECRET_ACCESS_KEY=your_secret
```

### Google Cloud DNS

```bash
KH_CERTBOT_EXTRA_ARGS=--dns-google --dns-google-credentials /path/to/credentials.json
```

### Azure DNS

```bash
KH_CERTBOT_EXTRA_ARGS=--dns-azure --dns-azure-credentials /path/to/credentials.json
```

### Manual DNS (For Testing)

```bash
KH_CERTBOT_EXTRA_ARGS=--manual --preferred-challenges dns
```

## Smallstep CA Setup

### Step 1: Install Smallstep CA

```bash
# Download and install step-ca
wget https://dl.step.sm/gh-release/certificates/latest/step-ca_linux_amd64.tar.gz
tar -xzf step-ca_linux_amd64.tar.gz
sudo mv step-ca_*/bin/step-ca /usr/local/bin/
```

### Step 2: Initialize CA

```bash
step ca init --deployment-type standalone --name "Internal CA" --dns ca.internal.example.com --address :9000 --provisioner admin
```

### Step 3: Configure ACME Provisioner

```bash
step ca provisioner add acme --type ACME
```

### Step 4: Start CA

```bash
step-ca $(step path)/config/ca.json
```

### Step 5: Get ACME Directory URL

```bash
step ca provisioner list

# Output will show:
# Name: acme
# Type: ACME
# URL: https://ca.internal.example.com:9000/acme/acme/directory
```

### Step 6: Configure DNS

Add DNS record for your domain:
```
kraken.internal.example.com.  A  10.0.0.100
```

### Step 7: Configure KrakenHashes

```bash
KH_TLS_MODE=certbot
KH_CERTBOT_DOMAIN=kraken.internal.example.com
KH_CERTBOT_EMAIL=admin@example.com
KH_CERTBOT_SERVER=https://ca.internal.example.com:9000/acme/acme/directory
CLOUDFLARE_API_TOKEN=your_token
```

### Step 8: Start KrakenHashes

```bash
docker-compose up -d
```

Monitor logs for certificate acquisition:
```bash
docker-compose logs -f krakenhashes | grep certbot
```

## HashiCorp Vault PKI

### Prerequisites

1. Vault server with PKI secrets engine enabled
2. ACME enabled on the PKI backend
3. Appropriate policies configured

### Enable ACME in Vault

```bash
# Enable PKI secrets engine
vault secrets enable pki

# Configure PKI
vault write pki/config/urls \
    issuing_certificates="https://vault.example.com:8200/v1/pki/ca" \
    crl_distribution_points="https://vault.example.com:8200/v1/pki/crl"

# Enable ACME
vault write pki/config/acme enabled=true
```

### KrakenHashes Configuration

```bash
KH_TLS_MODE=certbot
KH_CERTBOT_DOMAIN=kraken.example.com
KH_CERTBOT_EMAIL=admin@example.com
KH_CERTBOT_SERVER=https://vault.example.com:8200/v1/pki/acme/directory
CLOUDFLARE_API_TOKEN=your_token
```

## Certificate Renewal

### Automatic Renewal

Renewal happens automatically when enabled:

```bash
KH_CERTBOT_AUTO_RENEW=true
```

KrakenHashes checks for renewal:
- Every 12 hours (in-process check)
- Via cron/systemd timer (Docker deployments)

Certificates renew when less than 30 days until expiration.

### Manual Renewal

Force renewal immediately:

**Docker:**
```bash
docker exec krakenhashes /usr/local/bin/certbot-renew.sh
```

**Standalone:**
```bash
certbot renew \
    --server https://ca.internal.example.com/acme/directory \
    --dns-cloudflare \
    --dns-cloudflare-credentials /path/to/cloudflare.ini
```

### Renewal Hooks

Execute custom scripts after successful renewal:

```bash
KH_CERTBOT_RENEW_HOOK=/usr/local/bin/post-renewal.sh
```

Example hook script:
```bash
#!/bin/bash
# /usr/local/bin/post-renewal.sh

echo "Certificates renewed at $(date)" >> /var/log/cert-renewals.log

# Send notification
curl -X POST https://slack.com/api/chat.postMessage \
  -H "Authorization: Bearer $SLACK_TOKEN" \
  -d "text=KrakenHashes certificates renewed successfully"

# Reload services
docker-compose restart krakenhashes
```

## Troubleshooting

### "ACME server returned error"

**Cause:** Cannot connect to ACME server or server returned error.

**Diagnosis:**
```bash
# Test ACME directory endpoint
curl https://ca.internal.example.com/acme/acme/directory

# Should return JSON with ACME endpoints
```

**Solutions:**
- Verify ACME server URL is correct
- Check firewall allows outbound HTTPS to CA server
- Ensure CA server is running and accessible
- Check CA server logs for errors

### "DNS validation failed"

**Cause:** ACME server cannot verify domain ownership via DNS.

**Diagnosis:**
```bash
# Check if DNS record exists
dig TXT _acme-challenge.kraken.example.com

# Test from ACME server's perspective
# (run on ACME server or use its network)
nslookup _acme-challenge.kraken.example.com
```

**Solutions:**
- Verify DNS provider credentials are correct
- Ensure domain is managed by the DNS provider
- Check DNS propagation (may take minutes)
- Try different DNS challenge plugin

### "Certificate issued but agents can't connect"

**Cause:** Agents don't trust the custom CA.

**Solution:** This should work automatically because:
1. ACME server issues certificate
2. Certificate chain includes intermediate CA
3. KrakenHashes extracts intermediate for agent trust
4. Agents download and trust the intermediate
5. Agents verify server cert against trusted CA

If it still fails:
```bash
# Check what CA agents downloaded
cat agent/config/ca.crt

# Verify server cert chain
openssl s_client -connect server:31337 -showcerts
```

### "Rate limited by ACME server"

**Cause:** Too many certificate requests.

**Solutions:**
- Use staging environment for testing (if available)
- Wait for rate limit window to reset
- Check ACME server rate limit policies
- For smallstep, adjust rate limits in configuration

### Staging vs Production

Some ACME servers have staging environments for testing:

**Let's Encrypt:**
```bash
KH_CERTBOT_STAGING=true  # Uses LE staging
```

**Custom ACME:**
```bash
# Test environment
KH_CERTBOT_SERVER=https://ca-staging.example.com/acme/directory

# Production environment
KH_CERTBOT_SERVER=https://ca.example.com/acme/directory
```

Always test with staging first to avoid rate limits!

## Security Considerations

### Trust Chain

With custom CAs, ensure agents trust the certificate chain:

1. Server cert signed by internal CA
2. Agents download internal CA cert
3. Agents verify server cert against internal CA
4. Connection trusted!

### CA Certificate Distribution

The `/ca.crt` endpoint serves the CA certificate to agents. Ensure:
- Port 1337 accessible from agent networks
- HTTP is acceptable (certificate is public information)
- Agents can download on first connection

### Private Key Security

Even with automated management:
- Private keys never leave the server
- Stored with 600 permissions
- Not included in backups (regenerate instead)
- Rotated regularly via automated renewal

### Network Isolation

For maximum security:
- Run internal ACME CA on isolated network
- Restrict ACME server access to authorized clients only
- Use firewall rules to limit access
- Monitor ACME server for unauthorized requests

## Advanced: HTTP-01 Challenge

DNS-01 is default, but HTTP-01 is possible with extra configuration:

### Requirements

1. Port 80 publicly accessible (or from ACME server)
2. Webroot writable by certbot
3. HTTP server to serve challenges

### Configuration

```bash
KH_CERTBOT_EXTRA_ARGS=--standalone --preferred-challenges http-01
```

Or with existing webserver:
```bash
KH_CERTBOT_EXTRA_ARGS=--webroot --webroot-path /var/www/html --preferred-challenges http-01
```

**Note:** HTTP-01 doesn't work well for internal services not publicly accessible. Use DNS-01 instead.

## Comparison: Custom ACME vs Provided Mode

### Use Custom ACME (certbot mode) when:
✅ You have an internal ACME server
✅ You want automated renewal
✅ You can use DNS-01 or HTTP-01 challenges
✅ Certificates expire frequently (e.g., 90 days)

### Use Provided Mode when:
✅ Certificates from non-ACME CA
✅ You manage renewal externally
✅ Long-lived certificates (1+ years)
✅ Certificates obtained manually

## Examples

### Example 1: Smallstep with Cloudflare DNS

```bash
KH_TLS_MODE=certbot
KH_CERTBOT_DOMAIN=kraken.lab.example.com
KH_CERTBOT_EMAIL=lab@example.com
KH_CERTBOT_SERVER=https://step-ca.lab.example.com:9000/acme/acme/directory
KH_CERTBOT_AUTO_RENEW=true
CLOUDFLARE_API_TOKEN=abc123...
```

### Example 2: Vault PKI with Route53

```bash
KH_TLS_MODE=certbot
KH_CERTBOT_DOMAIN=kraken.corp.example.com
KH_CERTBOT_EMAIL=security@example.com
KH_CERTBOT_SERVER=https://vault.corp.example.com:8200/v1/pki/acme/directory
KH_CERTBOT_EXTRA_ARGS=--dns-route53
KH_CERTBOT_AUTO_RENEW=true
AWS_ACCESS_KEY_ID=AKIA...
AWS_SECRET_ACCESS_KEY=...
```

### Example 3: Custom CA with Manual DNS

```bash
KH_TLS_MODE=certbot
KH_CERTBOT_DOMAIN=kraken.dev.example.com
KH_CERTBOT_EMAIL=dev@example.com
KH_CERTBOT_SERVER=https://ca.dev.example.com/acme/directory
KH_CERTBOT_EXTRA_ARGS=--manual --preferred-challenges dns
KH_CERTBOT_AUTO_RENEW=false
```

## See Also

- [Provided Certificate Mode Guide](ssl-tls-provided-mode.md) - Manual certificate management
- [Main SSL/TLS Setup Guide](ssl-tls.md) - Overview of all TLS modes
- [Smallstep Documentation](https://smallstep.com/docs/)
- [ACME Protocol Specification](https://datatracker.ietf.org/doc/html/rfc8555)
