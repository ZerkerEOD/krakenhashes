# Provided Certificate Mode Guide

This guide explains how to use the "provided" TLS mode in KrakenHashes, which allows you to bring your own SSL/TLS certificates from any certificate authority (CA).

## Overview

Provided mode is ideal when you:
- Have existing certificates from a commercial CA (DigiCert, Sectigo, etc.)
- Use an internal PKI (Active Directory Certificate Services, OpenSSL CA, etc.)
- Obtained certificates from Let's Encrypt externally
- Have any other trusted certificates you want to use

### How It Works

KrakenHashes will:
1. Load your server certificate and private key
2. Automatically extract the CA certificate from your certificate chain
3. Serve the CA certificate to agents for trust verification
4. Use your certificates for all HTTPS/WSS connections

## Quick Start

### Minimum Configuration

```bash
KH_TLS_MODE=provided
KH_CERT_FILE=/path/to/server.crt
KH_KEY_FILE=/path/to/server.key
```

The CA certificate will be automatically extracted from your certificate chain.

### With Explicit CA

```bash
KH_TLS_MODE=provided
KH_CERT_FILE=/path/to/server.crt
KH_KEY_FILE=/path/to/server.key
KH_CA_FILE=/path/to/ca.crt
```

Explicitly specify the CA certificate to override auto-detection.

## Certificate Requirements

### File Format

All certificates must be in PEM format:

```
-----BEGIN CERTIFICATE-----
MIIDXTCCAkWgAwIBAgIJAKZ...
...
-----END CERTIFICATE-----
```

### Certificate Chain

Your server certificate file can contain:
- **Single certificate** (leaf only) - for self-signed or when chain is provided separately
- **Full chain** (leaf + intermediates) - recommended for maximum compatibility
- **Complete chain** (leaf + intermediates + root) - works but root usually not needed

## Common Scenarios

### Scenario 1: Let's Encrypt (External Certbot)

If you obtained Let's Encrypt certificates using certbot on another machine or manually:

**Files from certbot:**
- `fullchain.pem` - Contains your certificate + Let's Encrypt intermediate
- `privkey.pem` - Your private key
- `chain.pem` - Let's Encrypt intermediate only

**Configuration:**
```bash
KH_TLS_MODE=provided
KH_CERT_FILE=/path/to/fullchain.pem
KH_KEY_FILE=/path/to/privkey.pem
```

The Let's Encrypt intermediate will be auto-extracted from fullchain.pem for agent trust.

**Certificate Renewal:**
When you renew with certbot, simply replace the files and restart KrakenHashes. Agents will continue to trust the new certificate because it's signed by the same CA.

### Scenario 2: Commercial CA (DigiCert, Sectigo, etc.)

**Files from CA:**
- `server.crt` - Your server certificate
- `server.key` - Your private key
- `intermediate.crt` - CA intermediate certificate
- `root.crt` - CA root certificate

**Option A: Use full chain file**

Combine certificates into a chain file:
```bash
cat server.crt intermediate.crt root.crt > fullchain.crt
```

Configuration:
```bash
KH_TLS_MODE=provided
KH_CERT_FILE=/path/to/fullchain.crt
KH_KEY_FILE=/path/to/server.key
```

**Option B: Specify CA separately**

Configuration:
```bash
KH_TLS_MODE=provided
KH_CERT_FILE=/path/to/server.crt
KH_KEY_FILE=/path/to/server.key
KH_CA_FILE=/path/to/root.crt
```

### Scenario 3: Internal PKI

For corporate or lab environments with an internal certificate authority:

**Files:**
- `kraken-server.crt` - Your server certificate
- `kraken-server.key` - Your private key
- `internal-ca.crt` - Your internal CA root certificate

**Configuration:**
```bash
KH_TLS_MODE=provided
KH_CERT_FILE=/path/to/kraken-server.crt
KH_KEY_FILE=/path/to/kraken-server.key
KH_CA_FILE=/path/to/internal-ca.crt
```

**Agent Trust:**
Agents will download `internal-ca.crt` and use it to verify the server's identity. This works whether your internal PKI uses:
- Active Directory Certificate Services
- OpenSSL-based CA
- HashiCorp Vault PKI
- Smallstep CA
- Any other PKI infrastructure

### Scenario 4: Self-Signed Certificate

If you have a self-signed certificate created outside of KrakenHashes:

**Configuration:**
```bash
KH_TLS_MODE=provided
KH_CERT_FILE=/path/to/self-signed.crt
KH_KEY_FILE=/path/to/self-signed.key
```

