-- Migration 114: Insert default notification email templates
-- ENUM values were added in migration 113
-- Using ON CONFLICT to avoid duplicates if templates already exist

-- First, add unique constraint on template_type (required for ON CONFLICT)
CREATE UNIQUE INDEX IF NOT EXISTS email_templates_template_type_unique ON email_templates (template_type);

-- Security: Password Changed
INSERT INTO email_templates (template_type, name, subject, html_content, text_content) VALUES
('security_password_changed', 'Password Changed Notification',
 'KrakenHashes Security Alert: Your Password Has Been Changed',
 '<!DOCTYPE html>
<html>
<head>
    <meta charset="UTF-8">
    <style>
        .header {
            background-color: #000000;
            padding: 20px;
            text-align: center;
            width: 100%;
        }
        .header h1 {
            color: #FF0000;
            font-family: Arial, sans-serif;
            margin: 0;
        }
        .content {
            padding: 20px;
            font-family: Arial, sans-serif;
        }
        .alert-box {
            background-color: #fff3cd;
            border: 1px solid #ffc107;
            border-radius: 5px;
            padding: 15px;
            margin: 15px 0;
        }
        .details {
            background-color: #f5f5f5;
            padding: 15px;
            border-radius: 5px;
            margin: 15px 0;
        }
    </style>
</head>
<body>
    <div class="header">
        <h1>KrakenHashes</h1>
    </div>
    <div class="content">
        <h2>Password Changed</h2>
        <p>Hello {{ .Username }},</p>
        <p>Your KrakenHashes account password was successfully changed.</p>
        <div class="details">
            <h3>Change Details</h3>
            <p><strong>Time:</strong> {{ .Timestamp }}</p>
            <p><strong>IP Address:</strong> {{ .IPAddress }}</p>
            <p><strong>Browser:</strong> {{ .UserAgent }}</p>
        </div>
        <div class="alert-box">
            <strong>Did you make this change?</strong>
            <p>If you did not change your password, your account may be compromised. Please contact support immediately.</p>
        </div>
    </div>
</body>
</html>',
 'PASSWORD CHANGED

Hello {{ .Username }},

Your KrakenHashes account password was successfully changed.

Change Details:
- Time: {{ .Timestamp }}
- IP Address: {{ .IPAddress }}
- Browser: {{ .UserAgent }}

If you did not make this change, your account may be compromised. Please contact support immediately.')
ON CONFLICT (template_type) DO NOTHING;

-- Security: MFA Disabled
INSERT INTO email_templates (template_type, name, subject, html_content, text_content) VALUES
('security_mfa_disabled', 'MFA Disabled Notification',
 'KrakenHashes Security Alert: Two-Factor Authentication Disabled',
 '<!DOCTYPE html>
<html>
<head>
    <meta charset="UTF-8">
    <style>
        .header {
            background-color: #000000;
            padding: 20px;
            text-align: center;
            width: 100%;
        }
        .header h1 {
            color: #FF0000;
            font-family: Arial, sans-serif;
            margin: 0;
        }
        .content {
            padding: 20px;
            font-family: Arial, sans-serif;
        }
        .warning-box {
            background-color: #f8d7da;
            border: 1px solid #f5c6cb;
            border-radius: 5px;
            padding: 15px;
            margin: 15px 0;
        }
        .details {
            background-color: #f5f5f5;
            padding: 15px;
            border-radius: 5px;
            margin: 15px 0;
        }
    </style>
</head>
<body>
    <div class="header">
        <h1>KrakenHashes</h1>
    </div>
    <div class="content">
        <h2>Two-Factor Authentication Disabled</h2>
        <p>Hello {{ .Username }},</p>
        <p>Two-factor authentication has been disabled on your KrakenHashes account.</p>
        <div class="details">
            <h3>Details</h3>
            <p><strong>Method Disabled:</strong> {{ .DisabledMethod }}</p>
            <p><strong>Time:</strong> {{ .Timestamp }}</p>
            <p><strong>IP Address:</strong> {{ .IPAddress }}</p>
        </div>
        <div class="warning-box">
            <strong>Security Notice</strong>
            <p>Your account is now less secure. We strongly recommend re-enabling two-factor authentication.</p>
            <p>If you did not make this change, your account may be compromised. Please contact support immediately.</p>
        </div>
    </div>
</body>
</html>',
 'TWO-FACTOR AUTHENTICATION DISABLED

Hello {{ .Username }},

Two-factor authentication has been disabled on your KrakenHashes account.

Details:
- Method Disabled: {{ .DisabledMethod }}
- Time: {{ .Timestamp }}
- IP Address: {{ .IPAddress }}

SECURITY NOTICE: Your account is now less secure. We strongly recommend re-enabling two-factor authentication.

If you did not make this change, your account may be compromised. Please contact support immediately.')
ON CONFLICT (template_type) DO NOTHING;

-- Security: Suspicious Login
INSERT INTO email_templates (template_type, name, subject, html_content, text_content) VALUES
('security_suspicious_login', 'Suspicious Login Activity',
 'KrakenHashes Security Alert: Suspicious Login Activity Detected',
 '<!DOCTYPE html>
<html>
<head>
    <meta charset="UTF-8">
    <style>
        .header {
            background-color: #000000;
            padding: 20px;
            text-align: center;
            width: 100%;
        }
        .header h1 {
            color: #FF0000;
            font-family: Arial, sans-serif;
            margin: 0;
        }
        .content {
            padding: 20px;
            font-family: Arial, sans-serif;
        }
        .alert-box {
            background-color: #f8d7da;
            border: 1px solid #f5c6cb;
            border-radius: 5px;
            padding: 15px;
            margin: 15px 0;
        }
        .details {
            background-color: #f5f5f5;
            padding: 15px;
            border-radius: 5px;
            margin: 15px 0;
        }
    </style>
</head>
<body>
    <div class="header">
        <h1>KrakenHashes</h1>
    </div>
    <div class="content">
        <h2>Suspicious Login Activity</h2>
        <p>Hello {{ .Username }},</p>
        <p>{{ .Message }}</p>
        <div class="details">
            <h3>Activity Details</h3>
            <p><strong>Time:</strong> {{ .Timestamp }}</p>
            <p><strong>IP Address:</strong> {{ .IPAddress }}</p>
            <p><strong>Browser:</strong> {{ .UserAgent }}</p>
            {{ if .FailedAttempts }}<p><strong>Failed Attempts:</strong> {{ .FailedAttempts }}</p>{{ end }}
        </div>
        <div class="alert-box">
            <strong>Was this you?</strong>
            <p>If you do not recognize this activity, we recommend changing your password immediately and enabling two-factor authentication.</p>
        </div>
    </div>
</body>
</html>',
 'SUSPICIOUS LOGIN ACTIVITY

Hello {{ .Username }},

{{ .Message }}

Activity Details:
- Time: {{ .Timestamp }}
- IP Address: {{ .IPAddress }}
- Browser: {{ .UserAgent }}
{{ if .FailedAttempts }}- Failed Attempts: {{ .FailedAttempts }}{{ end }}

If you do not recognize this activity, we recommend changing your password immediately and enabling two-factor authentication.')
ON CONFLICT (template_type) DO NOTHING;

-- Job: Started
INSERT INTO email_templates (template_type, name, subject, html_content, text_content) VALUES
('job_started', 'Job Started Notification',
 'KrakenHashes: Job "{{ .JobName }}" Has Started',
 '<!DOCTYPE html>
<html>
<head>
    <meta charset="UTF-8">
    <style>
        .header {
            background-color: #000000;
            padding: 20px;
            text-align: center;
            width: 100%;
        }
        .header h1 {
            color: #FF0000;
            font-family: Arial, sans-serif;
            margin: 0;
        }
        .content {
            padding: 20px;
            font-family: Arial, sans-serif;
        }
        .info-box {
            background-color: #d1ecf1;
            border: 1px solid #bee5eb;
            border-radius: 5px;
            padding: 15px;
            margin: 15px 0;
        }
        .details {
            background-color: #f5f5f5;
            padding: 15px;
            border-radius: 5px;
            margin: 15px 0;
        }
    </style>
</head>
<body>
    <div class="header">
        <h1>KrakenHashes</h1>
    </div>
    <div class="content">
        <h2>Job Started</h2>
        <p>Your job has started processing.</p>
        <div class="details">
            <h3>Job Details</h3>
            <p><strong>Job Name:</strong> {{ .JobName }}</p>
            <p><strong>Hashlist:</strong> {{ .HashlistName }}</p>
            <p><strong>Priority:</strong> {{ .Priority }}</p>
            <p><strong>Total Hashes:</strong> {{ .TotalHashes }}</p>
        </div>
        <div class="info-box">
            <p>You will receive another notification when the job completes or if any issues occur.</p>
        </div>
    </div>
</body>
</html>',
 'JOB STARTED

Your job has started processing.

Job Details:
- Job Name: {{ .JobName }}
- Hashlist: {{ .HashlistName }}
- Priority: {{ .Priority }}
- Total Hashes: {{ .TotalHashes }}

You will receive another notification when the job completes or if any issues occur.')
ON CONFLICT (template_type) DO NOTHING;

-- Job: Failed
INSERT INTO email_templates (template_type, name, subject, html_content, text_content) VALUES
('job_failed', 'Job Failed Notification',
 'KrakenHashes: Job "{{ .JobName }}" Has Failed',
 '<!DOCTYPE html>
<html>
<head>
    <meta charset="UTF-8">
    <style>
        .header {
            background-color: #000000;
            padding: 20px;
            text-align: center;
            width: 100%;
        }
        .header h1 {
            color: #FF0000;
            font-family: Arial, sans-serif;
            margin: 0;
        }
        .content {
            padding: 20px;
            font-family: Arial, sans-serif;
        }
        .error-box {
            background-color: #f8d7da;
            border: 1px solid #f5c6cb;
            border-radius: 5px;
            padding: 15px;
            margin: 15px 0;
        }
        .details {
            background-color: #f5f5f5;
            padding: 15px;
            border-radius: 5px;
            margin: 15px 0;
        }
    </style>
</head>
<body>
    <div class="header">
        <h1>KrakenHashes</h1>
    </div>
    <div class="content">
        <h2>Job Failed</h2>
        <p>Unfortunately, your job has failed.</p>
        <div class="details">
            <h3>Job Details</h3>
            <p><strong>Job Name:</strong> {{ .JobName }}</p>
            <p><strong>Failed At:</strong> {{ .FailedAt }}</p>
        </div>
        <div class="error-box">
            <h3>Error Details</h3>
            <p>{{ .ErrorMessage }}</p>
        </div>
        <p>Please check your job configuration and try again. Contact support if the issue persists.</p>
    </div>
</body>
</html>',
 'JOB FAILED

Unfortunately, your job has failed.

Job Details:
- Job Name: {{ .JobName }}
- Failed At: {{ .FailedAt }}

Error Details:
{{ .ErrorMessage }}

Please check your job configuration and try again. Contact support if the issue persists.')
ON CONFLICT (template_type) DO NOTHING;

-- First Crack
INSERT INTO email_templates (template_type, name, subject, html_content, text_content) VALUES
('first_crack', 'First Crack Notification',
 'KrakenHashes: First Hash Cracked in "{{ .HashlistName }}"!',
 '<!DOCTYPE html>
<html>
<head>
    <meta charset="UTF-8">
    <style>
        .header {
            background-color: #000000;
            padding: 20px;
            text-align: center;
            width: 100%;
        }
        .header h1 {
            color: #FF0000;
            font-family: Arial, sans-serif;
            margin: 0;
        }
        .content {
            padding: 20px;
            font-family: Arial, sans-serif;
        }
        .success-box {
            background-color: #d4edda;
            border: 1px solid #c3e6cb;
            border-radius: 5px;
            padding: 15px;
            margin: 15px 0;
        }
        .details {
            background-color: #f5f5f5;
            padding: 15px;
            border-radius: 5px;
            margin: 15px 0;
        }
    </style>
</head>
<body>
    <div class="header">
        <h1>KrakenHashes</h1>
    </div>
    <div class="content">
        <h2>First Crack!</h2>
        <div class="success-box">
            <p>Great news! The first hash has been cracked in your hashlist.</p>
        </div>
        <div class="details">
            <h3>Details</h3>
            <p><strong>Hashlist:</strong> {{ .HashlistName }}</p>
            <p><strong>Job:</strong> {{ .JobName }}</p>
            {{ if .CrackedHash }}<p><strong>Hash:</strong> {{ .CrackedHash }}</p>{{ end }}
        </div>
        <p>Check your dashboard for real-time progress updates.</p>
    </div>
</body>
</html>',
 'FIRST CRACK!

Great news! The first hash has been cracked in your hashlist.

Details:
- Hashlist: {{ .HashlistName }}
- Job: {{ .JobName }}
{{ if .CrackedHash }}- Hash: {{ .CrackedHash }}{{ end }}

Check your dashboard for real-time progress updates.')
ON CONFLICT (template_type) DO NOTHING;

-- Task Completed
INSERT INTO email_templates (template_type, name, subject, html_content, text_content) VALUES
('task_completed', 'Task Completed Notification',
 'KrakenHashes: Task Completed with {{ .CrackCount }} Cracks',
 '<!DOCTYPE html>
<html>
<head>
    <meta charset="UTF-8">
    <style>
        .header {
            background-color: #000000;
            padding: 20px;
            text-align: center;
            width: 100%;
        }
        .header h1 {
            color: #FF0000;
            font-family: Arial, sans-serif;
            margin: 0;
        }
        .content {
            padding: 20px;
            font-family: Arial, sans-serif;
        }
        .stats {
            background-color: #f5f5f5;
            padding: 15px;
            border-radius: 5px;
            margin: 15px 0;
        }
    </style>
</head>
<body>
    <div class="header">
        <h1>KrakenHashes</h1>
    </div>
    <div class="content">
        <h2>Task Completed</h2>
        <p>A task has completed processing.</p>
        <div class="stats">
            <h3>Task Statistics</h3>
            <p><strong>Job:</strong> {{ .JobName }}</p>
            <p><strong>Agent:</strong> {{ .AgentName }}</p>
            <p><strong>Cracks Found:</strong> {{ .CrackCount }}</p>
        </div>
    </div>
</body>
</html>',
 'TASK COMPLETED

A task has completed processing.

Task Statistics:
- Job: {{ .JobName }}
- Agent: {{ .AgentName }}
- Cracks Found: {{ .CrackCount }}')
ON CONFLICT (template_type) DO NOTHING;

-- Agent Offline
INSERT INTO email_templates (template_type, name, subject, html_content, text_content) VALUES
('agent_offline', 'Agent Offline Notification',
 'KrakenHashes Alert: Agent "{{ .AgentName }}" is Offline',
 '<!DOCTYPE html>
<html>
<head>
    <meta charset="UTF-8">
    <style>
        .header {
            background-color: #000000;
            padding: 20px;
            text-align: center;
            width: 100%;
        }
        .header h1 {
            color: #FF0000;
            font-family: Arial, sans-serif;
            margin: 0;
        }
        .content {
            padding: 20px;
            font-family: Arial, sans-serif;
        }
        .warning-box {
            background-color: #fff3cd;
            border: 1px solid #ffc107;
            border-radius: 5px;
            padding: 15px;
            margin: 15px 0;
        }
        .details {
            background-color: #f5f5f5;
            padding: 15px;
            border-radius: 5px;
            margin: 15px 0;
        }
    </style>
</head>
<body>
    <div class="header">
        <h1>KrakenHashes</h1>
    </div>
    <div class="content">
        <h2>Agent Offline</h2>
        <div class="warning-box">
            <p>One of your agents has been offline for an extended period.</p>
        </div>
        <div class="details">
            <h3>Agent Details</h3>
            <p><strong>Agent Name:</strong> {{ .AgentName }}</p>
            <p><strong>Disconnected At:</strong> {{ .DisconnectedAt }}</p>
            <p><strong>Offline Duration:</strong> {{ .OfflineDuration }}</p>
        </div>
        <p>Please check the agent status and restart if necessary.</p>
    </div>
</body>
</html>',
 'AGENT OFFLINE

One of your agents has been offline for an extended period.

Agent Details:
- Agent Name: {{ .AgentName }}
- Disconnected At: {{ .DisconnectedAt }}
- Offline Duration: {{ .OfflineDuration }}

Please check the agent status and restart if necessary.')
ON CONFLICT (template_type) DO NOTHING;

-- Agent Error
INSERT INTO email_templates (template_type, name, subject, html_content, text_content) VALUES
('agent_error', 'Agent Error Notification',
 'KrakenHashes Alert: Agent "{{ .AgentName }}" Reported an Error',
 '<!DOCTYPE html>
<html>
<head>
    <meta charset="UTF-8">
    <style>
        .header {
            background-color: #000000;
            padding: 20px;
            text-align: center;
            width: 100%;
        }
        .header h1 {
            color: #FF0000;
            font-family: Arial, sans-serif;
            margin: 0;
        }
        .content {
            padding: 20px;
            font-family: Arial, sans-serif;
        }
        .error-box {
            background-color: #f8d7da;
            border: 1px solid #f5c6cb;
            border-radius: 5px;
            padding: 15px;
            margin: 15px 0;
        }
        .details {
            background-color: #f5f5f5;
            padding: 15px;
            border-radius: 5px;
            margin: 15px 0;
        }
    </style>
</head>
<body>
    <div class="header">
        <h1>KrakenHashes</h1>
    </div>
    <div class="content">
        <h2>Agent Error</h2>
        <p>An agent has reported an error.</p>
        <div class="details">
            <h3>Agent Details</h3>
            <p><strong>Agent Name:</strong> {{ .AgentName }}</p>
            <p><strong>Reported At:</strong> {{ .ReportedAt }}</p>
        </div>
        <div class="error-box">
            <h3>Error Details</h3>
            <p>{{ .Error }}</p>
            {{ if .Context }}<p><strong>Context:</strong> {{ .Context }}</p>{{ end }}
        </div>
        <p>Please investigate and address the issue.</p>
    </div>
</body>
</html>',
 'AGENT ERROR

An agent has reported an error.

Agent Details:
- Agent Name: {{ .AgentName }}
- Reported At: {{ .ReportedAt }}

Error Details:
{{ .Error }}
{{ if .Context }}Context: {{ .Context }}{{ end }}

Please investigate and address the issue.')
ON CONFLICT (template_type) DO NOTHING;

-- Webhook Failure
INSERT INTO email_templates (template_type, name, subject, html_content, text_content) VALUES
('webhook_failure', 'Webhook Failure Notification',
 'KrakenHashes Alert: Webhook Delivery Failed',
 '<!DOCTYPE html>
<html>
<head>
    <meta charset="UTF-8">
    <style>
        .header {
            background-color: #000000;
            padding: 20px;
            text-align: center;
            width: 100%;
        }
        .header h1 {
            color: #FF0000;
            font-family: Arial, sans-serif;
            margin: 0;
        }
        .content {
            padding: 20px;
            font-family: Arial, sans-serif;
        }
        .error-box {
            background-color: #f8d7da;
            border: 1px solid #f5c6cb;
            border-radius: 5px;
            padding: 15px;
            margin: 15px 0;
        }
        .details {
            background-color: #f5f5f5;
            padding: 15px;
            border-radius: 5px;
            margin: 15px 0;
        }
    </style>
</head>
<body>
    <div class="header">
        <h1>KrakenHashes</h1>
    </div>
    <div class="content">
        <h2>Webhook Delivery Failed</h2>
        <p>A webhook notification could not be delivered.</p>
        <div class="details">
            <h3>Webhook Details</h3>
            <p><strong>Webhook Name:</strong> {{ .WebhookName }}</p>
            <p><strong>URL:</strong> {{ .WebhookURL }}</p>
        </div>
        <div class="error-box">
            <h3>Error</h3>
            <p>{{ .Error }}</p>
        </div>
        <p>Please check your webhook configuration and ensure the endpoint is accessible.</p>
    </div>
</body>
</html>',
 'WEBHOOK DELIVERY FAILED

A webhook notification could not be delivered.

Webhook Details:
- Webhook Name: {{ .WebhookName }}
- URL: {{ .WebhookURL }}

Error:
{{ .Error }}

Please check your webhook configuration and ensure the endpoint is accessible.')
ON CONFLICT (template_type) DO NOTHING;
