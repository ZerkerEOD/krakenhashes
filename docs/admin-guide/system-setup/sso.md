# SSO Authentication

## Overview

KrakenHashes supports Single Sign-On (SSO) authentication, allowing users to log in using their existing enterprise identity providers. This reduces password fatigue, centralizes access control, and improves security through federated authentication.

## Supported Provider Types

KrakenHashes supports four SSO provider types:

| Provider Type | Description | Use Case |
|---------------|-------------|----------|
| **LDAP/AD** | Lightweight Directory Access Protocol | Active Directory, OpenLDAP |
| **SAML 2.0** | Security Assertion Markup Language | Enterprise IdPs (Okta, OneLogin, ADFS) |
| **OpenID Connect** | Modern OAuth2-based identity layer | Authentik, Keycloak, Azure AD, Google |
| **OAuth 2.0** | Authorization framework | GitHub, custom OAuth providers |

## Configuration

### Global SSO Settings

Navigate to **Admin Panel → SSO Settings** to configure global authentication options:

1. **Local Authentication Enabled**
   - Toggle to enable/disable username/password login
   - Default: Enabled
   - **Warning**: Ensure at least one admin has SSO access before disabling

2. **Auto-Create Users (JIT Provisioning)**
   - When enabled, new users are automatically created on first SSO login
   - Default: Enabled
   - User information is populated from the identity provider

3. **Auto-Enable Users**
   - When enabled, auto-created users are immediately active
   - Default: Disabled (requires admin approval)
   - When disabled, new users see "Pending Approval" message

### Provider-Specific Settings

Each provider can override global settings:

- **Override Auto-Create**: Enable/disable JIT provisioning per provider
- **Override Auto-Enable**: Enable/disable auto-activation per provider

## SSO Encryption Key

### Purpose

The `SSO_ENCRYPTION_KEY` environment variable protects sensitive SSO secrets stored in the database using AES-256-GCM encryption.

### What Gets Encrypted

| Secret Type | Description |
|-------------|-------------|
| LDAP Bind Password | Service account password for LDAP queries |
| SAML SP Private Key | Private key for signing SAML requests |
| OAuth Client Secret | Secret for OAuth token exchange |

### Key Requirements

- **Size**: Exactly 32 bytes (256 bits)
- **Format**: Base64-encoded string OR raw 32-byte string
- **Algorithm**: AES-256-GCM with 96-bit random nonce per encryption

### Generating a Key

```bash
# Generate a secure encryption key
openssl rand -base64 32
```

Example output: `K7gNU3sdo+OL0wNhqoVWhr3g6s1xYv72ol/pe/Unols=`

### Configuration

Add to your environment or `.env` file:

```bash
SSO_ENCRYPTION_KEY=K7gNU3sdo+OL0wNhqoVWhr3g6s1xYv72ol/pe/Unols=
```

!!! danger "Production Requirement"
    Always set `SSO_ENCRYPTION_KEY` in production. Without it, the system generates an ephemeral key that is lost on restart, making all encrypted secrets unrecoverable.

!!! warning "Ephemeral Key Behavior"
    If `SSO_ENCRYPTION_KEY` is not set:

    - A random 32-byte key is generated at startup
    - Log warning: "SSO_ENCRYPTION_KEY not set - generating ephemeral key"
    - All encrypted secrets become invalid after restart
    - Suitable only for development/testing

### High Availability Deployments

When running multiple backend instances:

- All instances **must** use the same `SSO_ENCRYPTION_KEY`
- Store the key in a secrets manager (Vault, AWS Secrets Manager, etc.)
- Rotate keys by re-encrypting all secrets with a new key

## LDAP Configuration

### Basic Setup

1. Navigate to **Admin Panel → SSO Settings → Add Provider**
2. Select **LDAP/Active Directory**
3. Configure the following:

| Field | Description | Example |
|-------|-------------|---------|
| Name | Display name for login page | `Corporate LDAP` |
| Server URL | LDAP server address | `ldap://ldap.example.com:389` or `ldaps://ldap.example.com:636` |
| Bind DN | Service account distinguished name | `cn=svc-krakenhashes,ou=Service Accounts,dc=example,dc=com` |
| Bind Password | Service account password | (encrypted in database) |
| Base DN | Search base for users | `ou=Users,dc=example,dc=com` |
| User Filter | LDAP filter for user lookup | `(&(objectClass=person)(sAMAccountName=%s))` |

