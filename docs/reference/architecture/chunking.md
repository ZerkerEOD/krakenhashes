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

## Accurate Keyspace Tracking from Hashcat

### Overview
KrakenHashes captures actual keyspace values directly from hashcat's `progress[1]` field to ensure precise progress reporting, especially for jobs with rules or combination attacks where estimation can be inaccurate.

### How It Works

1. **Initial Job Creation**: Job created with estimated keyspace based on wordlists/rules, `is_accurate_keyspace = false`
2. **First Progress Update**: Agent sends `progress[1]` value from hashcat, backend captures it as `chunk_actual_keyspace`
3. **Cascade Recalculation**: All subsequent chunks' start/end positions are recalculated using cumulative actual keyspace from completed chunks
4. **Self-Correcting**: The system handles out-of-order chunk completion correctly by recalculating all subsequent chunks whenever a chunk receives its actual keyspace

### Technical Details

**Chunk Actual Keyspace:**
- Each chunk's `chunk_actual_keyspace` field stores the immutable size from hashcat's `progress[1]` value
- This represents the actual keyspace processed by hashcat for that specific chunk
- Used to calculate exact start/end positions for subsequent chunks

**Cascade Updates:**
When a chunk completes and provides its actual keyspace:
1. Backend updates the chunk's `chunk_actual_keyspace` and `effective_keyspace_end`
2. All subsequent chunks (higher chunk numbers) have their positions recalculated
3. New chunk start position = sum of all previous chunks' actual keyspace
4. Works correctly even if chunks complete out of order

**Dispatched Keyspace Adjustments:**
- When actual keyspace differs from estimate, `dispatched_keyspace` is adjusted
- Ensures job progress accurately reflects work distribution

### Database Schema

**job_tasks table:**
- `chunk_actual_keyspace` (BIGINT): Immutable chunk size from hashcat `progress[1]`
- `is_actual_keyspace` (BOOLEAN): True when task has actual keyspace from hashcat

**job_executions table:**
- `is_accurate_keyspace` (BOOLEAN): True when keyspace is from hashcat `progress[1]`
- `avg_rule_multiplier` (FLOAT): Actual/estimated ratio for improving future estimates

### Benefits

1. **Precise Progress**: Progress percentages match hashcat's actual progress, not estimates
2. **Better Task Distribution**: Chunk sizes calculated based on real keyspace
3. **Improved Future Estimates**: Multiplier derived from actual values helps estimate remaining work
4. **Self-Correcting Ranges**: Chunks automatically adjust when actual differs from estimate
5. **No Backwards Ranges**: Ensures ascending keyspace ranges (10.45T → 20.14T, not 10.45T → 9.60T)

### Example

**Before accurate tracking:**
- Estimated: 10.00T per chunk
- Reality: 10.51T, 9.63T, 9.71T (varies by rule effectiveness)
- Problem: Progress bars and ETAs inaccurate

**After accurate tracking:**
- Chunk 1: 0 → 10.51T (actual from hashcat)
- Chunk 2: 10.51T → 20.14T (starts where chunk 1 ended)
- Chunk 3: 20.14T → 29.85T (continues from chunk 2)
- Result: Perfect cumulative chain with accurate progress

See [Benchmark Workflow](./benchmark-workflow.md) for more details on keyspace capture during benchmarking.

## Salt-Aware Chunk Calculations

For hash types that use per-hash salts, chunk calculations require special handling to account for how hashcat reports speed.

### The Salt Factor

Hashcat reports speed differently for salted vs. non-salted hashes:

| Hash Type | Speed Metric | Example |
|-----------|--------------|---------|
| Non-salted (NTLM) | Candidates/sec | 1 GH/s = 1B candidates/sec |
| Salted (NetNTLMv2) | Hash-ops/sec | 1 GH/s = candidate_rate × salt_count |

For salted hashes, the remaining uncracked hashes act as the salt count. The actual candidate throughput is:

```
candidate_speed = benchmark_speed / remaining_hashes
```

### Why This Matters for Chunking

Without salt-aware calculations, chunks for salted hash jobs would be dramatically oversized:

**Example: NetNTLMv2 (mode 5600) with 5000 remaining hashes**

| Calculation Type | Benchmark Speed | Candidate Speed | Chunk Size (20 min) |
|-----------------|-----------------|-----------------|---------------------|
| **Incorrect** (ignoring salts) | 500 MH/s | 500 MH/s | 600B candidates |
| **Correct** (salt-aware) | 500 MH/s | 100 KH/s | 120M candidates |

