# KrakenHashes Core Concepts Guide

## Overview

KrakenHashes is a distributed password cracking management system that orchestrates multiple agents running hashcat to efficiently crack password hashes. This guide explains the fundamental concepts and terminology you need to understand to effectively use the system.

## Table of Contents

- [Key Terminology](#key-terminology)
- [System Architecture](#system-architecture)
- [Job Execution Flow](#job-execution-flow)
- [Priority System](#priority-system)
- [Chunking and Distribution](#chunking-and-distribution)
- [File Management](#file-management)
- [Result Handling](#result-handling)

## Key Terminology

### Hashlist
A **hashlist** is a collection of password hashes uploaded to the system for cracking. Each hashlist:
- Contains one or more individual hashes of the same type (MD5, NTLM, SHA1, etc.)
- Has a lifecycle: `uploading` → `processing` → `ready` (or `error`)
- Can be associated with a client for engagement tracking
- Tracks total hashes and cracked count
- May include usernames in formats like `username:hash`

### Hash
A **hash** is an individual password hash within a hashlist. Each hash:
- Contains the encrypted password value to be cracked
- May include an associated username (preserved from original line format)
- Tracks whether it has been cracked and its plaintext value
- Can appear in multiple hashlists (deduplication handled by full line, not just hash value)
- **Cross-hashlist updates**: When a hash value is cracked, ALL hashes with that same value are updated across all hashlists

### Preset Job
A **preset job** is a pre-configured attack strategy that defines:
- Which wordlists to use
- Which rule files to apply
- The hashcat attack mode (dictionary, brute-force, hybrid, association, etc.)
- Priority level (0-1000, higher = more important)
- Chunk duration (how long each task should run)
- Maximum number of agents allowed to work on it
- The specific hashcat binary version to use

### Job Workflow
A **job workflow** is a named sequence of preset jobs executed in order. Workflows enable:
- Systematic attack progression (e.g., common passwords → rules → brute force)
- Reusable attack strategies across different hashlists
- Automated execution of multiple attack phases

### Job Execution
A **job execution** is an actual running instance of a preset job against a specific hashlist. It:
- Tracks the overall status: `pending`, `running`, `completed`, `failed`, `cancelled`, `interrupted`
- Manages keyspace progress (how much of the search space has been processed)
- Supports dynamic rule splitting for large rule files
- Can be interrupted by higher priority jobs and automatically resumed
- Tracks which user created it and when

#### Job Interruption Behavior
When a job is interrupted by a higher priority job:
- Status changes from `running` to `pending` (not `paused`)
- All progress is preserved and saved
- Job automatically returns to the queue with the same priority
- When agents become available, the job resumes from where it stopped
- No manual intervention required - the system handles everything automatically

### Job Task (Chunk)
A **job task** or **chunk** is an individual unit of work assigned to an agent. Tasks are:
- Time-based chunks (e.g., 10-minute segments of work)
- Defined by keyspace ranges (start/end positions in the search space)
- Self-contained with the complete hashcat command
- Tracked for progress and can be retried on failure
- For rule-based attacks, may represent a subset of rules

### Agent
An **agent** is a compute node that executes hashcat commands. Agents:
- Run on systems with GPUs or CPUs capable of password cracking
- Connect via WebSocket for real-time communication
- Report hardware capabilities (GPU types, benchmark speeds)
- Can be scheduled for specific time windows
- Are authenticated via API keys and claim codes
- Can be owned by specific users or shared within teams

### Pot File
A **pot file** (short for "potfile") is hashcat's database of cracked hashes. In KrakenHashes:
- Each successful crack is immediately synchronized to the backend
- Results are shared across all agents and jobs
- The system maintains a centralized view of all cracked hashes
- Previously cracked hashes are automatically filtered from new jobs

## System Architecture

```
┌─────────────────┐     ┌─────────────────┐     ┌─────────────────┐
│   Web Frontend  │     │  Backend API    │     │   PostgreSQL    │
│   (React/TS)    │────▶│   (Go REST)     │────▶│   Database      │
└─────────────────┘     └────────┬────────┘     └─────────────────┘
                                 │
                    WebSocket    │
                    Connection   │
                                 ▼
                    ┌────────────────────┐
                    │  Distributed Agents │
                    │    (Go + Hashcat)   │
                    └────────────────────┘
```

### Component Responsibilities

1. **Frontend**: User interface for uploading hashlists, creating jobs, monitoring progress
2. **Backend**: Orchestrates job scheduling, manages data, handles agent communication
3. **Database**: Stores all persistent data (users, hashlists, jobs, results)
4. **Agents**: Execute hashcat commands and report results back to the backend

## Job Execution Flow

### 1. Job Creation Phase
```
User uploads hashlist → System processes hashes → Hashlist marked as "ready"
                                    ↓
User creates job execution → Selects preset job → Sets priority
                                    ↓
System calculates keyspace → Creates job execution record
```

### 2. Task Generation Phase
```
Job execution created → System analyzes attack parameters
                               ↓
        Calculate total keyspace (wordlist × rules)
                               ↓
    Generate time-based chunks using agent benchmarks
                               ↓
          Create job tasks in "pending" status
```

### 3. Assignment Phase
```
Agent requests work → Scheduler checks available jobs (priority order)
                                    ↓
              Find highest priority job with pending tasks
                                    ↓
          Check agent capabilities and assign compatible task
                                    ↓
               Agent downloads required files if needed
```

### 4. Execution Phase
```
Agent receives task → Builds hashcat command → Starts execution
                                ↓
                    Reports progress every 5 seconds
                                ↓
            Backend updates task and job execution progress
                                ↓
                  Cracked hashes synchronized in real-time
```

### 5. Completion Phase
```
Task completes → Agent reports final status → Backend updates records
                                ↓
                Check if more tasks remain for job
                                ↓
    If no tasks remain → Mark job execution as completed
                                ↓
            Update hashlist cracked count
```

## Priority System

KrakenHashes uses a priority scale from **0 to 1000**, where:
- **1000** = Highest priority (urgent/critical jobs)
- **500** = Normal priority (default for most jobs)
- **0** = Lowest priority (background/research jobs)

### Priority Behavior

1. **Job Selection**: When agents request work, jobs are assigned in priority order
2. **FIFO Within Priority**: Jobs with the same priority follow First-In-First-Out
3. **Job Interruption**: Higher priority jobs can interrupt lower priority running jobs
4. **Priority-Based Agent Allocation**: How many agents your job receives depends on priority
5. **Automatic Resumption**: Interrupted jobs automatically resume when resources are available

### Priority-Based Agent Allocation

The priority system uses intelligent allocation to ensure critical jobs get the resources they need:

**Higher Priority Jobs Override max_agents:**
- When your job has **higher priority** than all other jobs, it receives **ALL available agents**
- The max_agents setting is **ignored** for highest priority jobs
- Ensures critical work completes as fast as possible

**Same Priority Jobs Respect max_agents:**
- When multiple jobs share the **same priority**, each respects its **max_agents limit**
- Overflow agents (beyond max_agents) are distributed based on **overflow allocation mode**:
  - **FIFO mode** (default): Oldest job gets all overflow agents
  - **Round-robin mode**: Overflow agents distributed evenly across all jobs

**Lower Priority Jobs Wait:**
- Jobs with lower priority wait for higher priority jobs to complete or release agents
- Automatically start when resources become available

**Examples:**

*Scenario 1: Different Priorities*
```
Job A: Priority 100, max_agents = 3
Job B: Priority 50,  max_agents = 5
15 agents available

Result:
- Job A: 15 agents (highest priority, gets ALL agents)
- Job B: 0 agents (lower priority, waits)
```

*Scenario 2: Same Priority with FIFO Mode*
```
Job A: Priority 50, max_agents = 2 (created first)
Job B: Priority 50, max_agents = 2 (created second)
Job C: Priority 50, max_agents = 2 (created third)
15 agents available

Result:
- Job A: 11 agents (2 max + 9 overflow, oldest job)
- Job B: 2 agents (max_agents only)
- Job C: 2 agents (max_agents only)
```

*Scenario 3: Same Priority with Round-Robin Mode*
```
Job A: Priority 50, max_agents = 2
Job B: Priority 50, max_agents = 2
Job C: Priority 50, max_agents = 2
15 agents available

Result:
- Job A: 5 agents (2 max + 3 overflow)
- Job B: 5 agents (2 max + 3 overflow)
- Job C: 5 agents (2 max + 3 overflow)
```

**Key Takeaways:**
- Set higher priority for time-critical jobs to get all available resources
- Use max_agents to control resource usage for jobs at the same priority
- Overflow allocation mode (FIFO vs round-robin) affects same-priority job distribution
- Administrators configure overflow mode via system settings

### Priority Guidelines

- **900-1000**: Time-critical engagements, incident response
- **600-899**: Active client engagements with deadlines
- **400-599**: Standard testing and assessments
- **100-399**: Research and development tasks
- **0-99**: Background processing, long-running attacks

## Chunking and Distribution

### Time-Based Chunking

KrakenHashes uses time-based chunks rather than fixed keyspace divisions:

```
Total Keyspace: 1,000,000,000 candidates
Agent Benchmark: 10,000,000 hashes/second
Chunk Duration: 600 seconds (10 minutes)

Chunk Size = Benchmark × Duration = 6,000,000,000 candidates
Number of Chunks = Total Keyspace ÷ Chunk Size = 167 chunks
```

### Benefits of Time-Based Chunks

1. **Predictable Duration**: Each chunk runs for approximately the same time
2. **Fair Distribution**: Fast and slow agents get appropriately sized work
3. **Better Scheduling**: Easier to estimate completion times
4. **Checkpoint Recovery**: Regular checkpoints minimize lost work

### Rule Splitting

For attacks using large rule files:

```
Wordlist: 10,000 words
Rules: 100,000 rules
Effective Keyspace: 1,000,000,000 (words × rules)

Instead of processing all rules at once:
- Split into chunks of 1,000 rules each
- Create 100 separate tasks
- Each task processes: 10,000 words × 1,000 rules
```

## File Management

### Storage Hierarchy

```
/data/krakenhashes/
├── binaries/          # Hashcat executables (multiple versions)
├── wordlists/         # Dictionary files
│   ├── general/       # Common password lists
│   ├── specialized/   # Domain-specific lists
│   └── custom/        # User-uploaded lists
├── rules/             # Rule files for mutations
│   ├── hashcat/       # Hashcat-format rules
│   └── custom/        # User-created rules
├── hashlists/         # Uploaded hash files
│   └── {hashlist_id}/ # Organized by hashlist ID
└── temp/              # Temporary files (rule chunks, etc.)
```

### File Synchronization

1. **Lazy Sync**: Agents download files only when needed
2. **Hash Verification**: MD5 checksums ensure file integrity
3. **Local Caching**: Agents cache files to avoid re-downloading
4. **Automatic Cleanup**: Temporary files removed after job completion

## Result Handling

### Real-Time Crack Synchronization

```
Agent cracks hash → Sends result to backend → Backend updates database
                            ↓
        Broadcast to other agents working on same hashlist
                            ↓
            Update hashlist statistics
                            ↓
        Notify connected web clients
```

### Result Storage

Each cracked hash stores:
- Original hash value
- Plaintext password
- Username (if available)
- Crack timestamp
- Which job/task found it
- Position in keyspace where found

### Pot File Management

- **Centralized Pot**: Backend maintains master record of all cracks
- **Agent Sync**: Agents receive relevant cracks for their current hashlist
- **Deduplication by Line**: Each unique input line preserved; duplicates by hash value automatically updated when cracked
- **Cross-Hashlist**: Cracks are automatically applied to all hashlists containing that hash value
- **Username Preservation**: Multiple users with same password (e.g., "Administrator", "Administrator1") tracked separately

### Result Access

Users can access results through:
1. **Hashlist View**: See all cracked hashes for a specific hashlist
2. **Pot File Export**: Download results in hashcat pot format
3. **Client Reports**: Generate reports filtered by client/engagement
4. **Real-Time Updates**: Live view of cracks as they happen

## Best Practices

### Job Design
1. Start with fast attacks (common passwords, small rules)
2. Progress to more intensive attacks (large wordlists, complex rules)
3. Use workflows to automate multi-stage attacks
4. Set appropriate priorities based on urgency

### Performance Optimization
1. Use appropriate chunk durations (5-15 minutes typically)
2. Limit max agents for jobs that don't scale well
3. Schedule large jobs during off-peak hours
4. Monitor agent efficiency and adjust benchmarks

### Resource Management
1. Organize wordlists by effectiveness
2. Test and optimize custom rules
3. Regular cleanup of old hashlists (retention policies)
4. Monitor storage usage

## Conclusion

Understanding these core concepts enables you to effectively use KrakenHashes for distributed password cracking. The system handles the complexity of distributing work, managing results, and coordinating agents, allowing you to focus on designing effective attack strategies and analyzing results.

For more detailed information on specific features, refer to the appropriate sections of the user guide.