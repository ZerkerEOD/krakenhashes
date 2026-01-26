# Agent File Synchronization

This document explains how KrakenHashes agents synchronize files with the backend server.

## Overview

KrakenHashes agents need access to the same wordlists and rules as the backend server to perform password cracking operations. The system implements a WebSocket-based file synchronization mechanism to ensure agents have the necessary files.

## Synchronization Process

The file synchronization process follows these steps:

1. When an agent connects to the backend server via WebSocket, the server initiates a file synchronization request
2. The agent scans its local directories and reports all files with their MD5 hashes
3. The server compares the agent's files with its database and identifies missing or outdated files
4. The server sends a synchronization command with a list of files the agent should download
5. The agent downloads each file in parallel from the backend server

## File Types

The system synchronizes the following types of files:

- **Wordlists**: Password dictionaries used for cracking
- **Rules**: Hashcat and John the Ripper rule files for password mutations
- **Binaries**: Tool binaries (future implementation)

## Directory Structure

Agents store synchronized files in a data directory structure:

```
<data_dir>/
├── wordlists/
│   ├── general/
│   ├── specialized/
│   ├── targeted/
│   ├── custom/
│   └── association/    # Association wordlists (v1.4.0+)
├── rules/
│   ├── hashcat/
│   ├── john/
│   └── custom/
├── binaries/
└── hashlists/
```

The base data directory location is determined by:

1. The `KH_DATA_DIR` environment variable, if set
2. Otherwise, a `data` directory relative to the agent executable

## WebSocket Messages

The file synchronization uses the following WebSocket message types:

### File Sync Request

Sent from server to agent to request a list of files:

```json
{
  "type": "file_sync_request",
  "payload": {
    "file_types": ["wordlist", "rule", "binary"]
  },
  "timestamp": "2023-07-01T12:00:00Z"
}
```

### File Sync Response

Sent from agent to server with the list of files:

```json
{
  "type": "file_sync_response",
  "payload": {
    "agent_id": 123,
    "files": [
      {
        "name": "rockyou.txt",
        "file_type": "wordlist",
        "hash": "7bfc9d4df2b5ce4e29ca14d40f7aef1b",
        "size": 139921507
      },
      {
        "name": "best64.rule",
        "file_type": "rule",
        "hash": "1e5f4a7e3cc31bd12a0f7a42c6ebab29",
        "size": 1234
      }
    ]
  },
  "timestamp": "2023-07-01T12:00:05Z"
}
```

### File Sync Command

Sent from server to agent with files to download:

```json
{
  "type": "file_sync_command",
  "payload": {
    "files": [
      {
        "name": "darkweb2017.txt",
        "file_type": "wordlist",
        "hash": "8b1a9953c4611296a827abf8c47804d7",
        "size": 8553126
      }
    ]
  },
  "timestamp": "2023-07-01T12:00:10Z"
}
```

## File Download Process

When an agent receives a file sync command:

1. It processes each file in the command asynchronously
2. For each file, it creates the appropriate directory structure if needed
3. It downloads the file from the backend server's file API endpoint
4. It verifies the downloaded file's MD5 hash matches the expected hash
5. If verification fails, it retries the download (up to 3 times)

## Synchronization Timing

File synchronization occurs at the following times:

1. When an agent first connects to the backend server
2. Periodically (every 6 hours by default)
3. When the backend server explicitly requests synchronization (e.g., after new files are added)

## Error Handling

The system implements several error handling mechanisms:

- Download timeouts (1 hour per file)
- Retry logic for failed downloads (3 attempts with exponential backoff)
- Partial file cleanup if a download is interrupted
- Verification of file integrity via MD5 hash

## Security Considerations

All file transfers occur over secure HTTPS connections with:

- TLS encryption for all communications
- Agent authentication required for file downloads
- File integrity verification via MD5 hash

## Monitoring

Administrators can monitor file synchronization through:

1. Agent logs, which show detailed information about file downloads
2. Backend server logs, which show synchronization requests and commands
3. The admin dashboard, which displays synchronization status for each agent

## Best Practices

1. **Ensure adequate storage**: Agents need sufficient disk space for wordlists and rules
2. **Monitor bandwidth usage**: Large file transfers may impact network performance
3. **Stagger agent registrations**: To prevent overwhelming the server with simultaneous downloads
4. **Pre-populate common files**: For faster agent setup, pre-copy large wordlists to agent machines 