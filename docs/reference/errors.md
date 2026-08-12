# KrakenHashes Error Codes Reference

This document provides a comprehensive reference for all error codes, HTTP status codes, and error conditions used throughout the KrakenHashes system.

## Table of Contents

- [HTTP Status Codes](#http-status-codes)
- [Application Error Types](#application-error-types)
- [Agent Error Conditions](#agent-error-conditions)
- [WebSocket Error Messages](#websocket-error-messages)
- [Common Error Scenarios](#common-error-scenarios)

## HTTP Status Codes

The KrakenHashes API uses standard HTTP status codes to indicate the success or failure of requests.

### Success Codes (2xx)

| Code | Name | Usage |
|------|------|-------|
| 200 | OK | Standard successful response for GET, PUT, DELETE requests |
| 201 | Created | Resource successfully created (POST requests) |
| 204 | No Content | Successful request with no response body (DELETE requests) |

### Client Error Codes (4xx)

| Code | Name | Common Usage |
|------|------|--------------|
| 400 | Bad Request | Invalid request format, missing required fields, validation errors |
| 401 | Unauthorized | Missing or invalid authentication credentials |
| 403 | Forbidden | Valid credentials but insufficient permissions |
| 404 | Not Found | Requested resource does not exist |
| 409 | Conflict | Conflict with current state (e.g., duplicate records) |

### Server Error Codes (5xx)

| Code | Name | Usage |
|------|------|-------|
| 500 | Internal Server Error | Unexpected server error, database errors, system failures |
| 502 | Bad Gateway | WebSocket upgrade failures, proxy errors |
| 503 | Service Unavailable | Server temporarily unavailable (maintenance, overload) |

## Application Error Types

### Model Layer Errors (`backend/internal/models/errors.go`)

```go
var (
    ErrNotFound     = errors.New("record not found")
    ErrInvalidInput = errors.New("invalid input")
)
```

### Repository Layer Errors (`backend/internal/repository/errors.go`)

```go
var (
    // Resource Errors
    ErrNotFound         = errors.New("resource not found")
    ErrDuplicateRecord  = errors.New("duplicate record")
    
    // Validation Errors
    ErrInvalidStatus    = errors.New("invalid status")
    ErrInvalidToken     = errors.New("invalid token")
    ErrInvalidHardware  = errors.New("invalid hardware information")
    ErrInvalidMetrics   = errors.New("invalid metrics")
    
    // Voucher Errors
    ErrInvalidVoucher      = errors.New("invalid or expired voucher")
    ErrVoucherAlreadyUsed  = errors.New("voucher has already been used")
    ErrVoucherDeactivated  = errors.New("voucher has been deactivated")
    ErrVoucherExpired      = errors.New("voucher has expired")
    
    // Agent Errors
    ErrDuplicateToken  = errors.New("agent token already exists")
    ErrAgentNotFound   = errors.New("agent not found")
)
```

## Agent Error Conditions

### Registration Errors

| Error | HTTP Status | Description |
|-------|-------------|-------------|
| Invalid claim code | 400 | The provided claim code is invalid or has been used |
| Expired claim code | 400 | The claim code has expired |
| Registration failed | 500 | Server error during agent registration |

### Authentication Errors

| Error | HTTP Status | Description |
|-------|-------------|-------------|
| Missing API key | 401 | API key header not provided |
| Invalid API key | 401 | API key does not match any registered agent |
| Agent ID mismatch | 401 | API key does not match the provided agent ID |
| TLS required | 400 | WebSocket connection requires TLS |

### Connection Errors

| Error | Description |
|-------|-------------|
| WebSocket upgrade failed | Failed to upgrade HTTP connection to WebSocket |
| Connection timeout | Agent failed to send heartbeat within timeout period |
| Invalid message format | WebSocket message does not match expected JSON format |

## WebSocket Error Messages

### Message Types

The WebSocket protocol uses typed messages for communication between agents and the server.

#### Server to Agent Error Messages

```json
{
    "type": "error_report",
    "payload": {
        "error": "error_message",
        "code": "ERROR_CODE",
        "details": {}
    }
}
```

#### Agent to Server Error Messages

```json
{
    "type": "error_report",
    "payload": {
        "agent_id": 123,
        "error": "error description",
        "stack": "stack trace if available",
        "context": {},
        "reported_at": "2025-01-20T10:30:00Z"
    }
}
```

### WebSocket Message Types

#### Agent → Server Messages

| Type | Purpose |
|------|---------|
| `heartbeat` | Regular heartbeat to maintain connection |
| `task_status` | Task execution status updates |
| `job_progress` | Job progress updates |
| `benchmark_result` | GPU benchmark results |
| `agent_status` | Agent status changes |
| `error_report` | Error reporting |
| `hardware_info` | Hardware capability updates |
| `file_sync_response` | File synchronization responses |
| `file_sync_status` | File sync progress updates |
| `hashcat_output` | Hashcat execution output |
| `device_detection` | GPU device detection results |
| `device_update` | GPU device status updates |

#### Server → Agent Messages

| Type | Purpose |
|------|---------|
| `task_assignment` | New task assignment |
| `job_stop` | Stop job execution |
| `benchmark_request` | Request GPU benchmark |
| `agent_command` | Generic agent command |
| `config_update` | Configuration updates |
| `file_sync_request` | Request file inventory |
| `file_sync_command` | File download commands |
| `force_cleanup` | Force cleanup of resources |

## Common Error Scenarios

### Authentication Flow Errors

1. **Login Failures**
   - Invalid credentials → 401 Unauthorized
   - System user login attempt → 401 Unauthorized
   - MFA required but not provided → Response with MFA session

2. **Token Errors**
   - Expired access token → 401 Unauthorized
   - Invalid refresh token → 401 Unauthorized
   - Expired refresh token → 401 Unauthorized

3. **MFA Errors**
   - Invalid TOTP code → 400 Bad Request
   - Invalid email code → 400 Bad Request
   - Expired MFA session → 401 Unauthorized
   - Invalid backup code → 400 Bad Request

### File Operation Errors

1. **Upload Errors**
   - File too large → 400 Bad Request
   - Invalid file type → 400 Bad Request
   - Disk space exceeded → 507 Insufficient Storage

2. **Download Errors**
   - File not found → 404 Not Found
   - Access denied → 403 Forbidden

### Job Execution Errors

1. **Job Creation**
   - Invalid job parameters → 400 Bad Request
   - Missing required fields → 400 Bad Request
   - Invalid hashlist ID → 404 Not Found

2. **Job Execution**
   - No available agents → 503 Service Unavailable
   - Agent disconnected → Task is truncated-and-completed, deleted, or cancelled, and its keyspace range is re-dispatched. The job is **not** failed. See [Task Lifecycle and Statuses](../troubleshooting/task-lifecycle.md#what-happens-when-a-task-is-stopped)
   - Hashcat execution error → Agent-reported task failure (`failed`), which fails the whole job

### Database Errors

1. **Connection Errors**
   - Database unreachable → 500 Internal Server Error
   - Connection pool exhausted → 500 Internal Server Error

2. **Query Errors**
   - Record not found → 404 Not Found
   - Duplicate key violation → 409 Conflict
   - Foreign key constraint → 400 Bad Request

## Error Resolution Guide

### For API Consumers

1. **401 Unauthorized**
   - Check if access token is expired
   - Refresh token if needed
   - Ensure proper authentication headers

2. **403 Forbidden**
   - Verify user has required permissions
   - Check role-based access requirements

3. **404 Not Found**
   - Verify resource ID is correct
   - Check if resource was deleted

4. **500 Internal Server Error**
   - Retry with exponential backoff
   - Check server logs for details
   - Contact support if persistent

### For Agent Developers

1. **WebSocket Connection Issues**
   - Ensure TLS is enabled
   - Verify API key is valid
   - Check network connectivity

2. **File Sync Errors**
   - Verify file permissions
   - Check disk space
   - Ensure file hashes match

3. **Job Execution Errors**
   - Check hashcat installation
   - Verify GPU drivers
   - Monitor system resources

## Error Logging

All errors are logged with appropriate severity levels:

- **DEBUG**: Detailed debugging information
- **INFO**: General informational messages
- **WARNING**: Warning messages for potential issues
- **ERROR**: Error messages for failures
- **CRITICAL**: Critical system failures

Error logs include:
- Timestamp
- Error message
- Stack trace (when available)
- Request context
- User/Agent information

## Best Practices

1. **Client-Side Error Handling**
   - Always check HTTP status codes
   - Parse error response bodies
   - Implement retry logic for transient errors
   - Display user-friendly error messages

2. **Server-Side Error Handling**
   - Use consistent error formats
   - Include helpful error details
   - Log errors with appropriate context
   - Monitor error rates and patterns

3. **Agent Error Handling**
   - Report errors via WebSocket
   - Implement local error recovery
   - Maintain error history
   - Include system state in error reports