# KrakenHashes Chunking System

## Overview

KrakenHashes uses an intelligent chunking system to distribute password cracking workloads across multiple agents. This document explains how chunks are created, distributed, and tracked for different attack types.

## What is Chunking?

Chunking divides large password cracking jobs into smaller, manageable pieces that can be:
- Distributed across multiple agents for parallel processing
- Completed within a reasonable time frame (default: 20 minutes)
- Resumed if interrupted or failed
- Tracked for accurate progress reporting

## How Chunking Works

### Basic Chunking (No Rules)

For simple dictionary attacks without rules:
1. The system calculates the total keyspace (number of password candidates)
2. Based on agent benchmark speeds, it determines optimal chunk sizes
3. Each chunk processes a portion of the wordlist using hashcat's `--skip` and `--limit` parameters

**Example**: 
- Wordlist: 1,000,000 passwords
- Agent speed: 1,000,000 H/s
- Target chunk time: 1,200 seconds (20 minutes)
- Chunk size: 1,200,000,000 candidates
- Result: Single chunk processes entire wordlist

### Enhanced Chunking with Rules

When rules are applied, the effective keyspace multiplies:

**Effective Keyspace = Wordlist Size × Number of Rules**

For example:
- Wordlist: 1,000,000 passwords
- Rules: 1,000 rules
- Effective keyspace: 1,000,000,000 candidates

#### Rule Splitting

When a job with rules would take significantly longer than the target chunk time, KrakenHashes can split the rules:

1. **Detection**: If estimated time > 2× target chunk time
2. **Splitting**: Divides rules into smaller files
3. **Distribution**: Each agent receives full wordlist + partial rules
4. **Progress**: Tracks completion across all rule chunks

**Example**:
- Wordlist: 1,000,000 passwords
- Rules: 10,000 rules
- Agent speed: 1,000,000 H/s
- Without splitting: 10,000 seconds (2.8 hours) per chunk
- With splitting into 10 chunks: 1,000 rules each, ~1,000 seconds per chunk

### Combination Attacks

For combination attacks (-a 1), the effective keyspace is:

**Effective Keyspace = Wordlist1 Size × Wordlist2 Size**

The system tracks progress through the virtual keyspace while hashcat processes the first wordlist sequentially.

### Attack Mode Support

| Attack Mode | Description | Chunking Method |
|------------|-------------|-----------------|
| 0 (Straight) | Dictionary | Wordlist position + optional rule splitting |
| 1 (Combination) | Two wordlists | Virtual keyspace tracking |
| 3 (Brute-force) | Mask attack | Mask position chunking |
| 6 (Hybrid W+M) | Wordlist + Mask | Wordlist position chunking |
| 7 (Hybrid M+W) | Mask + Wordlist | Mask position chunking |
| 9 (Association) | Per-hash rules | Rule splitting when applicable |

## Progress Tracking

### Standard Progress
- Shows candidates tested vs total keyspace
- Updates in real-time via WebSocket
- Accurate percentage completion

### With Rule Multiplication
- Display format: "X / Y (×Z)" where Z is the multiplication factor
- Accounts for all rules across all chunks
- Aggregates progress from distributed rule chunks

### Progress Bar Visualization
The progress bar always shows:
- Green: Completed keyspace
- Gray: Remaining keyspace
- Percentage: Based on effective keyspace

## Configuration

Administrators can tune chunking behavior via system settings:

| Setting | Default | Description |
|---------|---------|-------------|
| `default_chunk_duration` | 1200s | Target time per chunk (20 minutes) |
| `chunk_fluctuation_percentage` | 20% | Threshold for merging final chunks |
| `rule_split_enabled` | true | Enable automatic rule splitting |
| `rule_split_threshold` | 2.0 | Time multiplier to trigger splitting |
| `rule_split_min_rules` | 100 | Minimum rules before considering split |

## Best Practices

### For Users
1. **Large Rule Files**: Will automatically split for better distribution
2. **Multiple Rule Files**: Multiplication is handled automatically
3. **Progress Monitoring**: Check effective keyspace in job details
4. **Benchmarks**: Ensure agents have current benchmarks for accurate chunking

### For Administrators
1. **Chunk Duration**: Balance between progress granularity and overhead
2. **Rule Splitting**: Monitor temp directory space for large rule files
3. **Benchmarks**: Configure benchmark validity period appropriately
4. **Resource Usage**: Rule splitting creates temporary files

## Troubleshooting

### Slow Progress
- Check if effective keyspace is much larger than expected
- Verify agent benchmarks are current
- Consider enabling rule splitting if disabled