Since it's self-signed, the certificate itself will be used as the CA for agent trust.

**Note:** For most self-signed use cases, use `KH_TLS_MODE=self-signed` instead, which handles generation automatically.

## Smart CA Extraction

KrakenHashes automatically determines which certificate to use as the CA for agent trust:

### Priority 1: Explicit CA File
If `KH_CA_FILE` is set, that certificate is always used.

```bash
KH_CA_FILE=/path/to/specific-ca.crt  # Always takes precedence
```

### Priority 2: Extract from Chain
If your server certificate file contains multiple certificates:

```
-----BEGIN CERTIFICATE-----
[Leaf Certificate - server.example.com]
-----END CERTIFICATE-----
-----BEGIN CERTIFICATE-----
[Intermediate CA]
-----END CERTIFICATE-----
-----BEGIN CERTIFICATE-----
[Root CA]
-----END CERTIFICATE-----
```

The **last certificate** in the chain is used as the CA for agents.

### Priority 3: Self-Signed Detection
If your server certificate file contains only one certificate and no `KH_CA_FILE` is specified, that certificate is used as both the server cert and the CA (self-signed scenario).

## Certificate Renewal

### Transparent Renewal

One of the key advantages of using CA-based trust: **certificate renewal is transparent to agents**.

When your server certificate expires and you get a new one:

1. Replace the certificate files on the server
2. Restart KrakenHashes
3. Agents continue working **without any updates**

This works because:
- Agents trust the CA that signed your certificate
- The new certificate is signed by the same CA
- Therefore, agents automatically trust the new certificate

**Example with Let's Encrypt (90-day certificates):**
```bash
# Initial setup
KH_CERT_FILE=/etc/letsencrypt/live/example.com/fullchain.pem

# After 90 days, certbot renews automatically
# Restart KrakenHashes to load new cert
docker-compose restart krakenhashes

# All agents continue working - no updates needed!
```

### Certificate Pinning Warning

Some may consider using "certificate pinning" (trusting the server certificate directly instead of the CA). **This is strongly discouraged** because:

❌ Every time the server certificate renews, every agent must be manually updated
❌ Completely breaks automated renewal workflows
❌ Operational nightmare with short-lived certificates (Let's Encrypt 90 days)

CA-based trust is the industry standard for good reason.

## File Permissions

For security, ensure proper file permissions:

```bash
# Server certificate (public) - readable by all
chmod 644 /path/to/server.crt
chmod 644 /path/to/ca.crt

# Private key (secret) - readable only by owner
chmod 600 /path/to/server.key
chown krakenhashes:krakenhashes /path/to/server.key
```

## Validation

After configuring provided mode, verify the setup:

### 1. Check Backend Logs

Look for successful initialization:
```
INFO  Initializing user-provided certificate mode
INFO  Loaded 2 certificate(s) from chain
INFO  Server certificate subject: CN=kraken.example.com
INFO  Server certificate validity: 2025-01-01 to 2026-01-01
INFO  Auto-extracted CA from chain (cert 2/2): CN=Example CA
INFO  Provided certificate mode initialized successfully
```

### 2. Verify CA Certificate Endpoint

Agents download the CA from `http://server:1337/ca.crt`. Verify it's accessible:

```bash
curl http://your-server:1337/ca.crt
```

You should see a PEM-encoded certificate.

### 3. Test Agent Connection

Start an agent and verify it connects successfully. Check agent logs for:
```
INFO  Successfully loaded CA certificate from disk
INFO  WebSocket connection established
```

### 4. Verify Certificate Chain

Check that the server presents the full certificate chain:

```bash
openssl s_client -connect your-server:31337 -showcerts
```

You should see multiple certificates in the output (server + intermediates).

## Troubleshooting

### "Failed to parse certificate chain"

**Cause:** Certificate file is not in valid PEM format.

**Solution:** Ensure the file contains PEM-encoded certificates:
```bash
openssl x509 -in server.crt -text -noout
```

If you have a DER-encoded certificate, convert it:
```bash
openssl x509 -inform DER -in server.der -out server.pem
```

### "Private key does not match certificate"

**Cause:** The private key file doesn't correspond to the certificate's public key.

**Solution:** Verify the key matches:
```bash
# Extract public key from cert
openssl x509 -in server.crt -pubkey -noout > cert-pubkey.pem

# Extract public key from private key
openssl pkey -in server.key -pubout > key-pubkey.pem

# Compare - they should be identical
diff cert-pubkey.pem key-pubkey.pem
```

### "No valid certificates found in CA file"

**Cause:** `KH_CA_FILE` points to an invalid or empty file.

**Solution:** Verify the CA file is a valid PEM certificate:
```bash
openssl x509 -in ca.crt -text -noout
```

### Agents Can't Connect - "Certificate Verification Failed"

**Cause:** Agents can't verify the server certificate against the CA.

**Diagnosis:**
1. Check what CA certificate agents downloaded:
   ```bash
   cat agent/config/ca.crt
   ```

2. Verify the server cert was issued by that CA:
   ```bash
   openssl verify -CAfile ca.crt server.crt
   ```

**Solution:** Ensure the CA file includes the complete chain needed to verify the server cert.

### Certificate Expired But Can't Renew

**Cause:** Certificates expired and you can't get new ones immediately.

**Temporary Workaround:** Switch to self-signed mode temporarily:
```bash
KH_TLS_MODE=self-signed
```

Then obtain proper certificates and switch back to provided mode.

## Converting Certificate Formats

### PKCS#12 (.pfx, .p12) to PEM

Many CAs provide certificates in PKCS#12 format:

```bash
# Extract certificate
openssl pkcs12 -in cert.pfx -clcerts -nokeys -out server.crt

# Extract private key
openssl pkcs12 -in cert.pfx -nocerts -nodes -out server.key

# Extract CA chain
openssl pkcs12 -in cert.pfx -cacerts -nokeys -out ca.crt
```

### DER to PEM

```bash
# Convert certificate
openssl x509 -inform DER -in server.der -out server.crt

# Convert private key
openssl rsa -inform DER -in server.key.der -out server.key
```

### Combine Separate Certificates into Chain

```bash
cat server.crt intermediate.crt root.crt > fullchain.crt
```

## Security Best Practices

### 1. Use Strong Private Keys

Minimum 2048-bit RSA or 256-bit ECC:
```bash
# Check key size
openssl rsa -in server.key -text -noout | grep "Private-Key"
```

### 2. Protect Private Keys

- Store with 600 permissions (owner read/write only)
- Never commit to version control
- Use encrypted storage if possible
- Rotate regularly (annually at minimum)

### 3. Verify Certificate Validity

Before deploying, check:
```bash
openssl x509 -in server.crt -noout -dates
openssl x509 -in server.crt -noout -subject
openssl x509 -in server.crt -noout -issuer
```

### 4. Monitor Expiration

Set reminders for certificate renewal well before expiration:
```bash
# Check days until expiration
openssl x509 -in server.crt -noout -checkend $((30*86400)) && echo "Expires in >30 days" || echo "WARNING: Expires soon!"
```

### 5. Use Full Certificate Chains

Always include intermediates for maximum client compatibility:
```bash
# Good: Full chain
cat server.crt intermediate.crt > fullchain.crt

# Bad: Server cert only (may fail validation)
cp server.crt fullchain.crt
```

## Advanced: Wildcard Certificates

Wildcard certificates work perfectly with provided mode:

**Certificate:** `*.example.com`

**Valid for:**
- `kraken.example.com`
- `api.example.com`
- Any `*.example.com`

**Not valid for:**
- `example.com` (root domain, unless included as SAN)
- `sub.kraken.example.com` (multi-level subdomains)

**Configuration:**
```bash
KH_TLS_MODE=provided
KH_CERT_FILE=/path/to/wildcard.crt  # *.example.com
KH_KEY_FILE=/path/to/wildcard.key
```

## Migration

### From Self-Signed to Provided

1. Obtain certificates from your CA
2. Update configuration:
   ```bash
   KH_TLS_MODE=provided
   KH_CERT_FILE=/path/to/new-cert.crt
   KH_KEY_FILE=/path/to/new-cert.key
   ```
3. Restart KrakenHashes
4. **Important:** Agents will need to download the new CA certificate
5. On each agent machine, remove old CA: `rm agent/config/ca.crt`
6. Restart agents - they'll download the new CA automatically

### From Certbot Mode to Provided

If you want to manage Let's Encrypt certificates externally:

1. Stop using built-in certbot mode
2. Obtain certificates using your preferred method
3. Configure provided mode to use those certificates
4. Agents will continue working (same Let's Encrypt CA)

## See Also

- [Custom ACME Server Guide](ssl-tls-custom-acme.md) - Use internal ACME servers with certbot mode
- [Main SSL/TLS Setup Guide](ssl-tls.md) - Overview of all TLS modes
- [Agent Configuration](../agent-setup.md) - How agents handle certificates
