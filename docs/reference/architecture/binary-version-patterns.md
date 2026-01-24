# Binary Version Patterns

## Overview

KrakenHashes uses a pattern-based system for specifying which hashcat binary versions agents should use and which binaries jobs require. This system replaced the previous integer ID-based approach, providing more flexibility for managing mixed-version environments.

The pattern system enables:
- **Flexible agent configuration**: Agents can specify what binaries they can run
- **Precise job requirements**: Jobs can require specific version families or exact versions
- **Intelligent scheduling**: The scheduler matches agents to jobs based on compatibility
- **Gradual rollouts**: Deploy new binary versions incrementally across your agent fleet

## Pattern Types

| Type | Example | Description |
|------|---------|-------------|
| Default | `default` | Matches any binary version |
| Major Wildcard | `7.x` | Matches any version with that major number (7.0.0, 7.1.2, 7.2.0, etc.) |
| Minor Wildcard | `7.1.x` | Matches any version with that major.minor (7.1.0, 7.1.2, 7.1.5, etc.) |
| Exact | `7.1.2` | Matches exactly that version, with any suffix (7.1.2, 7.1.2-custom) |
| Exact with Suffix | `7.1.2-NTLMv3` | Matches only that exact version and suffix |

### Pattern Syntax

Patterns are case-insensitive for the wildcard `x` component:
- `7.x` and `7.X` are equivalent
- `default` must be lowercase

Suffixes can use `-` or `+` as separators:
- `7.1.2-NTLMv3` (dash separator)
- `7.1.2+338` (plus separator, often used for build numbers)

## Compatibility Rules

When the scheduler determines if an agent can run a job, it checks if the agent's pattern is **compatible** with the job's pattern.

### Core Principle

An agent is compatible with a job if the agent can provide at least one binary version that satisfies the job's requirement.

### Default Pattern

- **Agent "default"**: Compatible with ANY job (the agent can run any binary)
- **Job "default"**: Compatible with ANY agent (the job accepts any binary)

### Wildcard Compatibility

The key insight is that wildcards represent **ranges** of versions:

| Agent Pattern | Job Pattern | Compatible? | Why |
|---------------|-------------|-------------|-----|
| `7.x` | `7.1.2` | Yes | Agent can run any v7, including 7.1.2 |
| `7.x` | `6.2.6` | No | Agent can only run v7, not v6 |
| `7.1.x` | `7.1.2` | Yes | Agent can run any 7.1.x, including 7.1.2 |
| `7.1.x` | `7.2.0` | No | Agent can only run 7.1.x, not 7.2.x |
| `6.x` | `7.x` | No | Major version mismatch |
| `7.x` | `7.x` | Yes | Same major wildcard |

### Exact Version Compatibility

| Agent Pattern | Job Pattern | Compatible? | Why |
|---------------|-------------|-------------|-----|
| `7.1.2` | `7.x` | Yes | Agent's 7.1.2 satisfies job's v7 requirement |
| `7.1.2` | `7.1.2-NTLMv3` | Yes | Agent pattern without suffix matches any suffix |
| `7.1.2-NTLMv3` | `7.1.2` | Yes | Agent's specific suffix satisfies job's any-suffix requirement |
| `7.1.2-NTLMv3` | `7.1.2-other` | No | Exact suffix must match |

### Compatibility Matrix

```
Agent ↓ / Job →    default   7.x     7.1.x   7.1.2   7.1.2-NTLMv3
─────────────────────────────────────────────────────────────────
default            ✓         ✓       ✓       ✓       ✓
7.x                ✓         ✓       ✓       ✓       ✓
6.x                ✓         ✗       ✗       ✗       ✗
7.1.x              ✓         ✓       ✓       ✓       ✓
7.2.x              ✓         ✓       ✗       ✗       ✗
7.1.2              ✓         ✓       ✓       ✓       ✓
7.1.2-NTLMv3       ✓         ✓       ✓       ✓       ✓
7.1.2-other        ✓         ✓       ✓       ✓       ✗
6.2.6              ✓         ✗       ✗       ✗       ✗
```

## Usage in Jobs

When creating a job (via UI or API), you specify a binary version pattern:

```json
{
  "name": "Crack NTLM hashes",
  "attack_mode": 0,
  "wordlist_ids": ["4"],
  "binary_version": "7.x",
  "priority": 5,
  "max_agents": 3
}
```

### Choosing Job Patterns

- **`default`**: Job can run on any agent, uses whatever binary is available
- **`7.x`**: Require hashcat v7 features (e.g., new hash modes)
- **`6.x`**: Need hashcat v6 for driver compatibility
- **`7.1.2-NTLMv3`**: Require a specific custom build

## Usage in Agents

Agents have a binary version pattern that determines which jobs they can run. This can be set via:

1. **Admin UI**: Agent Management → Edit Agent → Binary Version
2. **API**: `PUT /api/admin/agents/{id}/settings` with `binaryVersion` field

```json
{
  "isEnabled": true,
  "binaryVersion": "7.x"
}
```

### Agent Pattern Strategy

| Pattern | Use Case |
|---------|----------|
| `default` | Agent can run any job, downloads whatever binary is needed |
| `7.x` | Agent is configured for v7 only (e.g., newer GPU drivers) |
| `6.x` | Agent needs v6 for compatibility (e.g., older drivers) |
| `7.1.2-NTLMv3` | Agent has a specific custom build installed |

## Scheduling Integration

The job scheduler uses binary version compatibility to make intelligent assignment decisions.

### Compatibility Matrix

At the start of each scheduling cycle, the system builds a compatibility matrix:

1. For each (agent, job) pair, check if `IsCompatible(agentPattern, jobPattern)` is true
2. Track which agents are compatible with which jobs
3. Calculate **constraint scores** (fewer compatible agents = more constrained job)
4. Calculate **flexibility scores** (fewer compatible jobs = more specialized agent)

### Constrained-First Assignment

Within each priority level, the scheduler:

1. Sorts jobs by constraint score (most constrained first)
2. Assigns agents sorted by flexibility score (specialists first)
3. This ensures constrained jobs get their specialist agents before flexible jobs take them

**Example:**
```
3 agents, 3 jobs (same priority)
- Agent A: "default" (flexible - compatible with all jobs)
- Agent B: "7.x" (medium - compatible with v7 jobs)
- Agent C: "7.1.2-NTLMv3" (specialist - only specific jobs)

- Job 1: "default" (unconstrained - any agent works)
- Job 2: "7.x" (medium constraint - needs v7 agents)
- Job 3: "7.1.2-NTLMv3" (highly constrained - only Agent C)

Assignment order:
1. Job 3 (most constrained) → Agent C (only compatible agent)
2. Job 2 → Agent B (specialist for v7)
3. Job 1 → Agent A (remaining flexible agent)
```

### FIFO Preservation

The compatibility system respects FIFO ordering:

- Jobs don't "jump the queue" based on compatibility
- A constrained job 4th in line waits until it becomes eligible
- Incompatible agents skip constrained jobs and flow to compatible jobs at the same or lower priority

### Overflow Handling

When extra agents are available beyond `max_agents`:

- **FIFO mode**: Extra agents go to the oldest job with compatible agents
- **Round-robin mode**: Extra agents distributed across jobs with compatible agents
- Incompatible agents automatically skip to the next compatible job

## Pattern Resolution

When an agent needs to download a binary, the pattern must resolve to an actual binary ID.

### Resolution Priority

1. **Exact match**: If pattern is exact (e.g., "7.1.2"), find that version
2. **Suffix match**: If pattern has suffix (e.g., "7.1.2-NTLMv3"), find exact match
3. **Wildcard resolution**: For wildcards, find the newest matching version
   - "7.x" → newest v7.x.x binary
   - "7.1.x" → newest v7.1.x binary

### Resolution API

```http
GET /api/binary/patterns
Authorization: Bearer <token>
```

Returns available patterns with their resolved binary IDs:

```json
{
  "patterns": [
    {"pattern": "default", "resolved_id": 5, "resolved_version": "7.1.2"},
    {"pattern": "7.x", "resolved_id": 5, "resolved_version": "7.1.2"},
    {"pattern": "6.x", "resolved_id": 3, "resolved_version": "6.2.6"},
    {"pattern": "7.1.2-NTLMv3", "resolved_id": 7, "resolved_version": "7.1.2-NTLMv3"}
  ]
}
```

