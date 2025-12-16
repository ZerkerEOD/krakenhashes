# Authentication Settings Administration

## Overview
KrakenHashes provides robust authentication settings to ensure system security. This document covers the configuration of password policies, account security settings, and multi-factor authentication (MFA) options.

## Password Policy

The password policy settings define the requirements for user passwords across the system.

### Configuration Options

1. **Minimum Password Length**
   - Default: 15 characters
   - Must be a positive integer
   - Recommended: 15+ characters minimum
   - Enforced during password creation and changes

2. **Character Requirements**
   - **Require Uppercase Letters**: When enabled, passwords must contain at least one uppercase letter (A-Z)
   - **Require Lowercase Letters**: When enabled, passwords must contain at least one lowercase letter (a-z)
   - **Require Numbers**: When enabled, passwords must contain at least one number (0-9)
   - **Require Special Characters**: When enabled, passwords must contain at least one special character (!@#$%^&*(),.?":{}|<>)

### Best Practices
- Enable all character requirements for maximum security
- Balance security with usability when setting minimum length
- Consider industry standards (NIST, OWASP) when configuring
- Document password requirements clearly for users

## Account Security

Account security settings manage login attempts, session duration, and security notifications.

### Configuration Options

1. **Maximum Failed Login Attempts**
   - Default: 5 attempts
   - Defines how many failed login attempts are allowed before account lockout
   - Must be a positive integer
   - Recommended range: 3-5 attempts

2. **Account Lockout Duration**
   - Default: 60 minutes
   - Duration in minutes before a locked account is automatically unlocked
   - Must be a positive integer
   - Affects accounts locked due to exceeded login attempts

3. **JWT Token Expiry**
   - Default: 60 minutes
   - Duration in minutes before an authentication token expires
   - Forces users to re-authenticate after expiration
   - Balances security with user convenience

4. **Notification Aggregation Interval**
   - Default: 60 minutes
   - How often to aggregate and send security notifications
   - Prevents notification fatigue while maintaining awareness
   - Groups similar security events within the interval

### Best Practices
- Adjust lockout duration based on threat model
- Consider user experience when setting token expiry
- Monitor failed login attempts for attack patterns
- Review security notifications regularly

## Multi-Factor Authentication (MFA) Settings

MFA provides an additional layer of security beyond passwords.

### General Settings

1. **Require MFA for All Users**
   - Toggle to enforce MFA across all user accounts
   - To enable an email provider must be configured as email is the default MFA
   - Affects new and existing users

### Allowed MFA Methods

The system supports multiple MFA methods:

1. **Email Authentication**
   - Sends verification codes to user's registered email
   - Requires configured email provider
   - Good balance of security and convenience

2. **Authenticator Apps**
   - Compatible with standard TOTP authenticator apps
   - More secure than email-based authentication
   - Works offline once configured
   - Examples: Bitwarden, Google Authenticator, Authy, Microsoft Authenticator

3. **Passkey (WebAuthn/FIDO2)**
   - Supports FIDO2/WebAuthn standard
   - Provides highest security level
   - Works with security keys (YubiKey), platform authenticators (Windows Hello, Face ID, Touch ID), and password managers (Bitwarden, 1Password)
   - Requires WebAuthn-compatible browser and configured RP ID
   - **Note**: WebAuthn does NOT support IP addresses as RP IDs - requires domain name

### WebAuthn Configuration

Before users can register passkeys, administrators must configure WebAuthn settings in the Authentication Settings page:

1. **Relying Party ID (RP ID)**
   - Domain name where KrakenHashes is hosted
   - Examples: `localhost` (dev), `krakenhashes.example.com` (prod)
   - Cannot be an IP address (WebAuthn specification limitation)
   - **Warning**: Once set and credentials registered, changing this will invalidate all existing passkeys

2. **Allowed Origins**
   - Full URLs where the application is accessed
   - Examples: `https://localhost:3000`, `https://krakenhashes.example.com`
   - Include all URLs users might use to access the system

3. **Display Name**
   - Friendly name shown during passkey registration
   - Default: "KrakenHashes"

### Code Settings

1. **Email Code Validity**
   - Default: 5 minutes
   - How long email-based MFA codes remain valid
   - Must be at least 1 minute
   - Balance security with delivery delays

2. **Code Cooldown Period**
   - Default: 1 minute
   - Minimum time between code requests
   - Prevents code request spam
   - Must be at least 1 minute

3. **Code Expiry Time**
   - Default: 5 minutes
   - How long codes remain valid after generation
   - Applies to all MFA methods
   - Should account for potential delays

4. **Maximum Code Attempts**
   - Default: 3 attempts
   - Maximum invalid code entries before invalidation
   - Requires new code generation after exceeded
   - Prevents brute force attacks

5. **Number of Backup Codes**
   - Default: 8 codes
   - One-time use backup codes for account recovery
   - Must be at least 1 code
   - Recommended: 8-10 codes

### Best Practices

1. **MFA Implementation**
   - Consider enforcing MFA for all users
   - Enable multiple MFA methods for flexibility
   - Educate users about backup codes importance
   - Regular review of MFA settings

2. **Code Security**
   - Keep validity periods short (5-15 minutes)
   - Implement reasonable cooldown periods
   - Limit invalid attempts
   - Generate sufficient backup codes

3. **User Experience**
   - Clear communication about MFA requirements
   - Document recovery procedures
   - Train support staff on MFA issues
   - Regular testing of MFA workflows

4. **Monitoring and Maintenance**
   - Regular review of MFA logs
   - Monitor failed MFA attempts
   - Update settings based on security needs
   - Keep documentation current

## Troubleshooting

### Common Issues

1. **Users Unable to Enable MFA**
   - Verify email provider configuration
   - Check user permissions
   - Confirm supported authenticator app
   - Review error messages

2. **Locked Accounts**
   - Verify lockout duration settings
   - Check failed attempt count
   - Review security logs
   - Consider administrative unlock

3. **MFA Code Issues**
   - Verify code validity period
   - Check cooldown period
   - Confirm correct email delivery
   - Review time synchronization

4. **Password Policy Problems**
   - Review current policy settings
   - Check character requirement conflicts
   - Verify minimum length appropriateness
   - Consider user feedback 