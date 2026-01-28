# Audit Log

## Overview

The Audit Log provides administrators with visibility into security-relevant and critical events across all users. It serves as a compliance and monitoring tool for tracking important system activities.

Access the Audit Log via **Admin Menu > Audit Log**.

## Purpose

The Audit Log is designed to:

- Track security events across all user accounts
- Provide an audit trail for compliance requirements
- Enable investigation of suspicious activities
- Monitor system health through critical event tracking

## Auditable Events

The following events are automatically logged:

### Security Events (Critical)

| Event Type | Description |
|------------|-------------|
| `security_suspicious_login` | Multiple failed login attempts from an IP |
| `security_mfa_disabled` | User disabled two-factor authentication |
| `security_password_changed` | User changed their password |

### System Events (Warning)

| Event Type | Description |
|------------|-------------|
| `job_failed` | A job execution failed with an error |
| `agent_error` | An agent reported an operational error |
| `agent_offline` | An agent disconnected after buffer period |
| `webhook_failure` | A webhook delivery failed after all retries |

## Severity Levels

Events are categorized by severity to help prioritize attention:

| Severity | Color | Description |
|----------|-------|-------------|
| **Critical** | Red | Security events requiring immediate attention |
| **Warning** | Orange | System failures that may need investigation |
| **Info** | Blue | Informational events for awareness |

## Using the Audit Log

### Viewing Events

1. Navigate to **Admin Menu > Audit Log**
2. Events are displayed in reverse chronological order
3. Click an event row to expand and view details

### Filtering Events

Use the filter bar to narrow down events:

| Filter | Options |
|--------|---------|
| **Event Type** | Select specific event types from dropdown |
| **Severity** | Critical, Warning, Info |
| **Start Date** | Events after this date |
| **End Date** | Events before this date |

Click **Clear Filters** to reset all filters.

### Event Details

Expand an event row to view additional information:

| Field | Description |
|-------|-------------|
| **Message** | Detailed event description |
| **IP Address** | Source IP (when available) |
| **User Agent** | Browser/client information |
| **Source** | Event source type and identifier |
| **Additional Data** | JSON object with event-specific details |

### Pagination

- Default: 10 events per page
- Options: 10, 25, 50, or 100 events per page
- Navigate between pages using page number buttons

## Event Data Structure

Each audit log entry contains:

```json
{
  "id": "uuid",
  "event_type": "security_suspicious_login",
  "severity": "critical",
  "title": "Suspicious Login Attempt",
  "message": "5 failed login attempts detected",
  "user_id": "user-uuid",
  "username": "operator1",
  "email": "operator1@example.com",
  "ip_address": "192.168.1.100",
  "user_agent": "Mozilla/5.0 ...",
  "source_type": "login",
  "source_id": "attempt-uuid",
  "data": {
    "failed_attempts": 5,
    "reason": "invalid_password"
  },
  "created_at": "2024-01-15T10:30:00Z"
}
```

## Real-Time Alerts

Critical security events trigger real-time alerts for connected administrators:

1. When a security event occurs, it's logged to the audit log
2. A system alert is broadcast to all connected admin sessions
3. Admins see a distinctive notification with event details
4. Alerts appear in the top-right corner with extended display time

!!! info "System Alerts"
    System alerts are ephemeral and not stored. They provide immediate awareness while the event is recorded in the audit log for permanent reference.

## Security Event Details

### Suspicious Login

Triggered when 3 or more failed login attempts occur:

**Data includes:**
- `failed_attempts`: Number of consecutive failures
- `reason`: Why logins failed (invalid_password, account_locked, etc.)
- `ip_address`: Source IP of attempts
- `user_agent`: Browser information

**Investigation steps:**
1. Check if the IP is from an expected location
2. Review timing of attempts (brute force indicators)
3. Contact user to verify account security
4. Consider IP blocking if malicious

### MFA Disabled

Triggered when a user disables two-factor authentication:

**Data includes:**
- `mfa_method`: Which method was disabled (totp, email, backup_codes)
- `timestamp`: When the change occurred

**Investigation steps:**
1. Verify user initiated the change
2. Review recent login activity for the user
3. Consider requiring MFA re-enrollment

### Password Changed

Triggered when a user changes their password:

**Data includes:**
- `timestamp`: When the change occurred
- `ip_address`: Where the change was made from

**Investigation steps:**
1. Verify user initiated the change
2. Check for suspicious login activity before the change
3. Confirm user has access to account

## Compliance Use Cases

### SOC 2

The audit log helps meet SOC 2 requirements for:
- CC6.1: Logical access controls
- CC6.2: Authentication mechanisms
- CC7.2: Monitoring of system components

### GDPR

Supports GDPR compliance through:
- Recording of account modifications
- Tracking access to user data
- Demonstrating security measures

### Internal Audits

Use the audit log for:
- Reviewing admin actions
- Investigating security incidents
- Generating compliance reports

## Data Retention

Audit log entries are:
- Stored permanently by default
- Not affected by data retention policies for other data types
- Available for historical review at any time

!!! warning "Storage Considerations"
    High-volume deployments may accumulate significant audit log data. Plan for storage growth accordingly.

## API Access

Administrators can query the audit log programmatically:

### List Audit Logs

```
GET /admin/audit-logs
```

Query parameters:
- `event_types`: Comma-separated list of event types
- `user_id`: Filter by specific user
- `severity`: Filter by severity level
- `start_date`: Events after this ISO8601 timestamp
- `end_date`: Events before this ISO8601 timestamp
- `limit`: Number of results (default 10)
- `offset`: Pagination offset

### Get Single Entry

```
GET /admin/audit-logs/{id}
```

### List Event Types

```
GET /admin/audit-logs/event-types
```

Returns all auditable event types with metadata.

## Best Practices

1. **Regular Review**
   - Check the audit log daily or weekly
   - Set up global webhook alerts for critical events
   - Establish an incident response process

2. **Filtering Strategy**
   - Use severity filter for routine monitoring
   - Filter by date range for incident investigation
   - Filter by user when investigating specific accounts

3. **Integration**
   - Export to SIEM for centralized monitoring
   - Set up global webhook to alerting systems
   - Include audit log review in security procedures

4. **Documentation**
   - Document investigation procedures
   - Record actions taken for audit trail
   - Maintain incident response playbooks

## Troubleshooting

### Events Not Appearing

1. Verify the event type is auditable (see list above)
2. Check filter settings aren't hiding events
3. Refresh the page to load latest data
4. Verify admin permissions

### Missing Details

1. Some events may not have all fields (IP, user agent)
2. Internal system events may lack user context
3. Check "Additional Data" for event-specific information

### Performance Issues

1. Use date filters to limit result sets
2. Reduce page size for faster loading
3. Consider exporting data for offline analysis
