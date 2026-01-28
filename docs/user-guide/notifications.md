# Notifications

## Overview

KrakenHashes provides a comprehensive notification system to keep you informed about important events in your password cracking operations. You can receive notifications through multiple channels and customize your preferences for each notification type.

## Notification Channels

Notifications can be delivered through three channels:

| Channel | Description |
|---------|-------------|
| **In-App** | Real-time notifications displayed in the notification bell and Notification Center |
| **Email** | Email notifications sent to your registered email address |
| **Webhook** | HTTP POST requests sent to your configured webhook endpoints |

## Notification Bell

The notification bell icon appears in the top navigation bar. It provides quick access to your recent notifications.

### Features

- **Unread Badge**: Shows the count of unread notifications (max 99)
- **Connection Status**: Indicates WebSocket connection state (connected/disconnected)
- **Quick Preview**: Click to see your 5 most recent notifications
- **Mark All Read**: Quickly mark all notifications as read
- **Navigate to Source**: Click a notification to go to the related job, agent, or hashlist

### Notification Icons

Notifications are color-coded by type:

- :material-check-circle:{ style="color: green" } **Green** - Success (job completed, first crack)
- :material-close-circle:{ style="color: red" } **Red** - Error (job failed, agent error)
- :material-alert:{ style="color: orange" } **Yellow** - Warning (agent offline, suspicious login)
- :material-information:{ style="color: blue" } **Blue** - Security (MFA disabled, password changed)

## Notification Center

Access the full Notification Center by clicking "View all notifications" in the bell dropdown or navigating to `/notifications`.

### Features

- **Pagination**: Browse through all notifications with 20 items per page
- **Filtering**: Filter by category (Job, Agent, Security, System) and status (All, Unread, Read)
- **Bulk Actions**: Select multiple notifications for deletion
- **Mark as Read**: Mark individual or all notifications as read
- **Source Navigation**: Click notifications to navigate to related items

## Notification Types

KrakenHashes supports 11 notification types organized into 4 categories:

### Job Notifications

| Type | Description |
|------|-------------|
| **Job Started** | A job execution has begun |
| **Job Completed** | A job has finished successfully |
| **Job Failed** | A job encountered an error and failed |
| **First Crack** | The first hash was cracked in a hashlist |
| **Task Completed** | An individual task completed (configurable) |

### Agent Notifications

| Type | Description |
|------|-------------|
| **Agent Offline** | An agent has disconnected (after buffer period) |
| **Agent Error** | An agent reported an error during operation |

### Security Notifications

!!! warning "Mandatory Notifications"
    Security notifications cannot be disabled. They are always sent via in-app and email channels to ensure you're aware of critical security events.

| Type | Description |
|------|-------------|
| **Suspicious Login** | Multiple failed login attempts detected |
| **MFA Disabled** | Two-factor authentication was disabled |
| **Password Changed** | Your password was changed |

### System Notifications

| Type | Description |
|------|-------------|
| **Webhook Failure** | A webhook delivery failed after all retries |

## Notification Preferences

Configure your notification preferences in **Settings > Notifications**.

### Per-Type Settings

For each notification type, you can enable or disable each delivery channel:

- **In-App**: Enable/disable in-app notifications
- **Email**: Enable/disable email notifications (requires email configuration)
- **Webhook**: Enable/disable webhook delivery (requires active webhooks)

### Task Report Mode

For "Task Completed" notifications, choose when to receive them:

- **Only if cracks found**: Only notify when the task cracked at least one hash
- **Always**: Notify for every task completion regardless of results

### Default Settings

| Type | In-App | Email | Webhook |
|------|--------|-------|---------|
| Job Started | On | Off | Off |
| Job Completed | On | On | Off |
| Job Failed | On | On | Off |
| First Crack | On | On | Off |
| Task Completed | On | Off | Off |
| Agent Offline | On | On | Off |
| Agent Error | On | On | Off |
| Security Events | On | On | N/A |
| Webhook Failure | On | Off | N/A |

## Personal Webhooks