The incorrect calculation produces chunks 5000× too large!

### Chunk Calculation Request

The chunking service now accepts salt-related parameters:

```go
type ChunkCalculationRequest struct {
    // ... existing fields ...

    // Salt-aware fields
    IsSalted      bool   // Whether hash type uses per-hash salts
    TotalHashes   int    // Total hashes in hashlist
    CrackedHashes int    // Number of cracked hashes
}
```

**Salt count is calculated as:**
```go
saltCount := request.TotalHashes - request.CrackedHashes
```

### Speed Adjustment Algorithm

When calculating chunks for salted hashes:

```go
func (s *ChunkingService) CalculateNextChunk(request ChunkCalculationRequest) *ChunkResult {
    benchmarkSpeed := s.GetBenchmarkSpeed(agentID, attackMode, hashType, saltCount)

    // Adjust for salt count
    var candidateSpeed int64
    if request.IsSalted && remainingHashes > 0 {
        // Hashcat reports hash_ops/sec for salted hashes
        // Convert to actual candidate throughput
        candidateSpeed = benchmarkSpeed / remainingHashes
    } else {
        candidateSpeed = benchmarkSpeed
    }

    // Calculate chunk size using adjusted speed
    chunkSize := candidateSpeed * targetChunkDuration

    return &ChunkResult{Size: chunkSize}
}
```

### Impact on Rule Splitting Decisions

Rule splitting decisions are also affected by salt-aware calculations:

1. **At job creation**: System checks if rule splitting should be enabled
2. **Comparison uses actual rule count**: Not salt-adjusted keyspace
3. **Prevents false positives**: Without this, salt multiplication could incorrectly trigger rule splitting

**Example:**
```
Wordlist: 1M words
Rules: 50 rules (below rule_split_min_rules of 100)
Hashes: 10,000 (salt count)

Salt-adjusted keyspace: 1M × 50 × 10,000 = 500T
Actual rule count: 50 (no rule splitting needed)
```

### Job Completion Estimation

The ETA calculation for jobs with salted hashes also uses adjusted speeds:

```go
func (s *ChunkingService) EstimateJobCompletion(request JobCompletionEstimateRequest) {
    avgSpeed := s.GetAverageAgentSpeed(hashType, attackMode, saltCount)

    if request.IsSalted && remainingHashes > 0 {
        avgSpeed = avgSpeed / remainingHashes
    }

    remainingKeyspace := totalKeyspace - completedKeyspace
    estimatedSeconds := remainingKeyspace / avgSpeed
}
```

### Salt Count Changes During Cracking

As hashes get cracked, the salt count decreases:

```
Job start:   5000 hashes → salt_count = 5000 → candidate_speed = X
After 1000:  4000 hashes → salt_count = 4000 → candidate_speed = X × 1.25
After 4000:  1000 hashes → salt_count = 1000 → candidate_speed = X × 5
```

The system handles this by:
1. Recalculating salt count for each new chunk
2. Using updated benchmarks when available
3. Progressively more accurate ETA as job progresses

### Salted Hash Types

The following hash types are automatically classified as salted:

- **Network Authentication**: NetNTLMv1 (5500), NetNTLMv2 (5600), Kerberos family
- **Password Hashing**: bcrypt, scrypt, PBKDF2, Argon2, all crypt variants
- **Encryption**: VeraCrypt, TrueCrypt, LUKS, BitLocker
- **Applications**: KeePass, 1Password, LastPass, Bitwarden

See [Hash Types Reference](../hash-types.md#salted-hash-type-classification) for the complete list.

## Dynamic Updates During Execution

When wordlists, rules, or potfiles are modified while jobs are running, the chunking system adapts:

### Automatic Recalculation
- **Keyspace Updates**: Effective keyspace recalculates based on current file state
- **Forward-Only**: Only undispatched chunks are affected by changes
- **No Interruption**: Active chunks continue with their original parameters

### Update Scenarios
1. **Wordlist Growth**: New words available for future chunks (if not using rule splitting)
2. **Rule Changes**: Multiplication factor adjusts for remaining work
3. **Potfile Updates**: Manual refresh triggers keyspace recalculation

For detailed information about how file updates affect running jobs, see the [Job Update System documentation](./job-update-system.md).

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

## Future Enhancements

- Pre-calculation of optimal chunk distribution
- Dynamic chunk resizing based on actual speed
- Rule deduplication before splitting
- Compression for rule chunk transfers