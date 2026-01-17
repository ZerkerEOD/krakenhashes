# Rule Splitting Implementation Summary

## Current Status

The rule splitting feature has been fully implemented but requires testing with a fresh job. The implementation includes:

### Backend Components

1. **Database Schema** (✓ Complete)
   - Added columns to `job_executions`: `uses_rule_splitting`, `rule_split_count`, `base_keyspace`, `effective_keyspace`, `multiplication_factor`
   - Added columns to `job_tasks`: `is_rule_split_task`, `rule_chunk_path`, `rule_start_index`, `rule_end_index`

2. **Rule Split Manager** (✓ Complete)
   - `RuleSplitManager` service that handles splitting rule files into chunks
   - Creates temporary chunk files in `/data/krakenhashes/temp/rule_chunks/`
   - Supports counting rules and creating evenly distributed chunks

3. **Job Execution Service** (✓ Complete)
   - `calculateEffectiveKeyspace` - Calculates virtual keyspace for rules/combination attacks
   - `determineRuleSplitting` - Decides if a job should use rule splitting based on thresholds
   - `InitializeRuleSplitting` - Creates rule chunk tasks when job starts
   - `GetNextRuleSplitTask` - Assigns rule chunks to agents

4. **Job Scheduling Service** (✓ Complete)
   - Enhanced to detect rule-split jobs and initialize splitting on first assignment
   - Routes rule-split jobs through special task assignment logic
   - Syncs rule chunk files to agents

5. **WebSocket Integration** (✓ Complete)
   - Sends rule chunk paths to agents as `rules/chunks/<filename>`
   - Properly handles file sync for rule chunks

### Frontend Components

1. **Keyspace Display** (✓ Complete)
   - Shows effective keyspace with multiplication factor badge
   - Tooltips explain virtual keyspace calculation
   - Proper handling of snake_case field names from backend

2. **Admin Settings** (✓ Complete)
   - Rule splitting configuration in Job Execution settings
   - Controls for threshold, min rules, max chunks

## Known Issues

1. **Existing Jobs**: Jobs created before the implementation don't have:
   - Effective keyspace calculated
   - Rule splitting decision made
   - This causes them to run with full rule files

2. **Rule File Path Resolution**: The `calculateEffectiveKeyspace` method may fail to count rules if the file path resolution doesn't match the actual file location.

## How Rule Splitting Works

### Decision Timing (v1.3.1+)

**Important Change:** Rule splitting is now determined at **job creation time**, not after the first benchmark completes. This prevents:
- Race conditions when multiple agents connect simultaneously
- Mid-job strategy changes after tasks are already dispatched
- Inconsistent rule splitting decisions across agents

### Decision Logic

1. When a job is created with attack mode 0 (straight) and rules:
   - Retrieve **actual rule count** from rule files
   - Compare against `rule_split_min_rules` threshold
   - If rule count >= min_rules AND rule splitting is enabled:
     - Mark job with `uses_rule_splitting = true`
     - Calculate optimal number of chunks based on estimated keyspace

**Note:** The decision uses the **actual rule count**, not the salt-adjusted keyspace. This prevents false positives where salt multiplication incorrectly triggers rule splitting.

2. When first agent picks up the job:
   - `InitializeRuleSplitting` is called
   - Rule file is split into N chunks
   - N tasks are created, each with a chunk file path

3. Agents receive tasks with:
   - Full wordlist keyspace (no skip/limit)
   - Rule chunk file instead of full rule file
   - Progress tracked per chunk

4. Progress aggregation accounts for:
   - Each chunk processes full wordlist with subset of rules
   - Total progress = sum of (chunk_progress × rules_in_chunk)

## Testing the Implementation

To test rule splitting with a new job:

1. Create a preset job with:
   - Attack mode 0 (straight)
   - A wordlist (e.g., crackstation.txt with 1.2B words)
   - A large rule file (e.g., _nsakey.v2.dive.rule with 123K rules)

2. Create a job execution from this preset
   - The job should be marked with `uses_rule_splitting = true`
   - `rule_split_count` should be calculated (e.g., 415 chunks)

3. When agent picks up the job:
   - Check logs for "Initializing rule splitting"
   - Verify chunk files created in temp directory
   - Agent should receive `rules/chunks/job_*_chunk_*.rule` path

## Configuration

Current settings (in `system_settings` table):
- `rule_split_enabled`: true
- `rule_split_threshold`: 2.0 (split if job takes > 2x chunk duration)
- `rule_split_min_rules`: 100 (only split if > 100 rules)
- `rule_split_max_chunks`: 1000 (maximum chunks to create)
- `rule_chunk_temp_dir`: /data/krakenhashes/temp/rule_chunks

## Agent Expectations

Agents expect rule chunks to be available at:
- `<agent_data_dir>/rules/chunks/<chunk_filename>`

The backend WebSocket integration correctly formats the path as `rules/chunks/<filename>` when sending task assignments for rule-split jobs.