Set up webhooks to receive notifications in external services like Slack, Discord, or Microsoft Teams.

### Creating a Webhook

1. Navigate to **Settings > Notifications > Webhooks**
2. Click **Add Webhook**
3. Configure the webhook:
   - **Name**: A descriptive name for your webhook
   - **URL**: The webhook endpoint URL
   - **Secret** (optional): HMAC-SHA256 signing secret for payload verification
   - **Notification Types**: Select which notification types to send
   - **Custom Headers** (optional): Additional HTTP headers as JSON
   - **Retry Count**: Number of retries on failure (0-10)
   - **Timeout**: Request timeout in seconds (1-60)

### Platform Detection

KrakenHashes automatically detects the platform from your webhook URL and formats payloads accordingly:

| Platform | URL Pattern | Payload Format |
|----------|-------------|----------------|
| **Discord** | `discord.com/api/webhooks/` | Rich embeds with color-coded severity |
| **Slack** | `hooks.slack.com/` | Block-formatted messages with context |
| **Microsoft Teams** | `webhook.office.com/` | MessageCard format with facts |
| **Generic** | Other URLs | Standard JSON format |

### Webhook Payload Structure

For generic webhooks, the payload includes:

```json
{
  "type": "job_completed",
  "title": "Job Completed",
  "message": "Job 'NTLM Attack' completed successfully",
  "data": {
    "job_id": "uuid",
    "job_name": "NTLM Attack",
    "hashlist_name": "domain_hashes.txt",
    "total_cracked": 150,
    "completion_time": "2024-01-15T10:30:00Z"
  },
  "timestamp": "2024-01-15T10:30:00Z"
}
```

### Webhook Security

- **HMAC Signing**: If a secret is configured, payloads are signed with HMAC-SHA256
- **Signature Header**: `X-KrakenHashes-Signature` contains the hex-encoded signature
- **Verification**: Verify by computing `HMAC-SHA256(secret, payload_body)`

### Testing Webhooks

Before saving, test your webhook configuration:

1. Click **Test URL** to send a test payload without saving
2. Verify the payload arrives at your endpoint
3. Check the format is correct for your platform
4. Save the webhook once testing succeeds

### Webhook Statistics

Each webhook tracks:

- **Total Sent**: Number of successful deliveries
- **Total Failed**: Number of failed deliveries (after all retries)
- **Last Success**: Timestamp of last successful delivery
- **Last Error**: Most recent error message

## Real-Time Updates

Notifications are delivered in real-time via WebSocket connection:

- **Automatic Connection**: WebSocket connects when you log in
- **Auto-Reconnect**: Automatically reconnects on connection loss (up to 10 retries)
- **Heartbeat**: Maintains connection with periodic ping/pong messages
- **Instant Delivery**: New notifications appear immediately without page refresh

### Connection Status

The notification bell indicates your connection status:

- **Connected**: Real-time notifications are active
- **Disconnected**: Notifications may be delayed until reconnection

## Best Practices

1. **Enable Email for Critical Events**
   - Keep email enabled for job failures and agent issues
   - Security notifications are always emailed for your protection

2. **Use Webhooks for Team Visibility**
   - Set up a Slack or Discord webhook for team-wide notifications
   - Filter to only important events (job completed, failures)

3. **Configure Task Reports Wisely**
   - Use "only if cracks" mode to reduce noise
   - Switch to "always" only when debugging

4. **Review Notifications Regularly**
   - Check the Notification Center periodically
   - Mark old notifications as read to keep the list manageable

## Troubleshooting

### Notifications Not Appearing

1. Check WebSocket connection status in the notification bell
2. Verify notifications are enabled in your preferences
3. Try refreshing the page to re-establish connection

### Emails Not Received

1. Verify email is configured by your administrator
2. Check your spam/junk folder
3. Confirm email is enabled in your notification preferences

### Webhooks Not Delivering

1. Check webhook statistics for error messages
2. Verify the URL is accessible from the server
3. Test the webhook URL manually
4. Review retry count and timeout settings
5. Check if the webhook is marked as active