### Attribute Mappings

| Attribute | Purpose | Common Values |
|-----------|---------|---------------|
| Email | User's email address | `mail`, `userPrincipalName` |
| Username | Login identifier | `sAMAccountName`, `uid` |
| Display Name | Friendly name | `displayName`, `cn` |

### TLS Settings

| Option | Description |
|--------|-------------|
| Use StartTLS | Upgrade connection to TLS (port 389) |
| Skip Certificate Verification | Disable cert validation (not recommended) |
| Custom CA Certificate | PEM-encoded CA cert for self-signed servers |

### MFA Behavior

!!! info "LDAP + Local MFA"
    LDAP authentication requires local MFA verification after successful bind. This provides an additional security layer since LDAP doesn't inherently support MFA.

## SAML 2.0 Configuration

### Prerequisites

Before configuring SAML, you'll need from your Identity Provider (IdP):

- IdP Entity ID
- IdP SSO URL
- IdP Certificate (for signature verification)

### Basic Setup

1. Navigate to **Admin Panel → SSO Settings → Add Provider**
2. Select **SAML 2.0**
3. Configure the required fields:

| Field | Description | Example |
|-------|-------------|---------|
| Name | Display name | `Okta SSO` |
| SP Entity ID | KrakenHashes identifier | `https://krakenhashes.example.com` |
| IdP Entity ID | Identity provider identifier | `http://www.okta.com/exk123abc` |
| IdP SSO URL | Login endpoint | `https://dev-123.okta.com/app/sso/saml` |
| IdP Certificate | Public cert for signature validation | PEM format |

### RSA Key Requirements for SAML Trust

#### Service Provider (SP) Keys

SP keys are **automatically generated** when you create or save a SAML provider. This simplifies configuration since manual key generation is no longer required.

**Automatic Key Generation:**

| Property | Value |
|----------|-------|
| Key Size | 2048-bit RSA |
| Certificate Validity | 10 years |
| Algorithm | RSA with SHA-256 |
| Common Name | SP Entity ID |

When you create a new SAML provider or save an existing one without keys, KrakenHashes automatically:

1. Generates a 2048-bit RSA private key
2. Creates a self-signed certificate valid for 10 years
3. Encrypts and stores the private key in the database
4. Makes the certificate available via the SP metadata endpoint

**Manual Key Generation (Optional):**

If you prefer to use your own keys (e.g., CA-signed certificates), you can still provide them:

**Key Format Requirements:**

| Requirement | Details |
|-------------|---------|
| Key Format | PKCS#8 (recommended) or PKCS#1, PEM-encoded |
| Certificate Format | PEM or base64-encoded DER |
| Self-Signed | Fully supported |
| CA-Signed | Fully supported |
| Key-Cert Match | Certificate must correspond to private key |

**Generating Custom Self-Signed SP Certificates:**

```bash
# Generate a self-signed certificate valid for 1 year
openssl req -x509 -newkey rsa:2048 \
  -keyout sp-key.pem \
  -out sp-cert.pem \
  -days 365 \
  -nodes \
  -subj "/CN=KrakenHashes SAML SP"

# View the certificate
openssl x509 -in sp-cert.pem -text -noout
```

- Copy contents of `sp-key.pem` to **SP Private Key** field
- Copy contents of `sp-cert.pem` to **SP Certificate** field

#### Identity Provider (IdP) Certificate

- **Always required** for SAML providers
- Used to verify assertion signatures
- Obtain from your IdP's metadata or admin console
- Format: PEM or base64-encoded DER

### SP Metadata Endpoint

KrakenHashes exposes SP metadata for IdP configuration:

```
GET /api/auth/saml/{provider_id}/metadata
```

This endpoint:

- Is publicly accessible (no authentication required)
- Returns XML metadata in SAML 2.0 format
- Includes SP certificate in `KeyDescriptor` element
- Provides ACS URL and entity ID

