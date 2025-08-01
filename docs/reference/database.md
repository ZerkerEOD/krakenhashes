# KrakenHashes Database Schema Reference

This document provides a comprehensive reference for the KrakenHashes database schema, extracted from migration files (v0.1.0-alpha).

## Table of Contents

1. [Core Tables](#core-tables)
   - [users](#users)
   - [teams](#teams)
   - [user_teams](#user_teams)
2. [Authentication & Security](#authentication--security)
   - [auth_tokens](#auth_tokens)
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
6. [Job Management](#job-management)
   - [job_workflows](#job_workflows)
   - [job_executions](#job_executions)
   - [job_tasks](#job_tasks)
   - [job_execution_settings](#job_execution_settings)
7. [Resource Management](#resource-management)
   - [wordlists](#wordlists)
   - [rules](#rules)
   - [binary_versions](#binary_versions)
8. [Client & Settings](#client--settings)
   - [clients](#clients)
   - [client_settings](#client_settings)
   - [system_settings](#system_settings)
9. [Performance & Scheduling](#performance--scheduling)
   - [performance_metrics](#performance_metrics)
   - [agent_scheduling](#agent_scheduling)
10. [Migration History](#migration-history)

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
| provider_type | email_provider_type | NOT NULL | | Provider: mailgun, sendgrid, mailchimp, gmail |
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

**Indexes:**
- idx_hashes_hash_value (hash_value)

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
| allow_high_priority_override | BOOLEAN | NOT NULL | false | Allow priority override |
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
| status | VARCHAR(50) | NOT NULL, CHECK | 'pending' | Status: pending, running, paused, completed, failed, cancelled, interrupted |
| priority | INT | NOT NULL | 0 | Execution priority |
| total_keyspace | BIGINT | | | Total keyspace size |
| processed_keyspace | BIGINT | | 0 | Processed keyspace |
| attack_mode | INT | NOT NULL | | Attack mode |
| created_at | TIMESTAMP WITH TIME ZONE | | CURRENT_TIMESTAMP | Creation time |
| started_at | TIMESTAMP WITH TIME ZONE | | | Start time |
| completed_at | TIMESTAMP WITH TIME ZONE | | | Completion time |
| error_message | TEXT | | | Error details |
| interrupted_by | UUID | FK → job_executions(id) | | Interrupting job |
| created_by | UUID | FK → users(id) | | Creator user (added in migration 33) |
| chunk_size | INTEGER | | | Chunk size override (added in migration 34) |
| chunk_overlap | INTEGER | | 0 | Chunk overlap (added in migration 34) |
| dispatched_keyspace | BIGINT | | 0 | Dispatched keyspace (added in migration 40) |
| progress | NUMERIC(6,3) | | 0 | Progress percentage (added in migration 36, updated in migration 38) |
| consecutive_failures | INTEGER | | 0 | Consecutive failure count (added in migration 37) |
| last_failure_at | TIMESTAMP WITH TIME ZONE | | | Last failure time (added in migration 37) |

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
| status | VARCHAR(50) | NOT NULL, CHECK | 'pending' | Status: pending, assigned, running, completed, failed, cancelled |
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

**Indexes:**
- idx_job_tasks_agent_status (agent_id, status)
- idx_job_tasks_execution (job_execution_id)
- idx_job_tasks_consecutive_failures (consecutive_failures)
- idx_job_tasks_chunk_number (job_execution_id, chunk_number)

**Triggers:**
- update_job_tasks_updated_at: Updates updated_at on row modification

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

Stores information about different versions of hash cracking binaries.

| Column | Type | Constraints | Default | Description |
|--------|------|-------------|---------|-------------|
| id | SERIAL | PRIMARY KEY | | Version ID |
| binary_type | binary_type | NOT NULL | | Type: hashcat, john |
| compression_type | compression_type | NOT NULL | | Compression: 7z, zip, tar.gz, tar.xz |
| source_url | TEXT | NOT NULL | | Download URL |
| file_name | VARCHAR(255) | NOT NULL | | File name |
| md5_hash | VARCHAR(32) | NOT NULL | | MD5 hash |
| file_size | BIGINT | NOT NULL | | File size in bytes |
| created_at | TIMESTAMP WITH TIME ZONE | | CURRENT_TIMESTAMP | Creation time |
| created_by | UUID | NOT NULL, FK → users(id) | | Creator user |
| is_active | BOOLEAN | | true | Active status |
| last_verified_at | TIMESTAMP WITH TIME ZONE | | | Last verification time |
| verification_status | VARCHAR(50) | | 'pending' | Status: pending, verified, failed |

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

Stores client-specific settings (added in migration 17).

| Column | Type | Constraints | Default | Description |
|--------|------|-------------|---------|-------------|
| id | SERIAL | PRIMARY KEY | | Settings ID |
| client_id | UUID | NOT NULL, FK → clients(id) | | Client reference |
| key | VARCHAR(255) | NOT NULL | | Setting key |
| value | TEXT | | | Setting value |
| data_type | VARCHAR(50) | NOT NULL | 'string' | Data type |
| created_at | TIMESTAMP WITH TIME ZONE | | CURRENT_TIMESTAMP | Creation time |
| updated_at | TIMESTAMP WITH TIME ZONE | | CURRENT_TIMESTAMP | Last update time |

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

**Triggers:**
- update_system_settings_updated_at: Updates updated_at on row modification

---

## Performance & Scheduling

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

Tracks individual compute devices (added in migration 29).

| Column | Type | Constraints | Default | Description |
|--------|------|-------------|---------|-------------|
| id | SERIAL | PRIMARY KEY | | Device record ID |
| agent_id | INTEGER | NOT NULL, FK → agents(id) | | Agent reference |
| device_id | INTEGER | NOT NULL | | Device ID |
| device_name | VARCHAR(255) | NOT NULL | | Device name |
| device_type | VARCHAR(50) | NOT NULL | | Type: GPU or CPU |
| enabled | BOOLEAN | NOT NULL | TRUE | Device enabled status |
| created_at | TIMESTAMP WITH TIME ZONE | NOT NULL | CURRENT_TIMESTAMP | Creation time |
| updated_at | TIMESTAMP WITH TIME ZONE | NOT NULL | CURRENT_TIMESTAMP | Last update time |

**Unique Constraint:** (agent_id, device_id)

**Indexes:**
- idx_agent_devices_agent_id (agent_id)
- idx_agent_devices_enabled (agent_id, enabled)

**Triggers:**
- update_agent_devices_updated_at: Updates updated_at on row modification

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
| mfa_type | text[] | CHECK | ARRAY['email'] | MFA types enabled |
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

JWT token storage (added in migration 7).

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

**Indexes:**
- idx_tokens_token (token)
- idx_tokens_user_id (user_id)
- idx_tokens_revoked (revoked)

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

Tracks active user sessions.

| Column | Type | Constraints | Default | Description |
|--------|------|-------------|---------|-------------|
| id | UUID | PRIMARY KEY | gen_random_uuid() | Session ID |
| user_id | UUID | FK → users(id) | | User reference |
| ip_address | INET | NOT NULL | | Session IP address |
| user_agent | TEXT | | | Client user agent |
| created_at | TIMESTAMP WITH TIME ZONE | | CURRENT_TIMESTAMP | Session start |
| last_active_at | TIMESTAMP WITH TIME ZONE | | CURRENT_TIMESTAMP | Last activity |

**Indexes:**
- idx_active_sessions_user_id (user_id)
- idx_active_sessions_last_active (last_active_at)

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

## Migration History

The database schema has evolved through 47 migrations:

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

---

## Important Notes

1. **UUID Usage**: Most primary keys use UUID except for legacy/performance-critical tables (agents, hashlists use SERIAL/BIGSERIAL)
2. **Soft Deletes**: Not implemented - uses CASCADE deletes for referential integrity
3. **Audit Trails**: Separate audit tables for binary_versions, wordlists, and rules
4. **Time Zones**: All timestamps stored as TIMESTAMP WITH TIME ZONE
5. **JSON Storage**: Heavy use of JSONB for flexible metadata storage
6. **System User**: Special user with UUID 00000000-0000-0000-0000-000000000000 for system operations

---

