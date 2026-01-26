# Troubleshooting Potfile Issues

## Potfile Preset Job Not Created

### Symptoms
- Potfile wordlist exists in Resources → Wordlists
- No "Potfile Run" job in Admin → Preset Jobs
- Logs show "No binary versions found" messages

### Cause
The potfile preset job requires a hashcat binary to be uploaded. On fresh installations, no binaries exist, so the preset job cannot be created (it needs a binary version pattern to resolve).

### Solution
1. Upload a hashcat binary via Admin → Binary Management
2. Wait for verification to complete
3. Check Admin → Preset Jobs - "Potfile Run" should appear within 5-10 seconds
4. If not visible after 30 seconds, restart the backend service

### Prevention
Always upload a hashcat binary as the first step after installation. This is documented in the Quick Start and First Crack guides.

### Verification
Check system logs for confirmation:
```bash
docker logs krakenhashes 2>&1 | grep -i "potfile preset job"
```

You should see one of these messages:
- "Successfully created pot-file preset job with ID: [uuid]" - Success
- "Waiting for binary versions to be added before creating pot-file preset job" - Still waiting
- "Found existing pot-file preset job with ID" - Already exists

### Technical Details

The potfile system initializes in two stages:

1. **Potfile Wordlist Creation** (always succeeds):
   - Creates `/data/krakenhashes/wordlists/custom/potfile.txt`
   - Adds wordlist entry to database with `is_potfile = true`
   - Sets system setting `potfile_wordlist_id`

2. **Preset Job Creation** (requires binary):
   - Attempts to create "Potfile Run" preset job
   - Requires at least one active binary for pattern resolution
   - If no binaries exist, starts background monitor
   - Monitor checks every 5 seconds for binary availability
   - Creates preset job once binary is uploaded

## Potfile Not Being Updated

### Symptoms
- Cracked passwords not appearing in potfile
- Potfile size not increasing
- Staging table has entries but potfile is unchanged

### Common Causes and Solutions

1. **Potfile Disabled**
   - Check: `SELECT value FROM system_settings WHERE key = 'potfile_enabled';`
   - Fix: `UPDATE system_settings SET value = 'true' WHERE key = 'potfile_enabled';`
   - Restart backend service

2. **Background Worker Stopped**
   - Check logs: `docker logs krakenhashes 2>&1 | grep "pot-file service"`
   - Look for: "Pot-file service started" vs error messages
   - Fix: Restart backend service

3. **Staging Table Processing Issues**
   - Check staging count: `SELECT COUNT(*) FROM potfile_staging;`
   - Check for old entries: `SELECT COUNT(*) FROM potfile_staging WHERE created_at < NOW() - INTERVAL '1 hour';`
   - If stuck, manually clear: `DELETE FROM potfile_staging WHERE created_at < NOW() - INTERVAL '1 hour';`

4. **File Permission Issues**
   - Check file exists: `ls -la /data/krakenhashes/wordlists/custom/potfile.txt`
   - Check permissions allow writing
   - Fix: `chmod 644 /data/krakenhashes/wordlists/custom/potfile.txt`

## Potfile Wordlist Shows Wrong Count

### Symptoms
- Wordlist entry shows incorrect word count
- Keyspace calculations are wrong
- Jobs using potfile have incorrect progress

### Solution
The system should automatically update counts during batch processing. If not:

1. Check recent batch processing:
   ```sql
   SELECT * FROM wordlists WHERE is_potfile = true;
   ```

2. Manually trigger recount (requires backend restart):
   ```sql
   UPDATE wordlists 
   SET word_count = (SELECT COUNT(*) FROM potfile_lines),
       updated_at = NOW()
   WHERE is_potfile = true;
   ```

3. Update preset job keyspace:
   ```sql
   UPDATE preset_jobs 
   SET keyspace = (SELECT word_count FROM wordlists WHERE is_potfile = true)
   WHERE name = 'Potfile Run';
   ```

## Duplicate Passwords in Potfile

### Symptoms
- Same password appears multiple times
- File size larger than expected
- Performance degradation

### Causes
- Manual editing of potfile while system is running
- Database/filesystem sync issues
- Processing errors

### Solution
1. Stop the backend service
2. Back up the current potfile
3. Remove duplicates:
   ```bash
   sort -u /data/krakenhashes/wordlists/custom/potfile.txt > /tmp/potfile_clean.txt
   mv /tmp/potfile_clean.txt /data/krakenhashes/wordlists/custom/potfile.txt
   ```
4. Update database:
   ```sql
   UPDATE wordlists 
   SET word_count = (SELECT COUNT(*) FROM potfile_lines),
       file_size = pg_stat_file('/data/krakenhashes/wordlists/custom/potfile.txt').size,
       updated_at = NOW()
   WHERE is_potfile = true;
   ```
5. Restart backend service

## Monitor Not Creating Preset Job

### Symptoms
- Binary uploaded but preset job still missing
- Logs show monitor is running but not creating job
- System seems stuck waiting

### Debugging Steps

1. Check if monitor is running:
   ```bash
   docker logs krakenhashes 2>&1 | grep "monitor for binary versions"
   ```

2. Check for any binaries:
   ```sql
   SELECT id, binary_type, is_active, verification_status 
   FROM binary_versions;
   ```

3. Check system settings:
   ```sql
   SELECT * FROM system_settings 
   WHERE key IN ('potfile_wordlist_id', 'potfile_preset_job_id');
   ```

4. Force retry by clearing preset job ID:
   ```sql
   UPDATE system_settings 
   SET value = NULL 
   WHERE key = 'potfile_preset_job_id';
   ```
   Then restart backend

### Manual Creation (Last Resort)

If automatic creation fails, manually create the preset job:

```sql
-- Get the wordlist ID
SELECT id FROM wordlists WHERE is_potfile = true;

-- Get a binary version ID
SELECT id FROM binary_versions WHERE is_active = true LIMIT 1;

-- Create the preset job
INSERT INTO preset_jobs (
    id, name, wordlist_ids, rule_ids, attack_mode,
    priority, chunk_size_seconds, status_updates_enabled,
    allow_high_priority_override, binary_version, keyspace
) VALUES (
    gen_random_uuid(),
    'Potfile Run',
    '["WORDLIST_ID"]'::jsonb,  -- Replace WORDLIST_ID
    '[]'::jsonb,
    0,  -- Dictionary attack
    1000,  -- Max priority
    1200,  -- 20 minute chunks
    true,
    true,
    'default',  -- Uses default binary pattern
    1  -- Initial keyspace
);

-- Update system settings with the new ID
UPDATE system_settings 
SET value = (SELECT id::text FROM preset_jobs WHERE name = 'Potfile Run')
WHERE key = 'potfile_preset_job_id';
```

## Best Practices

1. **Always upload a binary first** during initial setup
2. **Don't manually edit the potfile** while the system is running
3. **Monitor staging table size** - large backlogs indicate processing issues
4. **Check logs regularly** for potfile-related errors
5. **Keep batch intervals reasonable** (30-60 seconds recommended)
6. **Archive old potfiles** if they grow beyond 1GB

## Related Documentation

- [Potfile Management Guide](../admin-guide/operations/potfile.md)
- [Binary Management](../admin-guide/resource-management/binaries.md)