### Uneven Distribution
- Some chunks may be larger due to:
  - Fluctuation threshold preventing small final chunks
  - Rule count not evenly divisible
  - Different agent speeds

### Rule Splitting Not Occurring
Verify:
- `rule_split_enabled` is true
- Rule file has > `rule_split_min_rules` rules
- Estimated time exceeds threshold

## Technical Details

### Keyspace Calculation

```
Attack Mode 0 (Dictionary):
- Without rules: wordlist_size
- With rules: wordlist_size × total_rule_count

Attack Mode 1 (Combination):
- Always: wordlist1_size × wordlist2_size

Attack Mode 3 (Brute-force):
- Calculated from mask: charset_size^length

Attack Mode 6/7 (Hybrid):
- Wordlist_size × mask_keyspace
```

### Chunk Assignment

1. Agent requests work
2. System calculates optimal chunk size based on:
   - Agent's benchmark speed
   - Target chunk duration
   - Remaining keyspace
3. Chunk boundaries determined:
   - Start position (skip)
   - Chunk size (limit)
4. Agent receives chunk assignment
5. Progress tracked and aggregated

### Rule Chunk Files

When rule splitting is active:
- Temporary files created in configured directory
- Named: `job_[ID]_chunk_[N].rule`
- Automatically cleaned up after job completion
- Synced to agents like normal rule files

## Salted Hash Considerations

### Understanding Salt Impact

For hash types that use per-hash salts (e.g., NetNTLMv2, bcrypt, scrypt), chunk calculations behave differently because hashcat reports speed as `hash_ops/sec` rather than `candidates/sec`.

**Key Insight:**
```
For salted hashes: hash_ops/sec = candidate_rate × salt_count
```

The remaining uncracked hashes act as the effective salt count.

### How the System Adjusts

KrakenHashes automatically detects salted hash types and adjusts:

1. **Benchmark Caching**: Stores benchmarks per salt count, not just per hash type
2. **Speed Calculation**: Divides reported speed by remaining hash count
3. **Chunk Sizing**: Uses adjusted candidate speed for accurate chunk duration

**Example: NetNTLMv2 Job**

| Metric | Without Adjustment | With Adjustment |
|--------|-------------------|-----------------|
| Reported speed | 500 MH/s | 500 MH/s |
| Remaining hashes | 5,000 | 5,000 |
| Actual candidate speed | 500 MH/s (wrong!) | 100 KH/s (correct) |
| Chunk size (20 min) | 600 billion | 120 million |

Without adjustment, chunks would be 5,000× too large!

### Salted Hash Type Examples

The following are automatically classified as salted:

| Category | Examples |
|----------|----------|
| **Network Auth** | NetNTLMv1 (5500), NetNTLMv2 (5600), Kerberos |
| **Password Hashing** | bcrypt, scrypt, PBKDF2, Argon2, md5crypt |
| **Disk Encryption** | VeraCrypt, TrueCrypt, LUKS, BitLocker |
| **Password Managers** | KeePass, 1Password, LastPass, Bitwarden |

### Configuration Impact

No special configuration is needed - the system automatically:
- Detects salted hash types via the `is_salted` database flag
- Calculates salt count from remaining uncracked hashes
- Adjusts benchmark lookups and chunk calculations

### Performance Implications

**As hashes crack**, the salt count decreases:
- Fewer salts = higher candidate throughput
- Chunk sizes increase proportionally
- Job ETA becomes more accurate over time

**Initial chunks** may be conservative:
- Full salt count at job start
- System becomes more accurate as cracking progresses

### Monitoring Salted Hash Jobs

Watch for these patterns:

**Healthy progress:**
```
INFO: Salt count: 5000, adjusted speed: 100 KH/s
INFO: Chunk calculated: 120M candidates (~20 min)
```

**After cracking:**
```
INFO: Salt count: 1000 (4000 cracked), adjusted speed: 500 KH/s
INFO: Chunk calculated: 600M candidates (~20 min)
```

### Troubleshooting

**Chunks too large/slow:**
- Verify hash type has `is_salted = true` in database
- Check benchmark was captured with correct salt count
- Force re-benchmark if hash count changed significantly

**Chunks too small/fast:**
- Normal for salted hashes with many remaining targets
- Chunk duration will normalize as hashes crack

## Future Enhancements

- Pre-calculation of optimal chunk distribution
- Dynamic chunk resizing based on actual speed
- Rule deduplication before splitting
- Compression for rule chunk transfers
- Salt-count-aware benchmark interpolation