# Notification Administration

## Overview

Administrators can configure system-wide notification settings, including global webhooks for external integrations, agent offline monitoring parameters, and view all user webhook configurations.

Access notification settings via **Admin Settings > Notifications**.

## Global Webhook

The global webhook allows administrators to send all notification events to a central endpoint for monitoring, logging, or integration with external systems.

### Configuration

| Field | Description |
|-------|-------------|
| **Enabled** | Toggle to enable/disable the global webhook |
| **URL** | The webhook endpoint URL |
| **Secret** | HMAC-SHA256 signing secret (optional) |
| **Custom Headers** | Additional HTTP headers as JSON object |

### Setting Up Global Webhook

1. Navigate to **Admin Settings > Notifications**
2. Expand the **Global Webhook Settings** section
3. Toggle **Enable Global Webhook**
4. Enter the webhook URL
5. (Optional) Set a signing secret for payload verification
6. (Optional) Add custom headers in JSON format:
   ```json
   {
     "Authorization": "Bearer your-token",
     "X-Custom-Header": "value"
   }
   ```
7. Click **Save**

### Testing Global Webhook

Before relying on the global webhook:

1. Click **Test Global Webhook**
2. A test payload is sent to the configured URL
3. Verify receipt in your monitoring system
4. Check for any error messages

### Global Webhook Payload

The global webhook receives all notification events with additional context:

```json
{
  "type": "job_completed",
  "title": "Job Completed",
  "message": "Job 'NTLM Attack' completed successfully",
  "user": {
    "id": "user-uuid",
    "username": "operator1",
    "email": "operator1@example.com"
  },
  "data": {
    "job_id": "job-uuid",
    "job_name": "NTLM Attack",
    "total_cracked": 150
  },
  "timestamp": "2024-01-15T10:30:00Z"
}
```

### Use Cases

- **Centralized Logging**: Send all events to a SIEM or logging platform
- **Team Notifications**: Route critical alerts to a shared Slack/Discord channel
- **Custom Integrations**: Trigger external workflows on specific events
- **Compliance**: Maintain an external record of security events

## Agent Offline Buffer

The agent offline buffer prevents notification spam when agents briefly disconnect due to network issues.

### How It Works

1. When an agent disconnects, a buffer timer starts
2. If the agent reconnects within the buffer period, no notification is sent
3. If the buffer expires without reconnection, an "Agent Offline" notification is sent
4. Only the agent's **owner** receives the offline notification

### Configuration

| Setting | Default | Description |
|---------|---------|-------------|
| **Buffer Minutes** | 10 | Time to wait before sending offline notification |

### Setting Buffer Duration

1. Navigate to **Admin Settings > Notifications**
2. Expand **Agent Offline Buffer Settings**
3. Enter the desired buffer duration in minutes
4. Click **Save**

### Recommendations

| Environment | Recommended Buffer |
|-------------|-------------------|
| **Production (stable network)** | 5-10 minutes |
| **Development/Testing** | 1-2 minutes |
| **High-availability** | 2-5 minutes |
| **Remote agents (unstable network)** | 15-30 minutes |

!!! tip "Buffer Considerations"
    A shorter buffer provides faster alerts but may cause false positives on unstable networks. A longer buffer reduces noise but delays genuine offline detection.

## User Webhooks Overview

Administrators can view all user-configured webhooks for monitoring and troubleshooting.

### Viewing User Webhooks

1. Navigate to **Admin Settings > Notifications**
2. Expand **User Webhooks Overview**
3. View the table of all user webhooks

### Webhook Information Displayed

| Column | Description |
|--------|-------------|
| **Username** | The user who owns the webhook |
| **Email** | User's email address |
| **Name** | Webhook name configured by user |
| **URL** | Webhook endpoint (truncated for display) |
| **Active** | Whether the webhook is enabled |
| **Total Sent** | Successful delivery count |
| **Total Failed** | Failed delivery count |

### Administrative Notes

- Administrators can **view** but not **edit** user webhooks
- Users manage their own webhooks through Settings > Notifications
- Use this view to troubleshoot webhook delivery issues
- Monitor for webhooks with high failure counts

## Notification Email Templates

KrakenHashes includes email templates for all notification types. Templates are managed in **Admin Settings > Email > Templates**.

### Notification-Specific Templates

| Template Type | Purpose |
|--------------|---------|
| `job_started` | Job execution began |
| `job_completed` | Job finished successfully |
| `job_failed` | Job encountered an error |
| `first_crack` | First hash cracked in hashlist |
| `task_completed` | Individual task completed |
| `agent_offline` | Agent disconnected |
| `agent_error` | Agent reported error |
| `security_password_changed` | User password changed |
| `security_mfa_disabled` | MFA was disabled |
| `security_suspicious_login` | Suspicious login attempt |
| `webhook_failure` | Webhook delivery failed |

### Template Variables

Templates support the following variables:

| Variable | Description |
|----------|-------------|
| `{{ .Username }}` | User's display name |
| `{{ .JobName }}` | Name of the job |
| `{{ .HashlistName }}` | Name of the hashlist |
| `{{ .AgentName }}` | Name of the agent |
| `{{ .TotalCracked }}` | Number of cracked hashes |
| `{{ .IPAddress }}` | IP address (for security events) |
| `{{ .Timestamp }}` | Event timestamp |

### Customizing Templates

1. Navigate to **Admin Settings > Email > Templates**
2. Select the notification template to edit
3. Modify the HTML and/or plain text content
4. Use the preview feature to verify appearance
5. Save the template

## System Settings Database

Notification settings are stored in the `system_settings` table:

| Key | Type | Description |
|-----|------|-------------|
| `global_webhook_url` | string | Global webhook endpoint |
| `global_webhook_secret` | string | Signing secret (encrypted) |
| `global_webhook_enabled` | boolean | Global webhook toggle |
| `global_webhook_custom_headers` | JSON | Additional headers |
| `agent_offline_buffer_minutes` | integer | Offline buffer duration |

## Security Considerations

### Webhook Security

1. **Use HTTPS**: Always configure webhook URLs with HTTPS
2. **Set Secrets**: Enable HMAC signing for payload verification
3. **Validate Origins**: Configure receiving systems to validate signatures
4. **Limit Exposure**: Use dedicated webhook endpoints, not general APIs

### Audit Logging

All security notifications are automatically logged to the audit log:

- Suspicious login attempts
- MFA disabled events
- Password changes
- Webhook failures

See [Audit Log](audit-log.md) for details on viewing and filtering security events.

### Mandatory Security Notifications

The following notification types cannot be disabled by users:

- `security_mfa_disabled`
- `security_password_changed`

These always trigger both in-app and email notifications to ensure users are aware of critical account changes.

## Troubleshooting

### Global Webhook Not Receiving Events

1. Verify the webhook is enabled
2. Test the webhook using the Test button
3. Check the URL is accessible from the server
4. Review custom headers for syntax errors
5. Check server logs for delivery errors

### Agent Offline Notifications Too Frequent

1. Increase the buffer duration
2. Check network stability to agents
3. Review agent heartbeat configuration
4. Consider agent placement relative to network boundaries

### Email Notifications Not Sending

1. Verify email provider is configured (see [Email Configuration](../system-setup/email.md))
2. Check email templates exist for notification types
3. Review email service logs for errors
4. Verify monthly email limit hasn't been reached

### User Webhooks Failing

1. View webhook statistics in User Webhooks Overview
2. Contact the user to verify their endpoint
3. Check for rate limiting or firewall issues
4. Review the last error message for debugging
