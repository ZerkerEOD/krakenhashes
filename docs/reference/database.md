# KrakenHashes Database Schema Reference

This document provides a comprehensive reference for the KrakenHashes database schema, extracted from migration files (v0.1.0-alpha).

## Table of Contents

1. [Core Tables](#core-tables)
   - [users](#users)
   - [teams](#teams)
   - [user_teams](#user_teams)
2. [Authentication & Security](#authentication--security)
   - [auth_tokens](#auth_tokens)
   - [user_passkeys](#user_passkeys)
   - [pending_passkey_registration](#pending_passkey_registration)
   - [pending_passkey_authentication](#pending_passkey_authentication)
   - [mfa_methods](#mfa_methods)
   - [mfa_backup_codes](#mfa_backup_codes)
   - [login_attempts](#login_attempts)
   - [security_events](#security_events)
3. [Agent Management](#agent-management)
   - [agents](#agents)
   - [agent_metrics](#agent_metrics)
   - [agent_teams](#agent_teams)
   - [claim_vouchers](#claim_vouchers)
   - [claim_voucher_usage](#claim_voucher_usage)
4. [Email System](#email-system)
   - [email_config](#email_config)
   - [email_templates](#email_templates)
   - [email_usage](#email_usage)
5. [Hash Management](#hash-management)
   - [hashlists](#hashlists)
   - [hashes](#hashes)
   - [hashcat_hash_types](#hashcat_hash_types)
6. [LM/NTLM Support](#lmntlm-support-v121)
   - [lm_hash_metadata](#lm_hash_metadata)
   - [linked_hashlists](#linked_hashlists)
   - [linked_hashes](#linked_hashes)
7. [Job Management](#job-management)
   - [job_workflows](#job_workflows)
   - [job_executions](#job_executions)
   - [job_tasks](#job_tasks)
   - [job_increment_layers](#job_increment_layers)
   - [preset_increment_layers](#preset_increment_layers)
   - [job_execution_settings](#job_execution_settings)
8. [Resource Management](#resource-management)
   - [wordlists](#wordlists)
   - [rules](#rules)
   - [binary_versions](#binary_versions)
9. [Client & Settings](#client--settings)
   - [clients](#clients)
   - [client_settings](#client_settings)
   - [system_settings](#system_settings)
10. [Performance & Scheduling](#performance--scheduling)
   - [performance_metrics](#performance_metrics)
   - [agent_scheduling](#agent_scheduling)
11. [Migration History](#migration-history)

---

## Core Tables

### users

User accounts for the system, including the special system user with UUID 00000000-0000-0000-0000-000000000000.

| Column | Type | Constraints | Default | Description |
|--------|------|-------------|---------|-------------|
| id | UUID | PRIMARY KEY | gen_random_uuid() | Unique user identifier |
| username | VARCHAR(255) | UNIQUE NOT NULL | | Username for login |
| first_name | VARCHAR(255) | | | User's first name |
| last_name | VARCHAR(255) | | | User's last name |
| email | VARCHAR(255) | UNIQUE NOT NULL | | User's email address |
| password_hash | VARCHAR(255) | NOT NULL | | Bcrypt password hash |
| role | VARCHAR(50) | NOT NULL, CHECK | 'user' | Role: user, admin, agent, system |
| status | VARCHAR(50) | NOT NULL | 'active' | Account status |
| created_at | TIMESTAMP WITH TIME ZONE | NOT NULL | CURRENT_TIMESTAMP | Account creation time |
| updated_at | TIMESTAMP WITH TIME ZONE | NOT NULL | CURRENT_TIMESTAMP | Last update time |

**Indexes:**
- idx_users_username (username)
- idx_users_email (email)
- idx_users_role (role)

**Triggers:**
- update_users_updated_at: Updates updated_at on row modification

### teams

Organizational teams for grouping users.

| Column | Type | Constraints | Default | Description |
|--------|------|-------------|---------|-------------|
| id | UUID | PRIMARY KEY | gen_random_uuid() | Unique team identifier |
| name | VARCHAR(100) | NOT NULL, UNIQUE | | Team name |
| description | TEXT | | | Team description |
| created_at | TIMESTAMP WITH TIME ZONE | NOT NULL | CURRENT_TIMESTAMP | Team creation time |
| updated_at | TIMESTAMP WITH TIME ZONE | NOT NULL | CURRENT_TIMESTAMP | Last update time |

**Indexes:**
- idx_teams_name (name)

**Triggers:**
- update_teams_updated_at: Updates updated_at on row modification

### user_teams

Junction table for user-team relationships.

| Column | Type | Constraints | Default | Description |
|--------|------|-------------|---------|-------------|
| user_id | UUID | NOT NULL, FK → users(id) | | User reference |
| team_id | UUID | NOT NULL, FK → teams(id) | | Team reference |
| role | VARCHAR(50) | NOT NULL, CHECK | 'member' | Role in team: member, admin |
| joined_at | TIMESTAMP WITH TIME ZONE | NOT NULL | CURRENT_TIMESTAMP | Join timestamp |

**Primary Key:** (user_id, team_id)

**Indexes:**
- idx_user_teams_user_id (user_id)
- idx_user_teams_team_id (team_id)

---

## Authentication & Security

### auth_tokens

Stores refresh tokens for JWT authentication.

| Column | Type | Constraints | Default | Description |
|--------|------|-------------|---------|-------------|
| id | UUID | PRIMARY KEY | gen_random_uuid() | Token identifier |
| user_id | UUID | NOT NULL, FK → users(id) | | User reference |
| token | VARCHAR(255) | NOT NULL, UNIQUE | | Refresh token value |
| created_at | TIMESTAMP WITH TIME ZONE | | CURRENT_TIMESTAMP | Token creation time |

**Indexes:**
- idx_auth_tokens_token (token)
- idx_auth_tokens_user_id (user_id)

### user_passkeys

Stores registered WebAuthn/FIDO2 passkey credentials for users.

| Column | Type | Constraints | Default | Description |
|--------|------|-------------|---------|-------------|
| id | UUID | PRIMARY KEY | gen_random_uuid() | Passkey identifier |
| user_id | UUID | NOT NULL, FK → users(id) ON DELETE CASCADE | | User reference |
| credential_id | BYTEA | NOT NULL, UNIQUE | | WebAuthn credential ID |
| public_key | BYTEA | NOT NULL | | Public key for verification |
| aaguid | BYTEA | | | Authenticator attestation GUID |
| sign_count | BIGINT | NOT NULL | 0 | Sign counter for clone detection |
| transports | TEXT[] | | '{}' | Supported transports (usb, nfc, ble, internal) |
| name | VARCHAR(255) | NOT NULL | 'Passkey' | User-assigned passkey name |
| backup_eligible | BOOLEAN | NOT NULL | FALSE | Passkey can be synced/backed up |
| backup_state | BOOLEAN | NOT NULL | FALSE | Passkey is currently backed up |
| created_at | TIMESTAMP WITH TIME ZONE | NOT NULL | CURRENT_TIMESTAMP | Registration time |
| last_used_at | TIMESTAMP WITH TIME ZONE | | | Last authentication time |

**Unique Constraint:** (user_id, credential_id)

**Indexes:**
- idx_user_passkeys_user_id (user_id)
- idx_user_passkeys_credential_id (credential_id)

**Security Features:**
- **Clone Detection**: Sign count must increase with each authentication; non-increasing counts indicate cloned authenticators
- **Backup Flags**: Track whether passkey is synced across devices (Bitwarden, iCloud Keychain, etc.)
- **Phishing Resistant**: Credentials are bound to the configured RP ID (domain)

### pending_passkey_registration

Stores temporary challenges during passkey registration flow (5-minute expiry).

| Column | Type | Constraints | Default | Description |
|--------|------|-------------|---------|-------------|
| user_id | UUID | PRIMARY KEY, FK → users(id) ON DELETE CASCADE | | User registering passkey |
| challenge | BYTEA | NOT NULL | | WebAuthn challenge bytes |
| session_data | BYTEA | NOT NULL | | Serialized session state |
| created_at | TIMESTAMP WITH TIME ZONE | NOT NULL | CURRENT_TIMESTAMP | Challenge creation time |

**Notes:**
- Only one pending registration per user at a time
- Challenges expire after 5 minutes
- Cleanup trigger removes expired entries

### pending_passkey_authentication

Stores temporary challenges during passkey MFA authentication flow (5-minute expiry).

| Column | Type | Constraints | Default | Description |
|--------|------|-------------|---------|-------------|
| session_token | TEXT | PRIMARY KEY | | MFA session token |
| user_id | UUID | NOT NULL, FK → users(id) ON DELETE CASCADE | | User authenticating |
| challenge | BYTEA | NOT NULL | | WebAuthn challenge bytes |
| session_data | BYTEA | NOT NULL | | Serialized session state |
| created_at | TIMESTAMP WITH TIME ZONE | NOT NULL | CURRENT_TIMESTAMP | Challenge creation time |

**Indexes:**
- idx_pending_passkey_auth_user_id (user_id)

**Notes:**
- Linked to MFA session token from login flow
- Challenges expire after 5 minutes
- Cleanup trigger removes expired entries

**Triggers:**
- trigger_cleanup_passkey_challenges: Cleans up expired registration and authentication challenges

---

## Agent Management

### agents

Registered compute agents for distributed processing.

| Column | Type | Constraints | Default | Description |
|--------|------|-------------|---------|-------------|
| id | SERIAL | PRIMARY KEY | | Agent identifier |
| name | VARCHAR(255) | NOT NULL | | Agent name |
| status | VARCHAR(50) | NOT NULL | 'inactive' | Agent status |
| last_heartbeat | TIMESTAMP WITH TIME ZONE | | | Last heartbeat received |
| version | VARCHAR(50) | NOT NULL | | Agent version |
| hardware | JSONB | NOT NULL | | Hardware configuration |
| os_info | JSONB | NOT NULL | '{}' | Operating system info |
| created_by_id | UUID | NOT NULL, FK → users(id) | | Creator user |
| created_at | TIMESTAMP WITH TIME ZONE | NOT NULL | CURRENT_TIMESTAMP | Creation time |
| updated_at | TIMESTAMP WITH TIME ZONE | NOT NULL | CURRENT_TIMESTAMP | Last update time |
| api_key | VARCHAR(64) | UNIQUE | | Agent API key |
| api_key_created_at | TIMESTAMP WITH TIME ZONE | | | API key creation time |
| api_key_last_used | TIMESTAMP WITH TIME ZONE | | | API key last usage |
| last_error | TEXT | | | Last error message |
| metadata | JSONB | | '{}' | Additional metadata |
| owner_id | UUID | FK → users(id) | | Agent owner (added in migration 30) |
| extra_parameters | TEXT | | | Extra hashcat parameters (added in migration 30) |
| is_enabled | BOOLEAN | NOT NULL | true | Agent enabled status (added in migration 31) |

**Indexes:**
- idx_agents_status (status)
- idx_agents_created_by (created_by_id)
- idx_agents_last_heartbeat (last_heartbeat)
- idx_agents_api_key (api_key)
- idx_agents_owner_id (owner_id)

**Triggers:**
- update_agents_updated_at: Updates updated_at on row modification

### agent_metrics

Time-series metrics data for agents.

| Column | Type | Constraints | Default | Description |
|--------|------|-------------|---------|-------------|
| agent_id | INTEGER | NOT NULL, FK → agents(id) | | Agent reference |
| cpu_usage | FLOAT | NOT NULL | | CPU usage percentage |
| gpu_utilization | FLOAT | NOT NULL | | GPU utilization percentage |
| gpu_temp | FLOAT | NOT NULL | | GPU temperature |
| memory_usage | FLOAT | NOT NULL | | Memory usage percentage |
| gpu_metrics | JSONB | NOT NULL | '{}' | Additional GPU metrics |
| timestamp | TIMESTAMP WITH TIME ZONE | NOT NULL | CURRENT_TIMESTAMP | Metric timestamp |

**Primary Key:** (agent_id, timestamp)

**Indexes:**
- idx_agent_metrics_timestamp (timestamp)

### agent_teams

Junction table for agent-team associations.

| Column | Type | Constraints | Default | Description |
|--------|------|-------------|---------|-------------|
| agent_id | INTEGER | NOT NULL, FK → agents(id) | | Agent reference |
| team_id | UUID | NOT NULL, FK → teams(id) | | Team reference |

**Primary Key:** (agent_id, team_id)

### claim_vouchers

Stores active agent registration vouchers.

| Column | Type | Constraints | Default | Description |
|--------|------|-------------|---------|-------------|
| code | VARCHAR(50) | PRIMARY KEY | | Voucher code |
| created_by_id | UUID | NOT NULL, FK → users(id) | | Creator user |
| created_at | TIMESTAMP WITH TIME ZONE | NOT NULL | CURRENT_TIMESTAMP | Creation time |
| updated_at | TIMESTAMP WITH TIME ZONE | NOT NULL | CURRENT_TIMESTAMP | Last update time |
| is_continuous | BOOLEAN | NOT NULL | false | Can be used multiple times |
| is_active | BOOLEAN | NOT NULL | true | Voucher active status |
| used_at | TIMESTAMP WITH TIME ZONE | | | Usage timestamp |
| used_by_agent_id | INTEGER | FK → agents(id) | | Agent that used voucher |

**Indexes:**
- idx_claim_vouchers_code (code)
- idx_claim_vouchers_active (is_active)
- idx_claim_vouchers_created_by (created_by_id)

**Triggers:**
- update_claim_vouchers_updated_at: Updates updated_at on row modification

### claim_voucher_usage

Tracks usage attempts of claim vouchers.

| Column | Type | Constraints | Default | Description |
|--------|------|-------------|---------|-------------|
| id | SERIAL | PRIMARY KEY | | Usage record ID |
| voucher_code | VARCHAR(50) | NOT NULL, FK → claim_vouchers(code) | | Voucher reference |
| attempted_by_id | UUID | NOT NULL, FK → users(id) | | User who attempted |
| attempted_at | TIMESTAMP WITH TIME ZONE | NOT NULL | CURRENT_TIMESTAMP | Attempt timestamp |
| success | BOOLEAN | NOT NULL | false | Success status |
| ip_address | VARCHAR(45) | | | Client IP address |
| user_agent | TEXT | | | Client user agent |
| error_message | TEXT | | | Error message if failed |

**Indexes:**
- idx_claim_voucher_usage_voucher (voucher_code)
- idx_claim_voucher_usage_attempted_by (attempted_by_id)

---

## Email System

### email_config

Email provider configuration.

| Column | Type | Constraints | Default | Description |
|--------|------|-------------|---------|-------------|
| id | SERIAL | PRIMARY KEY | | Config ID |
| provider_type | email_provider_type | NOT NULL | | Provider: mailgun, sendgrid, smtp (added in migration 084) |
| api_key | TEXT | NOT NULL | | Provider API key |
| additional_config | JSONB | | | Additional configuration |
| monthly_limit | INTEGER | | | Monthly email limit |
| reset_date | TIMESTAMP WITH TIME ZONE | | | Limit reset date |
| is_active | BOOLEAN | NOT NULL | false | Active status |
| created_at | TIMESTAMP WITH TIME ZONE | NOT NULL | NOW() | Creation time |
| updated_at | TIMESTAMP WITH TIME ZONE | NOT NULL | NOW() | Last update time |

**Triggers:**
- update_email_config_updated_at: Updates updated_at on row modification

### email_templates

Email template definitions.

| Column | Type | Constraints | Default | Description |
|--------|------|-------------|---------|-------------|
| id | SERIAL | PRIMARY KEY | | Template ID |
| template_type | email_template_type | NOT NULL | | Type: security_event, job_completion, admin_error, mfa_code |
| name | VARCHAR(255) | NOT NULL | | Template name |
| subject | VARCHAR(255) | NOT NULL | | Email subject |
| html_content | TEXT | NOT NULL | | HTML template |
| text_content | TEXT | NOT NULL | | Plain text template |
| created_at | TIMESTAMP WITH TIME ZONE | NOT NULL | NOW() | Creation time |
| updated_at | TIMESTAMP WITH TIME ZONE | NOT NULL | NOW() | Last update time |
| last_modified_by | UUID | FK → users(id) | | Last modifier |

**Indexes:**
- idx_email_templates_type (template_type)

**Triggers:**
- update_email_templates_updated_at: Updates updated_at on row modification

### email_usage

Tracks email usage for rate limiting.

| Column | Type | Constraints | Default | Description |
|--------|------|-------------|---------|-------------|
| id | SERIAL | PRIMARY KEY | | Usage record ID |
| month_year | DATE | NOT NULL, UNIQUE | | Month/year for tracking |
| count | INTEGER | NOT NULL | 0 | Email count |
| last_reset | TIMESTAMP WITH TIME ZONE | NOT NULL | NOW() | Last reset time |

**Indexes:**
- idx_email_usage_month_year (month_year)

---

## Hash Management

### clients

Stores information about clients for whom hashlists are processed.

| Column | Type | Constraints | Default | Description |
|--------|------|-------------|---------|-------------|
| id | UUID | PRIMARY KEY | gen_random_uuid() | Client identifier |
| name | VARCHAR(255) | NOT NULL, UNIQUE | | Client name |
| description | TEXT | | | Client description |
| contact_info | TEXT | | | Contact information |
| created_at | TIMESTAMPTZ | NOT NULL | NOW() | Creation time |
| updated_at | TIMESTAMPTZ | NOT NULL | NOW() | Last update time |
| data_retention_months | INT | | NULL | Data retention policy (NULL = system default, 0 = keep forever) |

**Data Retention Notes:**
- `data_retention_months` overrides system default retention policy
- NULL means use system default (`client_settings.default_data_retention_months`)
- 0 means keep data forever (no automatic deletion)
- Positive integers specify months to retain data after creation
- When retention period expires, hashlists and associated data are securely deleted

**Indexes:**
- idx_clients_name (name)

**Triggers:**
- update_clients_updated_at: Updates updated_at on row modification

### hash_types

Stores information about supported hash types, keyed by hashcat mode ID.

| Column | Type | Constraints | Default | Description |
|--------|------|-------------|---------|-------------|
| id | INT | PRIMARY KEY | | Hashcat mode number |
| name | VARCHAR(255) | NOT NULL | | Hash type name |
| description | TEXT | | | Hash type description |
| example | TEXT | | | Example hash |
| needs_processing | BOOLEAN | NOT NULL | FALSE | Requires preprocessing |
| processing_logic | JSONB | | | Processing rules as JSON |
| is_enabled | BOOLEAN | NOT NULL | TRUE | Hash type enabled |
| slow | BOOLEAN | NOT NULL | FALSE | Slow hash algorithm |

### hashlists

Stores metadata about uploaded hash lists.

| Column | Type | Constraints | Default | Description |
|--------|------|-------------|---------|-------------|
| id | BIGSERIAL | PRIMARY KEY | | Hashlist identifier |
| name | VARCHAR(255) | NOT NULL | | Hashlist name |
| user_id | UUID | NOT NULL, FK → users(id) | | Owner user |
| client_id | UUID | FK → clients(id) | | Associated client |
| hash_type_id | INT | NOT NULL, FK → hash_types(id) | | Hash type |
| file_path | VARCHAR(1024) | | | File storage path |
| total_hashes | INT | NOT NULL | 0 | Total hash count |
| cracked_hashes | INT | NOT NULL | 0 | Cracked hash count |
| created_at | TIMESTAMPTZ | NOT NULL | NOW() | Creation time |
| updated_at | TIMESTAMPTZ | NOT NULL | NOW() | Last update time |
| status | TEXT | NOT NULL, CHECK | | Status: uploading, processing, ready, error |
| error_message | TEXT | | | Error details |

**Retention & Deletion Behavior:**
- Deletion is CASCADE - removing a hashlist deletes:
  - All associations in `hashlist_hashes`
  - Related `agent_hashlists` entries
  - Related `job_executions` and their `job_tasks`
- File at `file_path` is securely overwritten with random data before deletion
- Orphaned hashes (not linked to any other hashlist) are automatically deleted
- VACUUM ANALYZE runs after deletion to prevent WAL recovery

**Indexes:**
- idx_hashlists_user_id (user_id)
- idx_hashlists_client_id (client_id)
- idx_hashlists_hash_type_id (hash_type_id)
- idx_hashlists_status (status)

**Triggers:**
- update_hashlists_updated_at: Updates updated_at on row modification

### hashes

Stores individual hash entries.

| Column | Type | Constraints | Default | Description |
|--------|------|-------------|---------|-------------|
| id | UUID | PRIMARY KEY | gen_random_uuid() | Hash identifier |
| hash_value | TEXT | NOT NULL | | Hash value |
| original_hash | TEXT | | | Original hash if processed |
| username | TEXT | | | Associated username |
| hash_type_id | INT | NOT NULL, FK → hash_types(id) | | Hash type |
| is_cracked | BOOLEAN | NOT NULL | FALSE | Crack status |
| password | TEXT | | | Cracked password |
| last_updated | TIMESTAMPTZ | NOT NULL | NOW() | Last update time |
| cracked_by_task_id | UUID | FK → job_tasks(id) ON DELETE SET NULL | | Task that cracked this hash (added in migration 098) |

**Indexes:**
- idx_hashes_hash_value (hash_value)
- idx_hashes_original_hash_unique (original_hash) UNIQUE - Fast deduplication during bulk import (added in migration 096)
- idx_hashes_cracked_by_task_id (cracked_by_task_id) WHERE cracked_by_task_id IS NOT NULL - Crack attribution lookup (added in migration 098)

**Triggers:**
- update_hashes_last_updated: Updates last_updated on row modification

### hashlist_hashes

Junction table for the many-to-many relationship between hashlists and hashes.

| Column | Type | Constraints | Default | Description |
|--------|------|-------------|---------|-------------|
| hashlist_id | BIGINT | NOT NULL, FK → hashlists(id) | | Hashlist reference |
| hash_id | UUID | NOT NULL, FK → hashes(id) | | Hash reference |

**Primary Key:** (hashlist_id, hash_id)

**Indexes:**
- idx_hashlist_hashes_hashlist_id (hashlist_id)
- idx_hashlist_hashes_hash_id (hash_id)

### hashcat_hash_types

Stores hashcat-specific hash type information (added in migration 16).

| Column | Type | Constraints | Default | Description |
|--------|------|-------------|---------|-------------|
| mode | INT | PRIMARY KEY | | Hashcat mode number |
| name | VARCHAR(255) | NOT NULL | | Hash type name |
| category | VARCHAR(100) | | | Hash category |
| slow_hash | BOOLEAN | | FALSE | Is slow hash |
| password_length_min | INT | | | Minimum password length |
| password_length_max | INT | | | Maximum password length |
| supports_brain | BOOLEAN | | FALSE | Supports brain feature |
| example_hash_format | TEXT | | | Example hash format |
| benchmark_mask | VARCHAR(255) | | | Benchmark mask |
| benchmark_charset1 | VARCHAR(255) | | | Benchmark charset 1 |
| autodetect_regex | TEXT | | | Regex for autodetection |
| potfile_regex | TEXT | | | Regex for potfile parsing |
| test_hash | TEXT | | | Test hash value |
| test_password | VARCHAR(255) | | | Test password |
| valid_hash_regex | TEXT | | | Valid hash format regex |

---

## LM/NTLM Support (v1.2.1+)

### lm_hash_metadata

Tracks partial crack status for LM hashes (hash type 3000). This table is only populated for LM hashes and has zero impact on other hash types.

**Purpose**: LM hashes consist of two 7-character halves that can be cracked independently. This table tracks the crack status of each half and stores the password fragments.

| Column | Type | Constraints | Default | Description |
|--------|------|-------------|---------|-------------|
| hash_id | UUID | PRIMARY KEY, FK → hashes(id) | | Reference to parent hash record |
| first_half_cracked | BOOLEAN | NOT NULL | FALSE | True if first 16 chars of LM hash cracked |
| second_half_cracked | BOOLEAN | NOT NULL | FALSE | True if last 16 chars of LM hash cracked |
| first_half_password | VARCHAR(7) | | NULL | Password for first half (max 7 chars) |
| second_half_password | VARCHAR(7) | | NULL | Password for second half (max 7 chars) |
| created_at | TIMESTAMP | NOT NULL | CURRENT_TIMESTAMP | Record creation timestamp |
| updated_at | TIMESTAMP | NOT NULL | CURRENT_TIMESTAMP | Last update timestamp |

**Indexes:**
- `PRIMARY KEY (hash_id)`
- `idx_lm_metadata_crack_status (first_half_cracked, second_half_cracked)` - Fast partial crack queries
- `idx_lm_metadata_hash_id (hash_id)` - Foreign key lookup

**ON DELETE**: CASCADE - When parent hash is deleted, metadata is automatically removed

**Use Cases:**
- Track partial crack progress: "First half cracked, second half pending"
- Analytics: Count of partially cracked LM hashes
- Strategic intelligence: Keyspace reduction from 95^14 to 95^7 when one half known
- LM-to-NTLM mask generation from partial crack patterns

**Example Query - Find Partial Cracks:**
```sql
SELECT h.username, h.domain,
       lm.first_half_password, lm.second_half_password
FROM lm_hash_metadata lm
INNER JOIN hashes h ON lm.hash_id = h.id
WHERE (lm.first_half_cracked OR lm.second_half_cracked)
  AND NOT (lm.first_half_cracked AND lm.second_half_cracked);
```

### linked_hashlists

Manages relationships between entire hashlists (e.g., LM hashlist ↔ NTLM hashlist from same pwdump file).

**Purpose**: Track which hashlists are related to enable analytics calculations and proper hashlist counting.

| Column | Type | Constraints | Default | Description |
|--------|------|-------------|---------|-------------|
| id | UUID | PRIMARY KEY | gen_random_uuid() | Unique link identifier |
| hashlist_id_1 | BIGINT | NOT NULL, FK → hashlists(id) | | First hashlist in relationship |
| hashlist_id_2 | BIGINT | NOT NULL, FK → hashlists(id) | | Second hashlist in relationship |
| link_type | VARCHAR(50) | NOT NULL | | Type of relationship (e.g., 'lm_ntlm') |
| created_at | TIMESTAMP | NOT NULL | CURRENT_TIMESTAMP | Link creation timestamp |

**Constraints:**
- `UNIQUE (hashlist_id_1, hashlist_id_2)` - Prevents duplicate links
- `CHECK (hashlist_id_1 != hashlist_id_2)` - Prevents self-linking

**Indexes:**
- `PRIMARY KEY (id)`
- `idx_linked_hashlists_id2 (hashlist_id_2)` - Bidirectional lookup
- `idx_linked_hashlists_type (link_type)` - Filter by link type

**ON DELETE**: CASCADE - When either hashlist is deleted, link is automatically removed

**Link Types:**
- `lm_ntlm`: LM and NTLM hashlists from same pwdump file

**Use Cases:**
- Analytics: Count linked pairs as ONE hashlist (prevent double-counting)
- Determine when to create individual hash-to-hash links
- Track which hashlists were created together

**Example Query - Find Linked Hashlists:**
```sql
SELECT hl1.name AS lm_hashlist, hl2.name AS ntlm_hashlist
FROM linked_hashlists lh
INNER JOIN hashlists hl1 ON lh.hashlist_id_1 = hl1.id
INNER JOIN hashlists hl2 ON lh.hashlist_id_2 = hl2.id
WHERE lh.link_type = 'lm_ntlm';
```

### linked_hashes

Manages relationships between individual hash records (e.g., specific LM hash ↔ specific NTLM hash for same user).

**Purpose**: Enable correlation analysis showing which users have both LM and NTLM hashes cracked, partially cracked, or uncracked.

| Column | Type | Constraints | Default | Description |
|--------|------|-------------|---------|-------------|
| id | UUID | PRIMARY KEY | gen_random_uuid() | Unique link identifier |
| hash_id_1 | UUID | NOT NULL, FK → hashes(id) | | First hash in relationship (typically LM) |
| hash_id_2 | UUID | NOT NULL, FK → hashes(id) | | Second hash in relationship (typically NTLM) |
| link_type | VARCHAR(50) | NOT NULL | | Type of relationship (e.g., 'lm_ntlm') |
| created_at | TIMESTAMP | NOT NULL | CURRENT_TIMESTAMP | Link creation timestamp |

**Constraints:**
- `UNIQUE (hash_id_1, hash_id_2)` - Prevents duplicate links
- `CHECK (hash_id_1 != hash_id_2)` - Prevents self-linking

**Indexes:**
- `PRIMARY KEY (id)`
- `idx_linked_hashes_id2 (hash_id_2)` - Bidirectional lookup
- `idx_linked_hashes_type (link_type)` - Filter by link type

**ON DELETE**: CASCADE - When either hash is deleted, link is automatically removed

**Link Types:**
- `lm_ntlm`: LM and NTLM hashes for same username/domain

**Linking Strategy:**
Links are created by matching `username` and `domain` columns in the `hashes` table. This approach handles:
- Domain migrations (links persist across RID changes)
- Account renames (links updated if username changes)
- Multi-domain environments (links only within same domain)

**Use Cases:**
- Analytics: "Linked Hash Correlation" statistics
- Show: "Administrator's LM cracked but NTLM still unknown"
- Domain-filtered correlation analysis
- Identify high-value targets (both hashes cracked = full compromise)

**Example Query - Correlation Statistics:**
```sql
SELECT
    COUNT(*) AS total_pairs,
    COUNT(CASE WHEN lm.is_cracked AND ntlm.is_cracked THEN 1 END) AS both_cracked,
    COUNT(CASE WHEN NOT lm.is_cracked AND ntlm.is_cracked THEN 1 END) AS only_ntlm,
    COUNT(CASE WHEN lm.is_cracked AND NOT ntlm.is_cracked THEN 1 END) AS only_lm,
    COUNT(CASE WHEN NOT lm.is_cracked AND NOT ntlm.is_cracked THEN 1 END) AS neither
FROM linked_hashes lh
INNER JOIN hashes lm ON lh.hash_id_1 = lm.id
INNER JOIN hashes ntlm ON lh.hash_id_2 = ntlm.id
WHERE lh.link_type = 'lm_ntlm';
```

---

## Job Management

### preset_jobs

Stores predefined job configurations.

| Column | Type | Constraints | Default | Description |
|--------|------|-------------|---------|-------------|
| id | UUID | PRIMARY KEY | uuid_generate_v4() | Job identifier |
| name | TEXT | UNIQUE NOT NULL | | Job name |
| wordlist_ids | JSONB | NOT NULL | '[]' | Array of wordlist IDs |
| rule_ids | JSONB | NOT NULL | '[]' | Array of rule IDs |
| attack_mode | INTEGER | NOT NULL, CHECK | 0 | Attack mode: 0,1,3,6,7,9 |
| priority | INTEGER | NOT NULL | | Job priority |
| chunk_size_seconds | INTEGER | NOT NULL | | Chunk duration |
| status_updates_enabled | BOOLEAN | NOT NULL | true | Enable status updates |
| is_small_job | BOOLEAN | NOT NULL | false | Small job flag |
| allow_high_priority_override | BOOLEAN | NOT NULL | false | Allows this job to interrupt lower priority running jobs when no agents available |
| binary_version_id | INTEGER | NOT NULL, FK → binary_versions(id) | | Binary version |
| mask | TEXT | | NULL | Mask pattern |
| created_at | TIMESTAMPTZ | | NOW() | Creation time |
| updated_at | TIMESTAMPTZ | | NOW() | Last update time |
| keyspace_limit | BIGINT | | | Keyspace limit (added in migration 32) |
| max_agents | INTEGER | | | Max agents allowed (added in migration 32) |

**Triggers:**
- update_preset_jobs_updated_at: Updates updated_at on row modification

### job_workflows

Stores workflow definitions for multi-step attacks.

| Column | Type | Constraints | Default | Description |
|--------|------|-------------|---------|-------------|
| id | UUID | PRIMARY KEY | uuid_generate_v4() | Workflow identifier |
| name | TEXT | UNIQUE NOT NULL | | Workflow name |
| created_at | TIMESTAMPTZ | | NOW() | Creation time |
| updated_at | TIMESTAMPTZ | | NOW() | Last update time |

**Triggers:**
- update_job_workflows_updated_at: Updates updated_at on row modification

### job_workflow_steps

Defines steps within a workflow.

| Column | Type | Constraints | Default | Description |
|--------|------|-------------|---------|-------------|
| id | BIGSERIAL | PRIMARY KEY | | Step identifier |
| job_workflow_id | UUID | NOT NULL, FK → job_workflows(id) | | Workflow reference |
| preset_job_id | UUID | NOT NULL, FK → preset_jobs(id) | | Preset job reference |
| step_order | INTEGER | NOT NULL | | Execution order |

**Unique Constraint:** (job_workflow_id, step_order)

**Indexes:**
- idx_job_workflow_steps_job_workflow_id (job_workflow_id)
- idx_job_workflow_steps_preset_job_id (preset_job_id)

### job_executions

Tracks actual job runs.

| Column | Type | Constraints | Default | Description |
|--------|------|-------------|---------|-------------|
| id | UUID | PRIMARY KEY | gen_random_uuid() | Execution identifier |
| preset_job_id | UUID | NOT NULL, FK → preset_jobs(id) | | Preset job reference |
| hashlist_id | BIGINT | NOT NULL, FK → hashlists(id) | | Hashlist reference |
| status | VARCHAR(50) | NOT NULL, CHECK | 'pending' | Status: pending, running, paused, processing, completed, failed, cancelled (added migration 085: processing status) |
| priority | INT | NOT NULL | 0 | Execution priority |
| total_keyspace | BIGINT | | | Total keyspace size |
| processed_keyspace | BIGINT | | 0 | Processed keyspace |
| attack_mode | INT | NOT NULL | | Attack mode |
| created_at | TIMESTAMP WITH TIME ZONE | | CURRENT_TIMESTAMP | Creation time |
| started_at | TIMESTAMP WITH TIME ZONE | | | Start time |
| completed_at | TIMESTAMP WITH TIME ZONE | | | Completion time |
| error_message | TEXT | | | Error details |
| interrupted_by | UUID | FK → job_executions(id) | | ID of the higher priority job that interrupted this one |
| created_by | UUID | FK → users(id) | | Creator user (added in migration 33) |
| chunk_size | INTEGER | | | Chunk size override (added in migration 34) |
| chunk_overlap | INTEGER | | 0 | Chunk overlap (added in migration 34) |
| dispatched_keyspace | BIGINT | | 0 | Dispatched keyspace (added in migration 40) |
| progress | NUMERIC(6,3) | | 0 | Progress percentage (added in migration 36, updated in migration 38) |
| consecutive_failures | INTEGER | | 0 | Consecutive failure count (added in migration 37) |
| last_failure_at | TIMESTAMP WITH TIME ZONE | | | Last failure time (added in migration 37) |
| is_accurate_keyspace | BOOLEAN | | false | True when keyspace is from hashcat progress[1] values (added in migration 63) |
| avg_rule_multiplier | FLOAT | | | Actual/estimated keyspace ratio for improving future estimates (added in migration 63) |
| completion_email_sent | BOOLEAN | | false | Whether completion email was sent (added in migration 085) |
| completion_email_sent_at | TIMESTAMP WITH TIME ZONE | | | When completion email was sent (added in migration 085) |
| completion_email_error | TEXT | | | Error message if email sending failed (added in migration 085) |
| cracking_completed_at | TIMESTAMP WITH TIME ZONE | | | When all tasks finished hashcat processing - job enters processing state (added in migration 100) |

**Indexes:**
- idx_job_executions_status (status)
- idx_job_executions_priority (priority, created_at)
- idx_job_executions_created_by (created_by)
- idx_job_executions_consecutive_failures (consecutive_failures)

### job_tasks

Individual chunks assigned to agents.

| Column | Type | Constraints | Default | Description |
|--------|------|-------------|---------|-------------|
| id | UUID | PRIMARY KEY | gen_random_uuid() | Task identifier |
| job_execution_id | UUID | NOT NULL, FK → job_executions(id) | | Job execution reference |
| agent_id | INTEGER | FK → agents(id) | | Assigned agent (nullable in migration 35) |
| status | VARCHAR(50) | NOT NULL, CHECK | 'pending' | Status: pending, assigned, reconnect_pending, running, processing, completed, failed, cancelled (added migration 085: processing status) |
| keyspace_start | BIGINT | NOT NULL | | Keyspace start |
| keyspace_end | BIGINT | NOT NULL | | Keyspace end |
| keyspace_processed | BIGINT | | 0 | Processed amount |
| benchmark_speed | BIGINT | | | Hashes per second |
| chunk_duration | INT | NOT NULL | | Duration in seconds |
| assigned_at | TIMESTAMP WITH TIME ZONE | | CURRENT_TIMESTAMP | Assignment time |
| started_at | TIMESTAMP WITH TIME ZONE | | | Start time |
| completed_at | TIMESTAMP WITH TIME ZONE | | | Completion time |
| last_checkpoint | TIMESTAMP WITH TIME ZONE | | | Last checkpoint |
| error_message | TEXT | | | Error details |
| created_at | TIMESTAMP WITH TIME ZONE | NOT NULL | CURRENT_TIMESTAMP | Creation time (added in migration 25) |
| updated_at | TIMESTAMP WITH TIME ZONE | NOT NULL | CURRENT_TIMESTAMP | Last update time (added in migration 26) |
| progress | NUMERIC(6,3) | | 0 | Progress percentage (added in migration 36, updated in migration 38) |
| consecutive_failures | INTEGER | | 0 | Consecutive failure count (added in migration 37) |
| last_failure_at | TIMESTAMP WITH TIME ZONE | | | Last failure time (added in migration 37) |
| chunk_number | INTEGER | | | Chunk number for rule splits (added in migration 44) |
| effective_keyspace | BIGINT | | | Effective keyspace size (added in migration 47) |
| is_actual_keyspace | BOOLEAN | | false | True when task has actual keyspace from hashcat progress[1] (added in migration 63) |
| chunk_actual_keyspace | BIGINT | | | Immutable chunk size from hashcat progress[1] for accurate keyspace tracking (added in migration 64) |
| crack_count | INTEGER | | 0 | Number of hashes cracked by this task (existing field) |
| expected_crack_count | INTEGER | | 0 | Expected number of cracks from final progress message (added in migration 085) |
| received_crack_count | INTEGER | | 0 | Number of cracks received via crack_batch messages (added in migration 085) |
| batches_complete_signaled | BOOLEAN | | false | Whether agent has signaled all crack batches sent (added in migration 085) |
| increment_layer_id | UUID | FK → job_increment_layers(id) | | References increment layer for increment mode jobs (added in migration 089) |
| cracking_completed_at | TIMESTAMP WITH TIME ZONE | | | When hashcat finished for this task - task enters processing state (added in migration 100) |
| retransmit_count | INTEGER | | 0 | Number of crack retransmission attempts (added in migration 099) |
| last_retransmit_at | TIMESTAMP WITH TIME ZONE | | | Timestamp of last retransmission request (added in migration 099) |

**Indexes:**
- idx_job_tasks_agent_status (agent_id, status)
- idx_job_tasks_execution (job_execution_id)
- idx_job_tasks_consecutive_failures (consecutive_failures)
- idx_job_tasks_chunk_number (job_execution_id, chunk_number)
- idx_job_tasks_increment_layer (increment_layer_id) - added in migration 089
- idx_job_tasks_cracking_completed_at (cracking_completed_at) WHERE cracking_completed_at IS NOT NULL - Efficient completion state queries (added in migration 100)

**Triggers:**
- update_job_tasks_updated_at: Updates updated_at on row modification

### job_increment_layers

Sub-layers for increment mode jobs. Each layer represents one mask length in the increment sequence. Added in migration 088.

| Column | Type | Constraints | Default | Description |
|--------|------|-------------|---------|-------------|
| id | UUID | PRIMARY KEY | gen_random_uuid() | Layer identifier |
| job_execution_id | UUID | NOT NULL, FK → job_executions(id) ON DELETE CASCADE | | Parent job execution |
| layer_index | INT | NOT NULL | | Order in sequence (1=first) |
| mask | VARCHAR(255) | NOT NULL | | Layer-specific mask (e.g., `?l?l`) |
| status | VARCHAR(50) | NOT NULL, CHECK | 'pending' | Status: pending, running, completed, failed, cancelled |
| base_keyspace | BIGINT | | | Estimated keyspace from --keyspace |
| effective_keyspace | BIGINT | | | Actual keyspace from benchmark |
| processed_keyspace | BIGINT | | 0 | Completed keyspace |
| dispatched_keyspace | BIGINT | | 0 | Assigned keyspace |
| is_accurate_keyspace | BOOLEAN | | FALSE | TRUE after benchmark provides actual keyspace |
| overall_progress_percent | NUMERIC(5,2) | | 0.00 | Layer completion percentage |
| created_at | TIMESTAMP WITH TIME ZONE | NOT NULL | CURRENT_TIMESTAMP | Creation time |
| updated_at | TIMESTAMP WITH TIME ZONE | NOT NULL | CURRENT_TIMESTAMP | Last update time |
| started_at | TIMESTAMP WITH TIME ZONE | | | Layer start time |
| completed_at | TIMESTAMP WITH TIME ZONE | | | Layer completion time |
| error_message | TEXT | | | Error details if failed |

**Unique Constraint:** (job_execution_id, layer_index)

**Indexes:**
- idx_job_increment_layers_execution (job_execution_id)
- idx_job_increment_layers_status (status)

**Triggers:**
- update_job_increment_layers_updated_at: Updates updated_at on row modification

**Purpose:**
- Decomposes increment mode jobs into discrete layers for distributed processing
- Each layer can be scheduled and tracked independently
- Multiple agents can work on different layers simultaneously
- Provides granular progress tracking per mask length

**Use Cases:**
- Track progress for each mask length in an increment mode attack
- Enable parallel processing of different mask lengths
- Provide detailed status per layer in the UI

**Example Query - Layer Progress:**
```sql
SELECT layer_index, mask, status,
       overall_progress_percent,
       processed_keyspace, effective_keyspace
FROM job_increment_layers
WHERE job_execution_id = 'uuid-here'
ORDER BY layer_index;
```

See [Increment Mode Architecture](architecture/increment-mode.md) for implementation details.

### preset_increment_layers

Pre-calculated increment layers for preset jobs. When a job is created from a preset with increment mode enabled, these layers are copied to `job_increment_layers`. Added in migration 090.

| Column | Type | Constraints | Default | Description |
|--------|------|-------------|---------|-------------|
| id | UUID | PRIMARY KEY | gen_random_uuid() | Layer identifier |
| preset_job_id | UUID | NOT NULL, FK → preset_jobs(id) ON DELETE CASCADE | | Parent preset job |
| layer_index | INT | NOT NULL | | Order in sequence (1=first) |
| mask | VARCHAR(512) | NOT NULL | | Layer-specific mask (e.g., `?l?l`) |
| base_keyspace | BIGINT | | | Estimated keyspace from --keyspace |
| effective_keyspace | BIGINT | | | Calculated keyspace |
| created_at | TIMESTAMP WITH TIME ZONE | NOT NULL | CURRENT_TIMESTAMP | Creation time |
| updated_at | TIMESTAMP WITH TIME ZONE | NOT NULL | CURRENT_TIMESTAMP | Last update time |

**Unique Constraint:** (preset_job_id, layer_index)

**Indexes:**
- idx_preset_increment_layers_preset_job_id (preset_job_id)

**Triggers:**
- update_preset_increment_layers_updated_at: Updates updated_at on row modification

**Purpose:**
- Pre-calculate layers at preset creation time rather than job creation time
- Ensures consistent keyspace calculations across all jobs created from the same preset
- Faster job creation (no need to re-run hashcat --keyspace for each layer)
- Preset keyspace = sum of all layer effective_keyspaces

**Data Flow:**
1. Admin creates preset job with increment mode → `preset_increment_layers` populated
2. User creates job from preset → layers copied from `preset_increment_layers` to `job_increment_layers`
3. Job inherits preset's total keyspace

See [Increment Mode Architecture](architecture/increment-mode.md) for implementation details.

### job_execution_settings

Settings for job executions (added in migration 21).

| Column | Type | Constraints | Default | Description |
|--------|------|-------------|---------|-------------|
| id | SERIAL | PRIMARY KEY | | Settings ID |
| name | VARCHAR(255) | NOT NULL, UNIQUE | | Setting name |
| value | TEXT | NOT NULL | | Setting value |
| description | TEXT | | | Setting description |
| data_type | VARCHAR(50) | NOT NULL | 'string' | Data type |
| created_at | TIMESTAMP WITH TIME ZONE | | CURRENT_TIMESTAMP | Creation time |
| updated_at | TIMESTAMP WITH TIME ZONE | | CURRENT_TIMESTAMP | Last update time |

**Indexes:**
- idx_job_execution_settings_name (name)

**Triggers:**
- update_job_execution_settings_updated_at: Updates updated_at on row modification

---

## Resource Management

### binary_versions

Stores information about different versions of hash cracking binaries. Supports both URL downloads and direct uploads.

| Column | Type | Constraints | Default | Description |
|--------|------|-------------|---------|-------------|
| id | SERIAL | PRIMARY KEY | | Version ID |
| binary_type | binary_type | NOT NULL | | Type: hashcat, john |
| compression_type | compression_type | NOT NULL | | Compression: 7z, zip, tar.gz, tar.xz |
| source_type | VARCHAR(50) | NOT NULL | 'url' | Source type: 'url' or 'upload' |
| source_url | TEXT | | | Download URL (NULL for uploads) |
| file_name | VARCHAR(255) | NOT NULL | | File name |
| md5_hash | VARCHAR(32) | NOT NULL | | MD5 hash |
| file_size | BIGINT | NOT NULL | | File size in bytes |
| version | VARCHAR(100) | | | Version string (e.g., "6.2.6", "7.1.2+338") |
| description | TEXT | | | Human-readable description |
| created_at | TIMESTAMP WITH TIME ZONE | | CURRENT_TIMESTAMP | Creation time |
| created_by | UUID | NOT NULL, FK → users(id) | | Creator user |
| is_active | BOOLEAN | | true | Active status |
| is_default | BOOLEAN | | false | Whether this is the default version |
| last_verified_at | TIMESTAMP WITH TIME ZONE | | | Last verification time |
| verification_status | VARCHAR(50) | | 'pending' | Status: pending, verified, failed, deleted |

**Indexes:**
- idx_binary_versions_type_active (binary_type) WHERE is_active = true
- idx_binary_versions_verification (verification_status)

### binary_version_audit_log

Tracks all changes and actions performed on binary versions.

| Column | Type | Constraints | Default | Description |
|--------|------|-------------|---------|-------------|
| id | SERIAL | PRIMARY KEY | | Audit log ID |
| binary_version_id | INTEGER | NOT NULL, FK → binary_versions(id) | | Binary version reference |
| action | VARCHAR(50) | NOT NULL | | Action performed |
| performed_by | UUID | NOT NULL, FK → users(id) | | User who performed action |
| performed_at | TIMESTAMP WITH TIME ZONE | | CURRENT_TIMESTAMP | Action timestamp |
| details | JSONB | | | Additional details |

**Indexes:**
- idx_binary_version_audit_binary_id (binary_version_id)
- idx_binary_version_audit_performed_at (performed_at)

### wordlists

Stores information about wordlists used for password cracking.

| Column | Type | Constraints | Default | Description |
|--------|------|-------------|---------|-------------|
| id | SERIAL | PRIMARY KEY | | Wordlist ID |
| name | VARCHAR(255) | NOT NULL | | Wordlist name |
| description | TEXT | | | Description |
| wordlist_type | wordlist_type | NOT NULL | | Type: general, specialized, targeted, custom |
| format | wordlist_format | NOT NULL | 'plaintext' | Format: plaintext, compressed |
| file_name | VARCHAR(255) | NOT NULL | | File name |
| md5_hash | VARCHAR(32) | NOT NULL | | MD5 hash |
| file_size | BIGINT | NOT NULL | | File size in bytes |
| word_count | BIGINT | | | Number of words |
| created_at | TIMESTAMP WITH TIME ZONE | | CURRENT_TIMESTAMP | Creation time |
| created_by | UUID | NOT NULL, FK → users(id) | | Creator user |
| updated_at | TIMESTAMP WITH TIME ZONE | | CURRENT_TIMESTAMP | Last update time |
| updated_by | UUID | FK → users(id) | | Last updater |
| last_verified_at | TIMESTAMP WITH TIME ZONE | | | Last verification time |
| verification_status | VARCHAR(50) | | 'pending' | Status: pending, verified, failed |

**Indexes:**
- idx_wordlists_name (name)
- idx_wordlists_type (wordlist_type)
- idx_wordlists_verification (verification_status)
- idx_wordlists_md5 (md5_hash)

### wordlist_audit_log

Tracks all changes and actions performed on wordlists.

| Column | Type | Constraints | Default | Description |
|--------|------|-------------|---------|-------------|
| id | SERIAL | PRIMARY KEY | | Audit log ID |
| wordlist_id | INTEGER | NOT NULL, FK → wordlists(id) | | Wordlist reference |
| action | VARCHAR(50) | NOT NULL | | Action performed |
| performed_by | UUID | NOT NULL, FK → users(id) | | User who performed action |
| performed_at | TIMESTAMP WITH TIME ZONE | | CURRENT_TIMESTAMP | Action timestamp |
| details | JSONB | | | Additional details |

**Indexes:**
- idx_wordlist_audit_wordlist_id (wordlist_id)
- idx_wordlist_audit_performed_at (performed_at)

### wordlist_tags

Stores tags associated with wordlists.

| Column | Type | Constraints | Default | Description |
|--------|------|-------------|---------|-------------|
| id | SERIAL | PRIMARY KEY | | Tag ID |
| wordlist_id | INTEGER | NOT NULL, FK → wordlists(id) | | Wordlist reference |
| tag | VARCHAR(50) | NOT NULL | | Tag value |
| created_at | TIMESTAMP WITH TIME ZONE | | CURRENT_TIMESTAMP | Creation time |
| created_by | UUID | NOT NULL, FK → users(id) | | Creator user |

**Unique Index:** idx_wordlist_tags_unique (wordlist_id, tag)

**Indexes:**
- idx_wordlist_tags_tag (tag)

### rules

Stores information about rules used for password cracking.

| Column | Type | Constraints | Default | Description |
|--------|------|-------------|---------|-------------|
| id | SERIAL | PRIMARY KEY | | Rule ID |
| name | VARCHAR(255) | NOT NULL | | Rule name |
| description | TEXT | | | Description |
| rule_type | rule_type | NOT NULL | | Type: hashcat, john |
| file_name | VARCHAR(255) | NOT NULL | | File name |
| md5_hash | VARCHAR(32) | NOT NULL | | MD5 hash |
| file_size | BIGINT | NOT NULL | | File size in bytes |
| rule_count | INTEGER | | | Number of rules |
| created_at | TIMESTAMP WITH TIME ZONE | | CURRENT_TIMESTAMP | Creation time |
| created_by | UUID | NOT NULL, FK → users(id) | | Creator user |
| updated_at | TIMESTAMP WITH TIME ZONE | | CURRENT_TIMESTAMP | Last update time |
| updated_by | UUID | FK → users(id) | | Last updater |
| last_verified_at | TIMESTAMP WITH TIME ZONE | | | Last verification time |
| verification_status | VARCHAR(50) | | 'pending' | Status: pending, verified, failed |
| estimated_keyspace_multiplier | FLOAT | | | Keyspace multiplier estimate |

**Indexes:**
- idx_rules_name (name)
- idx_rules_type (rule_type)
- idx_rules_verification (verification_status)
- idx_rules_md5 (md5_hash)

### rule_audit_log

Tracks all changes and actions performed on rules.

| Column | Type | Constraints | Default | Description |
|--------|------|-------------|---------|-------------|
| id | SERIAL | PRIMARY KEY | | Audit log ID |
| rule_id | INTEGER | NOT NULL, FK → rules(id) | | Rule reference |
| action | VARCHAR(50) | NOT NULL | | Action performed |
| performed_by | UUID | NOT NULL, FK → users(id) | | User who performed action |
| performed_at | TIMESTAMP WITH TIME ZONE | | CURRENT_TIMESTAMP | Action timestamp |
| details | JSONB | | | Additional details |

**Indexes:**
- idx_rule_audit_rule_id (rule_id)
- idx_rule_audit_performed_at (performed_at)

### rule_tags

Stores tags associated with rules.

| Column | Type | Constraints | Default | Description |
|--------|------|-------------|---------|-------------|
| id | SERIAL | PRIMARY KEY | | Tag ID |
| rule_id | INTEGER | NOT NULL, FK → rules(id) | | Rule reference |
| tag | VARCHAR(50) | NOT NULL | | Tag value |
| created_at | TIMESTAMP WITH TIME ZONE | | CURRENT_TIMESTAMP | Creation time |
| created_by | UUID | NOT NULL, FK → users(id) | | Creator user |

**Unique Index:** idx_rule_tags_unique (rule_id, tag)

**Indexes:**
- idx_rule_tags_tag (tag)

### rule_wordlist_compatibility

Stores compatibility information between rules and wordlists.

| Column | Type | Constraints | Default | Description |
|--------|------|-------------|---------|-------------|
| id | SERIAL | PRIMARY KEY | | Compatibility ID |
| rule_id | INTEGER | NOT NULL, FK → rules(id) | | Rule reference |
| wordlist_id | INTEGER | NOT NULL, FK → wordlists(id) | | Wordlist reference |
| compatibility_score | FLOAT | NOT NULL | 1.0 | Score from 0.0 to 1.0 |
| notes | TEXT | | | Compatibility notes |
| created_at | TIMESTAMP WITH TIME ZONE | | CURRENT_TIMESTAMP | Creation time |
| created_by | UUID | NOT NULL, FK → users(id) | | Creator user |
| updated_at | TIMESTAMP WITH TIME ZONE | | CURRENT_TIMESTAMP | Last update time |
| updated_by | UUID | FK → users(id) | | Last updater |

**Unique Index:** idx_rule_wordlist_unique (rule_id, wordlist_id)

**Indexes:**
- idx_rule_wordlist_rule (rule_id)
- idx_rule_wordlist_wordlist (wordlist_id)

---

## Client & Settings

### client_settings

Stores client-specific settings (added in migration 17). Also used for system-wide settings without a client_id.

| Column | Type | Constraints | Default | Description |
|--------|------|-------------|---------|-------------|
| id | SERIAL | PRIMARY KEY | | Settings ID |
| client_id | UUID | NOT NULL, FK → clients(id) | | Client reference |
| key | VARCHAR(255) | NOT NULL | | Setting key |
| value | TEXT | | | Setting value |
| data_type | VARCHAR(50) | NOT NULL | 'string' | Data type |
| created_at | TIMESTAMP WITH TIME ZONE | | CURRENT_TIMESTAMP | Creation time |
| updated_at | TIMESTAMP WITH TIME ZONE | | CURRENT_TIMESTAMP | Last update time |

**Important System-Wide Settings:**
- `default_data_retention_months` - Default retention period for all hashlists (when client_id is NULL)
- `last_purge_run` - Timestamp of last retention purge execution

**Unique Constraint:** (client_id, key)

**Indexes:**
- idx_client_settings_client (client_id)

**Triggers:**
- update_client_settings_updated_at: Updates updated_at on row modification

### system_settings

Stores global system-wide settings.

| Column | Type | Constraints | Default | Description |
|--------|------|-------------|---------|-------------|
| key | VARCHAR(255) | PRIMARY KEY | | Setting key |
| value | TEXT | | | Setting value |
| description | TEXT | | | Setting description |
| data_type | VARCHAR(50) | NOT NULL | 'string' | Data type: string, integer, boolean, float |
| updated_at | TIMESTAMPTZ | NOT NULL | NOW() | Last update time |

**Default Settings:**
- max_job_priority: 1000 (integer)
- agent_scheduling_enabled: false (boolean) - added in migration 42
- hashcat_speedtest_timeout: 300 (integer) - added in migration 39
- task_heartbeat_timeout: 300 (integer) - added in migration 46
- agent_overflow_allocation_mode: 'fifo' (string) - added in migration 82
  - Values: 'fifo' (default) or 'round_robin'
  - Controls how overflow agents are distributed among same-priority jobs
  - FIFO: Oldest job gets all overflow agents
  - Round-robin: Distribute evenly across all jobs
- hashlist_bulk_batch_size: 100000 (integer) - added in migration 097
  - Number of hashes to process per batch during hashlist uploads
  - Higher values (500K-1M) may improve performance for large hashlists but use more memory
  - Lower values reduce memory usage but increase processing time

**Triggers:**
- update_system_settings_updated_at: Updates updated_at on row modification

---

## Performance & Scheduling

### benchmark_requests

Tracks parallel benchmark execution requests (added in migration 83).

| Column | Type | Constraints | Default | Description |
|--------|------|-------------|---------|-------------|
| id | UUID | PRIMARY KEY | gen_random_uuid() | Request identifier |
| agent_id | INTEGER | NOT NULL, FK → agents(id) ON DELETE CASCADE | | Agent reference |
| job_execution_id | UUID | FK → job_executions(id) ON DELETE CASCADE | | Job execution reference (for forced benchmarks) |
| hash_type | INTEGER | NOT NULL | | Hash type to benchmark |
| attack_mode | INTEGER | NOT NULL | | Attack mode to benchmark |
| benchmark_type | VARCHAR(50) | NOT NULL | | Type: 'forced' or 'agent_speed' |
| status | VARCHAR(50) | NOT NULL | 'pending' | Status: pending, completed, failed |
| requested_at | TIMESTAMP WITH TIME ZONE | NOT NULL | CURRENT_TIMESTAMP | Request timestamp |
| completed_at | TIMESTAMP WITH TIME ZONE | | | Completion timestamp |
| result | JSONB | | | Benchmark result data |
| created_at | TIMESTAMP WITH TIME ZONE | NOT NULL | CURRENT_TIMESTAMP | Creation time |

**Purpose:**
- Enables polling-based coordination of async WebSocket benchmarks
- Supports parallel benchmark execution for dramatic performance improvements
- Tracks both forced benchmarks (for accurate keyspace) and agent speed benchmarks
- Cleaned up after each scheduling cycle

**Benchmark Types:**
- **forced**: Run full hashcat benchmark with actual job configuration to obtain accurate keyspace
- **agent_speed**: Standard hashcat speed test to update agent performance metrics

**Indexes:**
- idx_benchmark_requests_status (status) WHERE status = 'pending'
- idx_benchmark_requests_agent (agent_id)
- idx_benchmark_requests_job (job_execution_id)

**Performance Impact:**
- **Before (Sequential)**: 15 agents × 30s = 450 seconds
- **After (Parallel)**: 15 agents in ~12 seconds
- **Improvement**: 96% reduction (37.5x faster)

### agent_benchmarks

Stores benchmark results for agents.

| Column | Type | Constraints | Default | Description |
|--------|------|-------------|---------|-------------|
| id | UUID | PRIMARY KEY | gen_random_uuid() | Benchmark ID |
| agent_id | INTEGER | NOT NULL, FK → agents(id) | | Agent reference |
| attack_mode | INT | NOT NULL | | Attack mode |
| hash_type | INT | NOT NULL | | Hash type |
| speed | BIGINT | NOT NULL | | Hashes per second |
| created_at | TIMESTAMP WITH TIME ZONE | | CURRENT_TIMESTAMP | Creation time |
| updated_at | TIMESTAMP WITH TIME ZONE | | CURRENT_TIMESTAMP | Last update time |

**Unique Constraint:** (agent_id, attack_mode, hash_type)

**Indexes:**
- idx_agent_benchmarks_lookup (agent_id, attack_mode, hash_type)

### agent_performance_metrics

Historical performance tracking for agents.

| Column | Type | Constraints | Default | Description |
|--------|------|-------------|---------|-------------|
| id | UUID | PRIMARY KEY | gen_random_uuid() | Metric ID |
| agent_id | INTEGER | NOT NULL, FK → agents(id) | | Agent reference |
| metric_type | VARCHAR(50) | NOT NULL, CHECK | | Type: hash_rate, utilization, temperature, power_usage |
| value | NUMERIC | NOT NULL | | Metric value |
| timestamp | TIMESTAMP WITH TIME ZONE | | CURRENT_TIMESTAMP | Metric timestamp |
| aggregation_level | VARCHAR(20) | NOT NULL, CHECK | 'realtime' | Level: realtime, daily, weekly |
| period_start | TIMESTAMP WITH TIME ZONE | | | Aggregation period start |
| period_end | TIMESTAMP WITH TIME ZONE | | | Aggregation period end |

**Indexes:**
- idx_agent_metrics_lookup (agent_id, metric_type, timestamp)
- idx_agent_metrics_aggregation (aggregation_level, timestamp)

### performance_metrics

Detailed performance metrics (added in migration 41).

| Column | Type | Constraints | Default | Description |
|--------|------|-------------|---------|-------------|
| id | UUID | PRIMARY KEY | gen_random_uuid() | Metric ID |
| job_task_id | UUID | FK → job_tasks(id) | | Job task reference |
| agent_id | INTEGER | NOT NULL, FK → agents(id) | | Agent reference |
| device_id | INTEGER | | | Device ID |
| device_name | VARCHAR(255) | | | Device name |
| timestamp | TIMESTAMP WITH TIME ZONE | NOT NULL | CURRENT_TIMESTAMP | Metric timestamp |
| hash_rate | BIGINT | | | Current hash rate |
| utilization | FLOAT | | | GPU utilization % |
| temperature | FLOAT | | | Temperature in Celsius |
| power_usage | FLOAT | | | Power usage in watts |
| memory_used | BIGINT | | | Memory used in bytes |
| memory_total | BIGINT | | | Total memory in bytes |
| fan_speed | FLOAT | | | Fan speed % |
| core_clock | INTEGER | | | Core clock in MHz |
| memory_clock | INTEGER | | | Memory clock in MHz |
| pcie_rx | BIGINT | | | PCIe RX throughput |
| pcie_tx | BIGINT | | | PCIe TX throughput |

**Indexes:**
- idx_performance_metrics_timestamp (timestamp)
- idx_performance_metrics_agent (agent_id, timestamp)
- idx_performance_metrics_job_task (job_task_id)
- idx_performance_metrics_device (agent_id, device_id, timestamp)

### job_performance_metrics

Job-level performance tracking.

| Column | Type | Constraints | Default | Description |
|--------|------|-------------|---------|-------------|
| id | UUID | PRIMARY KEY | gen_random_uuid() | Metric ID |
| job_execution_id | UUID | NOT NULL, FK → job_executions(id) | | Job execution reference |
| metric_type | VARCHAR(50) | NOT NULL, CHECK | | Type: hash_rate, progress_percentage, cracks_found |
| value | NUMERIC | NOT NULL | | Metric value |
| timestamp | TIMESTAMP WITH TIME ZONE | | CURRENT_TIMESTAMP | Metric timestamp |
| aggregation_level | VARCHAR(20) | NOT NULL, CHECK | 'realtime' | Level: realtime, daily, weekly |
| period_start | TIMESTAMP WITH TIME ZONE | | | Aggregation period start |
| period_end | TIMESTAMP WITH TIME ZONE | | | Aggregation period end |

**Indexes:**
- idx_job_metrics_lookup (job_execution_id, metric_type, timestamp)

### agent_hashlists

Tracks hashlist distribution to agents.

| Column | Type | Constraints | Default | Description |
|--------|------|-------------|---------|-------------|
| id | UUID | PRIMARY KEY | gen_random_uuid() | Record ID |
| agent_id | INTEGER | NOT NULL, FK → agents(id) | | Agent reference |
| hashlist_id | BIGINT | NOT NULL, FK → hashlists(id) | | Hashlist reference |
| file_path | TEXT | NOT NULL | | Local file path |
| downloaded_at | TIMESTAMP WITH TIME ZONE | | CURRENT_TIMESTAMP | Download time |
| last_used_at | TIMESTAMP WITH TIME ZONE | | CURRENT_TIMESTAMP | Last usage time |
| file_hash | VARCHAR(32) | | | MD5 hash for verification |

**Unique Constraint:** (agent_id, hashlist_id)

**Indexes:**
- idx_agent_hashlists_cleanup (last_used_at)

### agent_devices

Tracks individual physical compute devices with runtime selection support (updated in migration 81).

| Column | Type | Constraints | Default | Description |
|--------|------|-------------|---------|-------------|
| id | SERIAL | PRIMARY KEY | | Device record ID |
| agent_id | INTEGER | NOT NULL, FK → agents(id) | | Agent reference |
| device_id | INTEGER | NOT NULL | | Physical device index (0-based) |
| device_name | VARCHAR(255) | NOT NULL | | Device name |
| device_type | VARCHAR(50) | NOT NULL | | Type: GPU or CPU |
| enabled | BOOLEAN | NOT NULL | TRUE | Device enabled status |
| runtime_options | JSONB | NOT NULL | '[]'::jsonb | Available runtimes with capabilities |
| selected_runtime | VARCHAR(50) | | | Active runtime (CUDA/HIP/OpenCL) |
| created_at | TIMESTAMP WITH TIME ZONE | NOT NULL | CURRENT_TIMESTAMP | Creation time |
| updated_at | TIMESTAMP WITH TIME ZONE | NOT NULL | CURRENT_TIMESTAMP | Last update time |

**Unique Constraint:** (agent_id, device_id)

**Indexes:**
- idx_agent_devices_agent_id (agent_id)
- idx_agent_devices_enabled (agent_id, enabled)

**Triggers:**
- update_agent_devices_updated_at: Updates updated_at on row modification

**Runtime Options Structure (JSONB):**
```json
[
  {
    "backend": "HIP",
    "device_id": 1,
    "processors": 16,
    "clock": 2208,
    "memory_total": 8176,
    "memory_free": 8064,
    "pci_address": "03:00.0"
  }
]
```

**Migration History:**
- Migration 29: Initial table creation
- Migration 81: Added runtime_options and selected_runtime columns for GPU runtime selection

### agent_schedules

Stores daily scheduling information for agents (added in migration 42).

| Column | Type | Constraints | Default | Description |
|--------|------|-------------|---------|-------------|
| id | SERIAL | PRIMARY KEY | | Schedule ID |
| agent_id | INTEGER | NOT NULL, FK → agents(id) | | Agent reference |
| day_of_week | INTEGER | NOT NULL, CHECK | | Day: 0=Sunday...6=Saturday |
| start_time | TIME | NOT NULL | | Start time in UTC |
| end_time | TIME | NOT NULL | | End time in UTC |
| timezone | VARCHAR(50) | NOT NULL | 'UTC' | Original timezone |
| is_active | BOOLEAN | NOT NULL | true | Schedule active status |
| created_at | TIMESTAMP WITH TIME ZONE | NOT NULL | CURRENT_TIMESTAMP | Creation time |
| updated_at | TIMESTAMP WITH TIME ZONE | NOT NULL | CURRENT_TIMESTAMP | Last update time |

**Unique Constraint:** (agent_id, day_of_week)

**Check Constraint:** end_time != start_time (allows overnight schedules)

**Indexes:**
- idx_agent_schedules_agent_id (agent_id)
- idx_agent_schedules_day_active (day_of_week, is_active)

**Triggers:**
- update_agent_schedules_updated_at: Updates updated_at on row modification

---

## Authentication & Security (Extended)

The users table has been extended with additional security columns added through migrations:

### Additional users columns

| Column | Type | Constraints | Default | Description |
|--------|------|-------------|---------|-------------|
| mfa_enabled | BOOLEAN | | FALSE | MFA enabled status |
| mfa_type | text[] | CHECK | ARRAY['email'] | MFA types enabled: email, authenticator, backup, passkey |
| mfa_secret | TEXT | | | MFA secret |
| backup_codes | TEXT[] | | | Hashed backup codes |
| last_password_change | TIMESTAMP WITH TIME ZONE | | CURRENT_TIMESTAMP | Last password change |
| failed_login_attempts | INT | | 0 | Failed login count |
| last_failed_attempt | TIMESTAMP WITH TIME ZONE | | | Last failed attempt |
| account_locked | BOOLEAN | | FALSE | Account lock status |
| account_locked_until | TIMESTAMP WITH TIME ZONE | | | Lock expiration |
| account_enabled | BOOLEAN | | TRUE | Account enabled status |
| last_login | TIMESTAMP WITH TIME ZONE | | | Last successful login |
| disabled_reason | TEXT | | | Reason for disabling |
| disabled_at | TIMESTAMP WITH TIME ZONE | | | Disable timestamp |
| disabled_by | UUID | FK → users(id) | | Who disabled account |
| preferred_mfa_method | VARCHAR(20) | | | Preferred MFA method |

### tokens

JWT token storage with sliding window session support (added in migration 7, updated in migration 101).

| Column | Type | Constraints | Default | Description |
|--------|------|-------------|---------|-------------|
| id | UUID | PRIMARY KEY | gen_random_uuid() | Token ID |
| user_id | UUID | NOT NULL, FK → users(id) | | User reference |
| token | TEXT | NOT NULL, UNIQUE | | Token value |
| created_at | TIMESTAMP WITH TIME ZONE | | CURRENT_TIMESTAMP | Creation time |
| last_used_at | TIMESTAMP WITH TIME ZONE | | CURRENT_TIMESTAMP | Last usage time |
| expires_at | TIMESTAMP WITH TIME ZONE | NOT NULL | | Expiration time |
| revoked | BOOLEAN | | FALSE | Revocation status |
| revoked_at | TIMESTAMP WITH TIME ZONE | | | Revocation time |
| revoked_reason | TEXT | | | Revocation reason |
| superseded_at | TIMESTAMP WITH TIME ZONE | | | When token was replaced by a new token (migration 101) |
| superseded_by | UUID | FK → tokens(id) | | Reference to the replacement token (migration 101) |

**Sliding Window Session Behavior:**
- Tokens are refreshed on user activity after 1/3 of the session time has passed
- When refreshed, the old token is marked as superseded (not immediately invalidated)
- Superseded tokens remain valid for a 5-minute grace period to handle concurrent requests
- Token validity check: `superseded_at IS NULL OR superseded_at > NOW() - INTERVAL '5 minutes'`

**Relationships:**
- Referenced by `active_sessions(token_id)` with CASCADE delete (migration 65)
- Deleting a token automatically removes all associated sessions
- Self-referential via `superseded_by` to track token refresh chain

**Indexes:**
- idx_tokens_token (token)
- idx_tokens_user_id (user_id)
- idx_tokens_revoked (revoked)
- idx_tokens_superseded_at (superseded_at) - For efficient grace period queries (migration 101)

### auth_settings

Stores global authentication and security settings.

| Column | Type | Constraints | Default | Description |
|--------|------|-------------|---------|-------------|
| id | UUID | PRIMARY KEY | gen_random_uuid() | Settings ID |
| min_password_length | INT | | 15 | Minimum password length |
| require_uppercase | BOOLEAN | | TRUE | Require uppercase letters |
| require_lowercase | BOOLEAN | | TRUE | Require lowercase letters |
| require_numbers | BOOLEAN | | TRUE | Require numbers |
| require_special_chars | BOOLEAN | | TRUE | Require special characters |
| max_failed_attempts | INT | | 5 | Max failed login attempts |
| lockout_duration_minutes | INT | | 60 | Account lockout duration |
| require_mfa | BOOLEAN | | FALSE | Require MFA for all users |
| jwt_expiry_minutes | INT | | 60 | JWT token expiry |
| display_timezone | VARCHAR(50) | | 'UTC' | Display timezone |
| notification_aggregation_minutes | INT | | 60 | Notification aggregation period |
| allowed_mfa_methods | JSONB | | '["email", "authenticator"]' | Allowed MFA methods |
| email_code_validity_minutes | INT | | 5 | Email code validity |
| backup_codes_count | INT | | 8 | Number of backup codes |
| mfa_code_cooldown_minutes | INT | | 1 | MFA code cooldown |
| mfa_code_expiry_minutes | INT | | 5 | MFA code expiry |
| mfa_max_attempts | INT | | 3 | Max MFA attempts |
| webauthn_rp_id | VARCHAR(255) | | | WebAuthn Relying Party ID (domain) |
| webauthn_rp_origins | TEXT[] | | '{}' | Allowed WebAuthn origins |
| webauthn_rp_display_name | VARCHAR(255) | | 'KrakenHashes' | Display name for passkey prompts |

**WebAuthn Configuration Notes:**
- `webauthn_rp_id` must be a domain name (not IP address per WebAuthn spec)
- `webauthn_rp_origins` should include all URLs users access the system from
- Changing `webauthn_rp_id` after passkeys are registered will invalidate all existing passkeys

### login_attempts

Tracks login attempts for security monitoring.

| Column | Type | Constraints | Default | Description |
|--------|------|-------------|---------|-------------|
| id | UUID | PRIMARY KEY | gen_random_uuid() | Attempt ID |
| user_id | UUID | FK → users(id) | | User reference (nullable) |
| username | VARCHAR(255) | | | Attempted username |
| ip_address | INET | NOT NULL | | Client IP address |
| user_agent | TEXT | | | Client user agent |
| success | BOOLEAN | NOT NULL | | Success status |
| failure_reason | TEXT | | | Failure reason |
| attempted_at | TIMESTAMP WITH TIME ZONE | | CURRENT_TIMESTAMP | Attempt time |
| notified | BOOLEAN | | FALSE | Notification sent |

**Indexes:**
- idx_login_attempts_user_id (user_id)
- idx_login_attempts_attempted_at (attempted_at)
- idx_login_attempts_notified (notified)

### active_sessions

Tracks active user sessions linked to JWT tokens (updated in migration 65).

| Column | Type | Constraints | Default | Description |
|--------|------|-------------|---------|-------------|
| id | UUID | PRIMARY KEY | gen_random_uuid() | Session ID |
| user_id | UUID | FK → users(id) | | User reference |
| ip_address | INET | NOT NULL | | Session IP address |
| user_agent | TEXT | | | Client user agent |
| created_at | TIMESTAMP WITH TIME ZONE | | CURRENT_TIMESTAMP | Session start |
| last_active_at | TIMESTAMP WITH TIME ZONE | | CURRENT_TIMESTAMP | Last activity |
| token_id | UUID | FK → tokens(id) ON DELETE CASCADE | | Linked JWT token (migration 65) |

**Security Features:**
- **Session-Token Binding**: Each session is linked to its authentication token via foreign key
- **CASCADE Delete**: Deleting the token automatically removes the session
- **True Logout**: Terminating a session deletes the token, immediately invalidating authentication
- **No Orphaned Sessions**: Sessions cannot exist without valid tokens after migration 65

**Indexes:**
- idx_active_sessions_user_id (user_id)
- idx_active_sessions_last_active (last_active_at)
- idx_active_sessions_token_id (token_id)

### pending_mfa_setup

Tracks pending MFA setup processes (added in migration 8).

| Column | Type | Constraints | Default | Description |
|--------|------|-------------|---------|-------------|
| user_id | UUID | PRIMARY KEY, FK → users(id) | | User reference |
| method | VARCHAR(20) | NOT NULL, CHECK | | Method: email, authenticator |
| secret | TEXT | | | MFA secret |
| created_at | TIMESTAMP WITH TIME ZONE | NOT NULL | CURRENT_TIMESTAMP | Creation time |

**Indexes:**
- idx_pending_mfa_created_at (created_at)

### email_mfa_codes

Stores temporary MFA codes sent via email (added in migration 8).

| Column | Type | Constraints | Default | Description |
|--------|------|-------------|---------|-------------|
| user_id | UUID | PRIMARY KEY, FK → users(id) | | User reference |
| code | VARCHAR(6) | NOT NULL | | MFA code |
| attempts | INT | NOT NULL | 0 | Attempt count |
| expires_at | TIMESTAMP WITH TIME ZONE | NOT NULL | | Expiration time |
| created_at | TIMESTAMP WITH TIME ZONE | NOT NULL | CURRENT_TIMESTAMP | Creation time |

**Indexes:**
- idx_email_mfa_expires_at (expires_at)

### mfa_methods

Stores user MFA method configurations (added in migration 8).

| Column | Type | Constraints | Default | Description |
|--------|------|-------------|---------|-------------|
| id | UUID | PRIMARY KEY | gen_random_uuid() | Method ID |
| user_id | UUID | NOT NULL, FK → users(id) | | User reference |
| method | VARCHAR(20) | NOT NULL, CHECK | | Method: email, authenticator |
| is_primary | BOOLEAN | | FALSE | Primary method flag |
| created_at | TIMESTAMP WITH TIME ZONE | NOT NULL | CURRENT_TIMESTAMP | Creation time |
| last_used_at | TIMESTAMP WITH TIME ZONE | | | Last usage time |
| metadata | JSONB | | | Method-specific data |

**Unique Constraint:** (user_id, method)

**Indexes:**
- idx_mfa_methods_user (user_id)
- idx_mfa_methods_primary (user_id, is_primary)

### mfa_backup_codes

Stores MFA backup codes (added in migration 8).

| Column | Type | Constraints | Default | Description |
|--------|------|-------------|---------|-------------|
| id | UUID | PRIMARY KEY | gen_random_uuid() | Code ID |
| user_id | UUID | NOT NULL, FK → users(id) | | User reference |
| code_hash | VARCHAR(255) | NOT NULL | | Hashed backup code |
| used_at | TIMESTAMP WITH TIME ZONE | | | Usage timestamp |
| created_at | TIMESTAMP WITH TIME ZONE | NOT NULL | CURRENT_TIMESTAMP | Creation time |

**Indexes:**
- idx_mfa_backup_codes_user (user_id)
- idx_mfa_backup_codes_unused (user_id, used_at) WHERE used_at IS NULL

### mfa_sessions

Tracks MFA verification sessions during login (added in migration 11).

| Column | Type | Constraints | Default | Description |
|--------|------|-------------|---------|-------------|
| id | UUID | PRIMARY KEY | gen_random_uuid() | Session ID |
| user_id | UUID | NOT NULL, FK → users(id) | | User reference |
| session_token | TEXT | NOT NULL | | Session token |
| expires_at | TIMESTAMP WITH TIME ZONE | NOT NULL | | Expiration time |
| attempts | INT | NOT NULL | 0 | Failed attempts |
| created_at | TIMESTAMP WITH TIME ZONE | NOT NULL | NOW() | Creation time |

**Indexes:**
- idx_mfa_sessions_user_id (user_id)
- idx_mfa_sessions_session_token (session_token)
- idx_mfa_sessions_expires_at (expires_at)

**Triggers:**
- enforce_mfa_max_attempts_trigger: Enforces max attempts limit
- cleanup_expired_mfa_sessions_trigger: Cleans up expired sessions

### security_events

Logs security-related events (added in migration 8).

| Column | Type | Constraints | Default | Description |
|--------|------|-------------|---------|-------------|
| id | UUID | PRIMARY KEY | gen_random_uuid() | Event ID |
| user_id | UUID | FK → users(id) | | User reference |
| event_type | VARCHAR(50) | NOT NULL | | Event type |
| ip_address | INET | | | Client IP address |
| user_agent | TEXT | | | Client user agent |
| details | JSONB | | | Event details |
| created_at | TIMESTAMP WITH TIME ZONE | NOT NULL | CURRENT_TIMESTAMP | Event time |

**Indexes:**
- idx_security_events_user (user_id)
- idx_security_events_type (event_type)
- idx_security_events_created (created_at)

---

## Potfile Initialization Sequence

The potfile system initializes in stages during server startup:

### 1. On Server Startup
- Creates `/data/krakenhashes/wordlists/custom/potfile.txt` if missing
- Creates potfile wordlist entry in database with `is_potfile = true`
- Attempts to create "Potfile Run" preset job

### 2. Binary Dependency
- Preset jobs require a `binary_version_id` (NOT NULL constraint in database)
- If no binaries exist, preset job creation is deferred
- A background monitor runs every 5 seconds checking for binary availability
- Monitor stops once preset job is successfully created

### 3. Completion
- Once a binary is uploaded and verified, the preset job is created
- System settings are updated with both `potfile_wordlist_id` and `potfile_preset_job_id`
- The potfile system is fully operational

### Related Tables
- **wordlists**: Contains potfile entry with `is_potfile = true`
- **preset_jobs**: Contains "Potfile Run" job (once binary available)
- **potfile_staging**: Temporary storage for passwords before batch processing
- **system_settings**: Stores `potfile_wordlist_id` and `potfile_preset_job_id`

---

## Migration History

The database schema has evolved through 101 migrations:

1. **000001**: Initial schema - users, teams, user_teams
2. **000002**: Add auth_tokens table
3. **000003**: Create agents system
4. **000004**: Create voucher system
5. **000005**: Add email system
6. **000006**: Add email templates (enhancement)
7. **000007**: Auth security infrastructure
8. **000008**: Add MFA tables
9. **000009**: Update auth settings
10. **000010**: Add preferred MFA method
11. **000011**: Add MFA session
12. **000012**: Add binary versions
13. **000013**: Add wordlists
14. **000014**: Add rules
15. **000015**: Add hashlist tables
16. **000016**: Add hashcat hash types
17. **000017**: Add client settings
18. **000018**: Add job workflows
19. **000019**: Add system settings
20. **000020**: Add job execution (fixed)
21. **000021**: Add job execution settings
22. **000022**: Enhance job tasks and system settings
23. **000023**: Add max_agents column
24. **000024**: Add interrupted status
25. **000025**: Add job_tasks created_at
26. **000026**: Add job_tasks updated_at
27. **000027**: Fix hashes trigger
28. **000028**: Fix cracked counts
29. **000029**: Add agent devices
30. **000030**: Add agent owner and extra parameters
31. **000031**: Add agent is_enabled
32. **000032**: Add preset job keyspace and max_agents
33. **000033**: Add job created_by
34. **000034**: Add enhanced chunking support
35. **000035**: Make agent_id nullable in job_tasks
36. **000036**: Add progress tracking
37. **000037**: Add consecutive failures tracking
38. **000038**: Update progress precision
39. **000039**: Add speedtest timeout setting
40. **000040**: Add dispatched_keyspace to job_executions
41. **000041**: Add device tracking to performance_metrics
42. **000042**: Add agent scheduling
43. **000043**: Set owner_id for existing agents
44. **000044**: Add chunk_number to job_tasks
45. **000045**: Fix total_keyspace for rule split jobs
46. **000046**: Add task heartbeat timeout setting
47. **000047**: Add effective_keyspace to job_tasks
48. **000048**: Add potfile support
49. **000049**: Make job executions self-contained
50. **000050**: Add reconnect_pending status
51. **000051**: Add monitoring settings
52. **000052**: Remove is_small_job column
53. **000053**: Add binary default system
54. **000054**: Add auth token last activity tracking
55. **000055**: Add job notification tracking
56. **000056**: Add reconnect grace period setting
57. **000057**: Add agent download settings
58. **000058**: Add agent sync status
59. **000059**: Add average speed to tasks
60. **000060**: Add missing hash types
61. **000061**: Add hashlist potfile exclusion
62. **000062**: Add client potfile exclusion
63. **000063**: Add accurate keyspace tracking
64. **000064**: Add chunk_actual_keyspace tracking
65. **000065**: Link sessions to tokens with CASCADE delete for session security
66. **000066-000081**: [Various enhancements and bug fixes]
82. **000082**: Add agent_overflow_allocation_mode system setting
   - Controls overflow agent distribution (FIFO vs round-robin)
   - Applies to same-priority jobs exceeding max_agents limits
   - Default value: 'fifo' (oldest job gets all overflow agents)
   - Alternative: 'round_robin' (distribute evenly across jobs)
83. **000083**: Add benchmark_requests table for parallel benchmark execution
   - Enables polling-based coordination of async WebSocket benchmarks
   - Tracks both forced benchmarks (accurate keyspace) and agent speed benchmarks
   - Supports 96% performance improvement (15 agents: 450s → 12s)
   - Status tracking: pending, completed, failed
   - Automatic cleanup after each scheduling cycle
84-87. **000084-000087**: [Various enhancements]
88. **000088**: Add job_increment_layers table for increment mode support
   - Stores sub-layers for increment mode jobs
   - Each layer represents one mask length in the increment sequence
   - Enables parallel processing of different mask lengths
   - Tracks per-layer progress and status
89. **000089**: Add increment_layer_id to job_tasks
   - Links tasks to their parent increment layer
   - NULL for non-increment mode jobs
   - Enables layer-specific task tracking
90. **000090**: Add preset_increment_layers table
   - Pre-calculated increment layers for preset jobs
   - Layers are copied to job_increment_layers when job is created from preset
   - Ensures consistent keyspace calculations across jobs from same preset
91. **000091**: [Reserved]
92. **000092**: Add WebAuthn/Passkey support
   - Creates `user_passkeys` table for storing passkey credentials
   - Creates `pending_passkey_registration` table for registration challenges
   - Creates `pending_passkey_authentication` table for MFA authentication challenges
   - Adds WebAuthn settings to `auth_settings` (rp_id, rp_origins, rp_display_name)
   - Adds cleanup trigger for expired challenges
93. **000093**: Add passkey to MFA type constraints
   - Updates `users.mfa_type` CHECK constraint to allow 'passkey'
   - Updates `users.preferred_mfa_method` CHECK constraint to allow 'passkey'
94. **000094**: Add passkey backup flags
   - Adds `backup_eligible` column to `user_passkeys`
   - Adds `backup_state` column to `user_passkeys`
   - Required for WebAuthn credential validation with synced passkeys
95. **000095**: [Reserved/Internal]
96. **000096**: Add original_hash unique index
   - Creates `idx_hashes_original_hash_unique` unique index on `hashes.original_hash`
   - Enables fast deduplication during bulk import using `ON CONFLICT DO NOTHING`
   - Uses `CONCURRENTLY` to avoid locking during creation
97. **000097**: Add hashlist bulk batch size setting
   - Adds `hashlist_bulk_batch_size` system setting (default: 100000)
   - Controls batch size during hashlist upload processing
   - Higher values improve performance for large hashlists at cost of memory
98. **000098**: Add cracked_by_task_id to hashes
   - Adds `hashes.cracked_by_task_id` column referencing `job_tasks(id)`
   - Enables granular tracking of which task cracked each hash
   - Used for retransmit deduplication in the outfile acknowledgment protocol
   - ON DELETE SET NULL to preserve hashes when tasks are deleted
99. **000099**: Add task retransmit tracking
   - Adds `job_tasks.retransmit_count` to track retransmission attempts
   - Adds `job_tasks.last_retransmit_at` for timing information
   - Supports the outfile acknowledgment protocol for crack recovery
100. **000100**: Add cracking_completed_at timestamps
   - Adds `job_tasks.cracking_completed_at` - when hashcat finished (enters processing state)
   - Adds `job_executions.cracking_completed_at` - when all tasks finished hashcat
   - Distinguishes between hashcat completion and full processing completion
   - Enables tracking of hashcat work time vs data transmission time
101. **000101**: Add token sliding window session support
   - Adds `tokens.superseded_at` column for tracking when a token was replaced
   - Adds `tokens.superseded_by` column referencing the replacement token
   - Creates `idx_tokens_superseded_at` index for efficient grace period queries
   - Enables sliding window sessions that extend on user activity
   - Old tokens remain valid for 5-minute grace period after refresh

---

## Enums and Custom Types

### email_provider_type
- mailgun
- sendgrid
- mailchimp
- gmail

### email_template_type
- security_event
- job_completion
- admin_error
- mfa_code

### binary_type
- hashcat
- john

### compression_type
- 7z
- zip
- tar.gz
- tar.xz

### wordlist_type
- general
- specialized
- targeted
- custom

### wordlist_format
- plaintext
- compressed

### rule_type
- hashcat
- john

---

## Key Relationships

1. **User System**: users ↔ teams (many-to-many via user_teams)
2. **Agent System**: agents → users (created_by), agents ↔ teams (many-to-many via agent_teams)
3. **Hash Management**: hashlists → users, hashlists → clients, hashlists ↔ hashes (many-to-many via hashlist_hashes)
4. **Job System**: preset_jobs → binary_versions, job_executions → preset_jobs + hashlists, job_tasks → job_executions + agents
5. **Resource Management**: wordlists/rules → users (created_by), rules ↔ wordlists (compatibility)
6. **Authentication**: Various MFA and security tables → users
7. **Session Security**: tokens (parent) → active_sessions (child) with CASCADE delete - ensures session termination revokes authentication

---

## Data Lifecycle & Security

### Data Retention System

The database implements a comprehensive data retention system with automatic purging:

1. **Retention Policy Hierarchy**
   - System default: `client_settings.default_data_retention_months` (when client_id is NULL)
   - Client-specific: `clients.data_retention_months` overrides system default
   - Special values: NULL = use system default, 0 = keep forever

2. **Automatic Purge Process**
   - Runs daily at midnight and on backend startup
   - Processes hashlists older than retention period based on `created_at`
   - Executes within database transactions for atomicity
   - Logs all deletions for audit compliance

3. **Secure Deletion Process**
   - **Database:** Transactional deletion with CASCADE to dependent tables
   - **Filesystem:** Files overwritten with random data before removal
   - **PostgreSQL:** VACUUM ANALYZE on affected tables to prevent WAL recovery
   - **Orphan Cleanup:** Automatic removal of hashes not linked to any hashlist

4. **Affected Tables During Purge**
   - `hashlists` - Primary deletion target
   - `hashlist_hashes` - Junction table entries removed
   - `hashes` - Orphaned entries deleted
   - `agent_hashlists` - CASCADE deletion
   - `job_executions` - CASCADE deletion
   - `job_tasks` - CASCADE deletion via job_executions

### Security Features

1. **Deletion Security**
   - Files are securely overwritten with random data to prevent recovery
   - VACUUM ANALYZE prevents recovery from PostgreSQL dead tuples
   - Audit trail maintained for compliance verification

2. **CASCADE Deletion Paths**
   ```
   hashlists deletion triggers:
   ├── hashlist_hashes (explicit deletion)
   ├── hashes (orphan cleanup)
   ├── agent_hashlists (CASCADE)
   └── job_executions (CASCADE)
       └── job_tasks (CASCADE)
   ```

3. **Agent-Side Cleanup**
   - Agents automatically clean files older than 3 days
   - Prevents storage accumulation on compute nodes
   - Preserves base resources (binaries, wordlists, rules)

## Important Notes

1. **UUID Usage**: Most primary keys use UUID except for legacy/performance-critical tables (agents, hashlists use SERIAL/BIGSERIAL)
2. **Soft Deletes**: Not implemented - uses CASCADE deletes for referential integrity
3. **Audit Trails**: Separate audit tables for binary_versions, wordlists, and rules
4. **Time Zones**: All timestamps stored as TIMESTAMP WITH TIME ZONE
5. **JSON Storage**: Heavy use of JSONB for flexible metadata storage
6. **System User**: Special user with UUID 00000000-0000-0000-0000-000000000000 for system operations
7. **Data Retention**: Automatic purging with secure deletion and WAL protection

---

