# Wordlists and Rules Management

KrakenHashes provides automatic monitoring and management of wordlists and rules used for password cracking operations. This document explains how the system handles these files and provides best practices for administrators.

## Directory Structure

The system monitors two specific directories for files:

- **Wordlists Directory**: `<data_dir>/wordlists/`
- **Rules Directory**: `<data_dir>/rules/`

The system automatically creates the following subdirectory structure:

**Wordlists:**
```
wordlists/
├── general/       # Common wordlists for general use
├── specialized/   # Domain-specific wordlists
├── targeted/      # Target-specific wordlists
├── custom/        # User-created or modified wordlists
└── clients/       # Auto-managed client potfiles & wordlists (NOT monitored)
    └── {uuid}/
        ├── potfile.txt     # Auto-generated client potfile
        └── *.txt           # Uploaded client wordlists
```

> **Important**: The `wordlists/clients/` subdirectory is **auto-managed** by the client potfile and client wordlist systems. It is explicitly **excluded from directory monitoring** — the monitor performs a prefix check and skips any file path starting with `clients/`. Files placed here manually will NOT be detected, imported, or counted. Client wordlists must be uploaded through the Client Management UI (Admin → Clients → Wordlists action) or the `POST /api/clients/{id}/wordlists` API endpoint. Client potfiles are generated automatically from cracked passwords. See the [Client Management](../operations/clients.md) guide for details.

**Rules:**
```
rules/
├── hashcat/       # Hashcat-compatible rules
├── john/          # John the Ripper rules
└── custom/        # User-created or modified rules
```

## File Formats

### Wordlists
The system supports the following wordlist formats:
- **Plaintext**: `.txt`, `.lst`, `.dict` files
- **Compressed**: `.gz`, `.zip` files

### Rules
The system supports the following rule formats:
- **Hashcat**: Standard hashcat rule files
- **John**: John the Ripper rule files

## Auto-Monitoring Process

When files are added to these directories, the system automatically:

1. Detects new or modified files
2. Calculates MD5 hashes for integrity verification
3. Imports metadata into the database
4. Counts words/rules in the files
5. Makes them available for use in password cracking jobs

## File Upload Handling

When uploading files through the web interface:

1. The system preserves the original filename (with sanitization for security)
2. Files are automatically placed in the appropriate subdirectory based on their type
3. Duplicate detection is performed based on filename:
   - If a file with the same name exists and has the same MD5 hash, the upload is skipped
   - If a file with the same name exists but has a different MD5 hash, the file is updated
4. The system automatically calculates the MD5 hash and counts words/rules

### Duplicate Detection

The system handles duplicate files intelligently:

- **Same filename, same content**: The system will recognize the file as already existing and return the existing entry
- **Same filename, different content**: The system will update the existing file with the new content
- **Different filename, same content**: The system will store both files separately

This approach ensures that:
- Files are not unnecessarily duplicated
- Updates to existing files are properly tracked
- Users can maintain multiple versions of similar files with different names

## Auto-Monitoring Details

### System User

All auto-imported wordlists and rules are created in the database using a special system user (UUID: `00000000-0000-0000-0000-000000000000`). This user:

- Cannot be used for frontend login
- Is used exclusively for system-generated actions
- Helps track which files were auto-imported vs. manually added

### Monitoring Interval

- The system checks for new files every **5 minutes**
- Initial scan happens immediately when the server starts
- There's a small delay (2 seconds) after database migrations complete before monitoring starts to prevent race conditions

### File Detection Process

1. The system detects new or modified files in the monitored directories
2. Files are checked to ensure they're not still being transferred
3. MD5 hash is calculated for each file
4. If a file with the same name exists but has a different hash, it's updated
5. If a file with the same name and hash exists, it's skipped
6. New files are added to the database with "pending" verification status
7. File contents are counted (words or rules)
8. Status is updated to "verified" once counting is complete

## File Transfer Considerations

When transferring files to the monitored directories, be aware of the following:

- **File Stability Check**: The system waits for files to be stable (not actively changing) before processing them
- **SCP/SFTP Transfer Time**: For large files transferred via SCP or SFTP, allow sufficient time for the transfer to complete before the file will be processed
  - The system waits 30 seconds after the last modification before considering a file stable
  - For files larger than 100MB, the system also checks if the file size has changed

> **Note**: When using SCP to transfer large wordlists, the file may not be detected for import until 30 seconds after the transfer completes.

## File Size and Format Restrictions

### Size Restrictions

- **No file size limits**: The system can handle wordlists and rules of any size
- For very large files (>1GB), be aware that:
  - Initial hash calculation may take longer
  - Word/rule counting operations run in the background
  - The file will be available with a "pending" status until counting completes

## Automatic Tagging

Auto-imported files are automatically tagged for easy identification:

- All auto-imported files receive the tag `auto-imported`
- Updated files additionally receive the tag `updated`

The system does not automatically tag files based on subdirectories or other criteria.

## Rule Type Detection

The system automatically assigns a default rule type based on the file path:

- If the path contains "john" (case-insensitive), the rule is classified as a John the Ripper rule
- Otherwise, it's classified as a Hashcat rule

Administrators can edit the rule type after import if needed.

## Wordlist Type Classification

Wordlists are classified based on their subdirectory:

- **General**: Common wordlists suitable for most cracking jobs
- **Specialized**: Wordlists focused on specific patterns or domains
- **Targeted**: Wordlists tailored for specific targets
- **Custom**: User-created or modified wordlists

When uploading through the web interface, you can specify the wordlist type, which determines the subdirectory where the file will be stored.

## Best Practices

1. **Use descriptive filenames**: Filenames are used as the default name in the database
2. **Pre-verify large files**: For very large wordlists, consider pre-calculating word counts to display immediately
3. **Monitor the logs**: Check server logs for any import errors or issues
4. **Avoid frequent updates**: Updating large files frequently can cause unnecessary processing overhead
5. **Organize by type**: Use the appropriate wordlist type when uploading to keep files organized

## Monitoring Import Status

Administrators can check the status of file imports through:

1. The admin dashboard in the web interface
2. Server logs, which show detailed information about file processing
3. The database, where each wordlist and rule has a `verification_status` field

## Manual Management

While auto-importing is convenient, you can also manually:

1. Add wordlists and rules through the web interface
2. Update metadata for auto-imported files
3. Delete files that are no longer needed

> **Important**: Deleting a file from the database does not remove it from the filesystem. Similarly, removing a file from the filesystem will not automatically remove it from the database.

## Wordlist Types

Wordlists are categorized into the following types:
- **General**: Common wordlists for general use
- **Specialized**: Domain-specific wordlists
- **Targeted**: Target-specific wordlists
- **Custom**: User-created or modified wordlists 