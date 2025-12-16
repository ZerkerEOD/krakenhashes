# Security Guide

This guide covers security considerations and best practices for KrakenHashes deployment and operation.

## Table of Contents

1. [Data Security](#data-security)
2. [Data Retention Security](#data-retention-security)
3. [Authentication & Access Control](#authentication-access-control)
4. [Network Security](#network-security)
5. [Agent Security](#agent-security)
6. [Database Security](#database-security)
7. [File System Security](#file-system-security)
8. [Audit & Compliance](#audit-compliance)

## Data Security

### Password Hash Protection

KrakenHashes handles sensitive password hash data. Follow these practices:

- **Access Control**: Limit access to hashlists based on roles and teams
- **Client Isolation**: Hashlists are associated with specific clients for data segregation
- **Secure Storage**: Hash files stored with restricted permissions in `/var/lib/krakenhashes/hashlists/`
- **No Plaintext Logging**: System never logs recovered passwords or sensitive hash values

### Cracked Password Handling

- Passwords stored only in database after successful crack
- No automatic export of cracked passwords
- Access to cracked passwords requires authentication
- Potfile managed separately with controlled access

!!! warning "Potfile Security"
    The potfile (`/var/lib/krakenhashes/wordlists/custom/potfile.txt`) contains plaintext passwords from ALL cracked hashes and is **NOT** affected by the retention system. This means:
    - Passwords remain in the potfile even after hashlists are deleted
    - The potfile grows indefinitely unless manually managed
    - May conflict with data protection compliance requirements
    - Must be secured with strict file permissions and access controls

## Data Retention Security

### Secure Data Deletion

The retention system implements multiple layers of secure deletion to prevent data recovery:

#### 1. File System Security

When files are deleted due to retention policies:

- **Random Overwrite**: Files are overwritten with random data before deletion
- **Multiple Passes**: Sensitive data overwritten in 4KB chunks
- **Immediate Removal**: Files removed from filesystem after overwrite
- **No Recovery**: Prevents standard file recovery tools from retrieving data

Implementation:
```go
// Files are overwritten with random data
randomData := make([]byte, 4096)
for written < fileInfo.Size() {
    rand.Read(randomData)
    file.Write(randomData)
}
os.Remove(filePath)
```

#### 2. Database Security

Database records are protected against recovery:

- **Transaction Safety**: All deletions occur within database transactions
- **CASCADE Deletion**: Related records automatically removed via foreign key constraints
- **Orphan Cleanup**: Hashes not linked to any hashlist are deleted
- **VACUUM Operations**: PostgreSQL VACUUM ANALYZE prevents recovery from:
  - Dead tuples in heap files
  - Write-Ahead Log (WAL) entries
  - Transaction logs

Affected tables during retention purge:
- `hashlists` (primary target)
- `hashlist_hashes` (associations)
- `hashes` (orphaned entries)
- `agent_hashlists` (distribution records)
- `job_executions` (job history)
- `job_tasks` (task assignments)

#### 3. Agent-Side Cleanup

Agents automatically clean temporary files:

- **3-Day Retention**: Temporary files removed after 3 days
- **Automatic Process**: Runs every 6 hours
- **Protected Resources**: Base files (binaries, wordlists, rules) never auto-deleted
- **Storage Management**: Prevents disk space exhaustion on compute nodes

### Retention Policy Configuration

- **System Default**: Configurable default retention period for all data
- **Client-Specific**: Per-client retention overrides for compliance
- **Audit Trail**: All deletions logged with timestamp and affected records
- **Manual Override**: Administrators can trigger immediate purge if needed

### Potfile Exclusion from Retention

!!! danger "Critical Security Gap"
    The potfile is **NOT** managed by the retention system and requires separate security procedures:

    **Security Implications:**
    - Plaintext passwords persist indefinitely in the potfile
    - Deleted hashlist passwords remain recoverable from potfile
    - No automatic cleanup when clients/hashlists are deleted
    - Potential compliance violation for GDPR/data protection laws

    **Required Actions:**
    1. Implement manual potfile cleanup procedures
    2. Create audit trail for potfile modifications
    3. Consider encrypting the potfile at rest
    4. Restrict potfile access to minimum required personnel
    5. Document potfile retention policy separately for compliance

## Authentication & Access Control

### Multi-Factor Authentication (MFA)

KrakenHashes supports multiple MFA methods:

- **TOTP**: Time-based One-Time Passwords via authenticator apps (Bitwarden, Google Authenticator, Authy)
- **Email**: Verification codes sent to registered email
- **Passkey**: WebAuthn/FIDO2 credentials using security keys or platform authenticators
- **Backup Codes**: Recovery codes for emergency access

### Passkey Security

Passkeys provide the highest level of MFA security:

- **Phishing Resistant**: Credentials bound to specific domain (RP ID)
- **Clone Detection**: Sign count validation detects duplicated authenticators
- **No Shared Secrets**: Public key cryptography instead of shared secrets
- **User Presence**: Requires user interaction (touch, biometric)

### Password Security

- **Bcrypt Hashing**: All passwords hashed with bcrypt
- **Configurable Requirements**: Min length, complexity rules
- **Password History**: Prevents password reuse
- **Account Lockout**: Automatic lockout after failed attempts

### Role-Based Access Control (RBAC)

System roles with increasing privileges:

1. **User**: Standard access to assigned resources
2. **Admin**: Full system administration
3. **Agent**: Agent-specific operations only
4. **System**: Internal system operations

### JWT Token Security

- **Short-Lived Access Tokens**: 15-minute default expiry
- **Refresh Token Rotation**: New refresh token on each use
- **Secure Storage**: Tokens never logged or stored in plaintext
- **Revocation Support**: Immediate token invalidation

#### Session-Token Linking

KrakenHashes enforces true session termination through a database-backed token-session relationship:

- **Bound Sessions**: Each active session is linked to its JWT token via foreign key constraint
- **CASCADE Delete**: Terminating a session automatically revokes the associated JWT token from the database
- **True Logout**: Session termination immediately invalidates authentication - users cannot continue accessing resources
- **No Orphaned Tokens**: Database constraints prevent tokens from persisting after session removal
- **Security Enforcement**: Authentication middleware validates both token existence and session validity

This architecture ensures that when a user or administrator terminates a session through the UI, the action has immediate effect. The JWT token is removed from the database, causing the authentication middleware to reject subsequent requests with that token.

## Network Security

### TLS/SSL Configuration

Multiple TLS modes supported:

1. **Self-Signed Certificates**
   - Automatic generation with proper extensions
   - Browser-compatible certificates
   - Full certificate chain support

2. **Provided Certificates**
   - Custom certificate installation
   - Certificate validation
   - Chain verification

3. **Let's Encrypt**
   - Automatic renewal via ACME
   - Production-ready certificates

### API Security

- **Rate Limiting**: Prevents abuse and DoS attacks
- **CORS Configuration**: Controlled cross-origin access
- **Request Validation**: Input sanitization and validation
- **API Key Authentication**: Secure agent authentication

## Agent Security

### Registration Security

- **Claim Codes**: One-time or continuous registration codes
- **API Key Generation**: Unique keys per agent
- **Certificate Exchange**: TLS certificate verification
- **Voucher Expiration**: Time-limited registration windows

### Communication Security

- **WebSocket over TLS**: Encrypted agent communication
- **Heartbeat Monitoring**: Detect disconnected agents
- **Message Authentication**: Signed messages prevent tampering
- **Command Authorization**: Agents verify command sources

### File Synchronization

- **Checksum Verification**: MD5/SHA validation of transferred files
- **Partial Downloads**: Resume support for large files
- **Access Control**: Agents access only assigned files
- **Cleanup Policy**: Automatic removal of unused files

## Database Security

### Connection Security

- **TLS Connections**: Encrypted database connections
- **Connection Pooling**: Limited concurrent connections
- **Prepared Statements**: SQL injection prevention
- **Transaction Isolation**: ACID compliance

### Data Protection

- **No Soft Deletes**: Hard deletion with CASCADE
- **Audit Tables**: Separate audit trail for critical operations
- **UUID Primary Keys**: Prevent sequential ID attacks
- **JSONB Validation**: Schema validation for JSON fields

### Backup Security

- **Encrypted Backups**: Optional backup encryption
- **Offsite Storage**: Remote backup recommendations
- **Point-in-Time Recovery**: Transaction log backups
- **Test Restorations**: Regular recovery testing

## File System Security

### Directory Permissions

Recommended permissions:

```bash
/var/lib/krakenhashes/
├── binaries/     (755, krakenhashes:krakenhashes)
├── wordlists/    (755, krakenhashes:krakenhashes)
├── rules/        (755, krakenhashes:krakenhashes)
└── hashlists/    (750, krakenhashes:krakenhashes)  # Restricted
```

### Path Sanitization

- **Directory Traversal Prevention**: Path validation
- **Symlink Protection**: Restricted symlink following
- **Temporary File Security**: Secure temp file creation
- **Upload Validation**: File type and size limits

## Audit & Compliance

### Audit Logging

Comprehensive audit trail for:

- **User Actions**: Login, logout, configuration changes
- **Data Access**: Hashlist views, downloads
- **Administrative Actions**: User management, system configuration
- **Security Events**: Failed logins, MFA failures, suspicious activity
- **Retention Operations**: All deletion operations with details

### Compliance Features

- **Data Retention Policies**: Configurable per regulations
- **Secure Deletion**: Meets data destruction requirements
- **Access Logs**: Complete access trail for auditing
- **Export Controls**: Restricted data export capabilities

### Security Monitoring

Monitor these key metrics:

1. **Failed Login Attempts**: Potential brute force attacks
2. **API Rate Limit Hits**: Possible abuse
3. **Agent Disconnections**: Network or security issues
4. **Retention Purge Logs**: Verify proper data deletion
5. **Database VACUUM Status**: Ensure WAL cleanup

### Log Retention

- **Security Logs**: Retain for compliance period
- **Audit Logs**: Permanent retention recommended
- **System Logs**: Rotate based on size/age
- **Backup Logs**: Match backup retention period

## Security Best Practices

### Regular Tasks

1. **Weekly**
   - Review security event logs
   - Check failed login attempts
   - Verify agent connectivity

2. **Monthly**
   - Audit user access and roles
   - Review retention policy compliance
   - Test backup restoration

3. **Quarterly**
   - Update TLS certificates
   - Review and update passwords
   - Security assessment

### Emergency Procedures

1. **Suspected Breach**
   - Disable affected accounts
   - Revoke all JWT tokens
   - Review audit logs
   - Change system passwords

2. **Data Leak**
   - Identify affected hashlists
   - Trigger immediate retention purge
   - Notify affected clients
   - Document incident

3. **Agent Compromise**
   - Revoke agent API key
   - Remove agent from system
   - Audit agent's job history
   - Regenerate claim codes

## Conclusion

Security in KrakenHashes is multi-layered, from secure data deletion in the retention system to comprehensive authentication and audit trails. Regular monitoring and adherence to these security practices ensures data protection and compliance with security requirements.