**ACS URL Format:**
```
https://your-domain.com/api/auth/saml/{provider_id}/acs
```

### Assertion Settings

| Option | Description | Default |
|--------|-------------|---------|
| Sign Requests | Sign AuthnRequests with SP key | Yes (always enabled) |
| Require Signed Assertions | Verify IdP signature | Yes |
| Require Encrypted Assertions | Decrypt assertions with SP key | No |

!!! note "Request Signing"
    Request signing is always enabled because SP keys are automatically generated. This improves security by ensuring all AuthnRequests are cryptographically signed.

### MFA Behavior

!!! info "SAML MFA Trust"
    SAML authentication trusts the IdP's MFA. If your IdP requires MFA, KrakenHashes will not prompt for additional local MFA.

## OAuth 2.0 / OpenID Connect Configuration

### OIDC vs OAuth 2.0

| Feature | OIDC | OAuth 2.0 |
|---------|------|-----------|
| ID Token | Yes | No |
| Discovery | Yes (`/.well-known/openid-configuration`) | No |
| User Info | Standardized | Provider-specific |
| Use Case | Identity + Authorization | Authorization only |

### Basic Setup (OIDC)

1. Navigate to **Admin Panel → SSO Settings → Add Provider**
2. Select **OpenID Connect**
3. Configure:

| Field | Description | Example |
|-------|-------------|---------|
| Name | Display name | `Authentik` |
| Client ID | Application identifier | `krakenhashes-app` |
| Client Secret | Application secret | (encrypted in database) |
| Discovery URL | OIDC configuration endpoint | `https://auth.example.com/application/o/krakenhashes/.well-known/openid-configuration` |

### Callback URL

Your IdP needs the OAuth callback URL:

```
https://your-domain.com/api/auth/oauth/{provider_id}/callback
```

This URL is displayed in the provider card after creation.

### OAuth 2.0 Manual Configuration

For non-OIDC providers, disable discovery and configure manually:

| Field | Description |
|-------|-------------|
| Authorization URL | OAuth authorize endpoint |
| Token URL | Token exchange endpoint |
| User Info URL | User profile endpoint |
| JWKS URL | JSON Web Key Set endpoint (optional) |

### Scopes

Default scopes: `openid profile email`

Additional scopes may be required depending on your IdP and attribute needs.

### Signature Algorithm Requirements

!!! warning "RS256 Required"
    KrakenHashes requires ID tokens signed with RS256 (RSA-SHA256). If your IdP uses HS256 (HMAC-SHA256), you must reconfigure it.

    **Why RS256?**

    - RS256 uses asymmetric cryptography (only IdP can sign)
    - HS256 uses symmetric cryptography (anyone with client_secret can forge tokens)

**Configuring Authentik for RS256:**

1. Go to **Authentik Admin → Applications → Providers**
2. Edit your OIDC provider
3. Set **Signing Key** to an RSA key
4. Save changes

### MFA Behavior

!!! info "OAuth/OIDC MFA Trust"
    OAuth and OIDC authentication trusts the IdP's MFA. No additional local MFA is required.

### Username Attribute Fallback

KrakenHashes automatically detects usernames from common claim names. The configured **Username Attribute** is tried first, followed by these fallbacks:

| Order | Claim Name | Description |
|-------|------------|-------------|
| 1 | (configured attribute) | Your custom username attribute |
| 2 | `preferred_username` | OIDC standard claim |
| 3 | `username` | Common claim name |
| 4 | `user_name` | Some providers use this |
| 5 | `login` | GitHub uses this |
| 6 | `nickname` | Some providers |
| 7 | `name` | Fallback to display name |

If no username is found after trying all fallbacks, the email address is used as the username.

!!! tip "Provider-Specific Notes"
    - **GitHub**: Uses `login` claim for username
    - **Google**: Uses `email` as username (no username claim)
    - **Azure AD**: Uses `preferred_username` or `upn`
    - **Authentik/Keycloak**: Uses `preferred_username` by default

## SSO User Accounts

### Just-In-Time (JIT) Provisioning

When a user authenticates via SSO for the first time:

1. **Email Matching**: System checks for existing user with matching email
2. **Identity Linking**: If found, SSO identity is linked to existing account
3. **User Creation**: If not found and auto-create is enabled, new user is created
4. **Account Status**: New users are enabled or disabled based on auto-enable setting

### Account Linking

Users can have multiple SSO identities linked:

- View linked accounts in **User Settings → Linked Accounts**
- Unlink accounts (if not the last authentication method)
- Link additional providers while logged in

### SSO User Password Generation

#### Why Random Passwords?

SSO users authenticate through external identity providers and don't need local passwords. However, the database requires a `password_hash` value. Instead of modifying the database schema, KrakenHashes generates an unguessable random password.

#### How It Works

1. Generate 48 cryptographically random bytes using `crypto/rand`
2. Encode to base64 → 64-character string
3. Hash with bcrypt (cost factor 10)
4. Store the hash in the database

#### Security Properties

| Property | Value |
|----------|-------|
| Entropy | 384 bits (48 random bytes) |
| Password Length | 64 characters (base64) |
| Bcrypt Compliance | Under 72-byte maximum |
| Guessability | Computationally impossible |
| Local Login | Impossible without admin reset |

#### Admin Override

Administrators can reset an SSO user's password through User Management, enabling local login as a fallback if the SSO provider is unavailable.

## Per-User Authentication Overrides

Administrators can override global authentication settings per user:

| Override | Purpose |
|----------|---------|
| Local Auth Override | Force enable/disable local login for specific user |
| SSO Auth Override | Force enable/disable SSO for specific user |
| Override Notes | Document reason for override |

**Use Cases:**

- Emergency admin access when SSO is down
- Restricting specific users to SSO only
- Service accounts that require local authentication

## Best Practices

### Security

1. **Always set `SSO_ENCRYPTION_KEY`** in production
2. **Use RS256** for OIDC token signing
3. **Enable assertion signing** for SAML
4. **Use LDAPS** or StartTLS for LDAP connections
5. **Regularly rotate** SP certificates and encryption keys
6. **Monitor failed login attempts** in audit logs

### Deployment

1. **Test thoroughly** before disabling local authentication
2. **Keep at least one admin** with local auth override
3. **Document recovery procedures** for IdP outages
4. **Use the same encryption key** across all backend instances

### User Experience

1. **Provide clear error messages** for SSO failures
2. **Document SSO options** for end users
3. **Configure appropriate session timeouts**
4. **Test all provider types** before production deployment

## Troubleshooting

### Common Issues

1. **"Invalid signature algorithm HS256"**
   - **Cause**: IdP signing tokens with HS256 instead of RS256
   - **Solution**: Configure IdP to use RS256 signing key

2. **"Issuer mismatch"**
   - **Cause**: Trailing slash inconsistency in discovery URL
   - **Solution**: Ensure discovery URL matches IdP exactly (including trailing slashes)

3. **"Account processing failed"**
   - **Cause**: Various server-side errors
   - **Solution**: Check backend logs for specific error message

4. **"Pending approval"**
   - **Cause**: Auto-enable users is disabled
   - **Solution**: Admin must enable the user account

5. **"SSO provider not found"**
   - **Cause**: Provider failed to load after creation
   - **Solution**: Check provider configuration and use Test Connection

6. **"Failed to decrypt assertion"**
   - **Cause**: Missing or incorrect SP private key
   - **Solution**: Ensure SP private key matches the certificate sent to IdP

7. **LDAP "Invalid credentials"**
   - **Cause**: Wrong bind DN or password
   - **Solution**: Verify service account credentials with `ldapsearch`

### Diagnostic Steps

1. **Check backend logs** for detailed error messages
2. **Use Test Connection** to verify provider configuration
3. **Verify IdP configuration** matches KrakenHashes settings
4. **Check network connectivity** to IdP servers
5. **Validate certificates** haven't expired

### Getting Help

If you encounter issues not covered here:

1. Check the [GitHub Issues](https://github.com/ZerkerEOD/krakenhashes/issues)
2. Join the [Discord community](https://discord.gg/taafA9cSFV)
3. Review IdP-specific documentation