## Benchmark Integration

The benchmark system also respects binary version compatibility:

- Benchmarks are only requested from compatible agents
- An agent with "7.x" pattern won't benchmark hash types for "6.x" jobs
- This prevents wasted benchmark time on incompatible combinations

See [Benchmark Workflow](benchmark-workflow.md) for details on the benchmark-first assignment system.

## Migration from Legacy System

Prior to version 1.5, KrakenHashes used integer binary IDs:

**Old schema:**
```sql
binary_version_id INTEGER  -- Foreign key to binary_versions.id
binary_override BOOLEAN    -- Whether to use agent-specific binary
```

**New schema:**
```sql
binary_version VARCHAR(50) -- Pattern string like "default", "7.x", "7.1.2"
```

### Migration Behavior

The database migration (`00110_convert_binary_version_to_patterns`) automatically converts:

1. **Agents without override**: Set to `"default"` (can run any job)
2. **Agents with override**: Converted based on their binary version string
3. **Jobs with specific binary**: Converted to exact version pattern
4. **Jobs without binary**: Set to `"default"` (accepts any agent)

## Best Practices

### For Administrators

1. **Start with "default"**: New agents should use "default" unless they have specific constraints
2. **Use wildcards for compatibility**: "7.x" is usually better than "7.1.2" for flexibility
3. **Reserve exact patterns for custom builds**: Only use suffix patterns for special binaries
4. **Monitor incompatible jobs**: Jobs staying pending may have no compatible agents

### For Job Creation

1. **Use "default" when possible**: Maximizes agent compatibility
2. **Specify major version for new features**: Use "7.x" if you need v7-specific hash modes
3. **Be specific for custom builds**: Use exact suffix pattern for custom-compiled binaries

### For Mixed Environments

If you have agents with different hashcat versions:

```
Fleet of 10 agents:
- 5 agents with "7.x" (newer GPUs, updated drivers)
- 5 agents with "6.x" (older hardware, legacy drivers)

Job strategy:
- Routine jobs: "default" (uses all 10 agents)
- New hash modes: "7.x" (uses 5 v7 agents)
- Compatibility-sensitive: "6.x" (uses 5 v6 agents)
```

## Troubleshooting

### Job Stuck Pending (No Compatible Agents)

**Symptom**: Job stays in "pending" status indefinitely

**Cause**: No online agents have a compatible binary version pattern

**Solution**:
1. Check job's `binary_version` requirement
2. Check online agents' `binary_version` patterns
3. Either update agent patterns or change job requirement

**Query to diagnose:**
```sql
-- Find jobs with no compatible agents
SELECT je.name, je.binary_version as job_pattern,
       COUNT(CASE WHEN a.status = 'online' THEN 1 END) as online_agents,
       COUNT(CASE WHEN a.status = 'online' AND
             version_compatible(a.binary_version, je.binary_version) THEN 1 END) as compatible_agents
FROM job_executions je
CROSS JOIN agents a
WHERE je.status = 'pending'
GROUP BY je.id
HAVING COUNT(CASE WHEN a.status = 'online' AND
             version_compatible(a.binary_version, je.binary_version) THEN 1 END) = 0;
```

### Agent Not Receiving Jobs

**Symptom**: Agent is online but never gets assigned work

**Cause**: Agent's pattern may be too restrictive

**Solution**:
1. Check agent's `binary_version` pattern
2. Check pending jobs' `binary_version` requirements
3. Consider using a more permissive pattern like "default" or "7.x"

### Binary Resolution Failures

**Symptom**: "No binary found for pattern" errors

**Cause**: Pattern doesn't match any uploaded binary

**Solution**:
1. Upload a binary that matches the pattern
2. Or update the pattern to match available binaries
3. Check available patterns via `GET /api/binary/patterns`

## Related Documentation

- [Managing Binaries](../../admin-guide/resource-management/binaries.md) - Binary upload and management
- [Agent Management](../../admin-guide/operations/agents.md) - Agent configuration
- [Benchmark Workflow](benchmark-workflow.md) - How benchmarks work with version patterns
- [Job Priority](../../admin-guide/advanced/job-priority.md) - Priority-based scheduling
