#!/bin/bash

# KrakenHashes Job Troubleshooting Script
# Usage: ./troubleshoot-job.sh <JOB_ID>
#
# This script collects diagnostic information for troubleshooting job issues.
# Output can be shared in GitHub issues to help with debugging.

set -e

# ============================================
# CONFIGURATION - Modify these if needed
# ============================================
JOB_ID="${1:-}"
LOGS_DIR="${LOGS_DIR:-./logs/krakenhashes/backend}"
DB_CONTAINER="${DB_CONTAINER:-krakenhashes-postgres}"
DB_USER="${DB_USER:-krakenhashes}"
DB_NAME="${DB_NAME:-krakenhashes}"

# ============================================
# Validation
# ============================================
if [ -z "$JOB_ID" ]; then
    echo "Usage: $0 <JOB_ID>"
    echo "Example: $0 79a242fd-cc7a-4e93-a90e-3352077a3bdd"
    exit 1
fi

echo "========================================"
echo "KrakenHashes Troubleshooting Report"
echo "Job ID: $JOB_ID"
echo "Generated: $(date -u '+%Y-%m-%d %H:%M:%S UTC')"
echo "========================================"

# ============================================
# Database Queries
# ============================================
echo ""
echo "=== JOB EXECUTION ==="
docker exec "$DB_CONTAINER" psql -U "$DB_USER" -d "$DB_NAME" -c "
SELECT
    id,
    status,
    priority,
    attack_mode,
    hash_type,
    total_keyspace,
    effective_keyspace,
    base_keyspace,
    dispatched_keyspace,
    processed_keyspace,
    overall_progress_percent,
    max_agents,
    binary_version_id,
    uses_rule_splitting,
    rule_split_count,
    multiplication_factor,
    increment_mode,
    increment_min,
    increment_max,
    consecutive_failures,
    is_accurate_keyspace,
    chunk_size_seconds,
    error_message,
    created_at,
    started_at,
    completed_at,
    last_progress_update,
    updated_at
FROM job_executions
WHERE id = '$JOB_ID';
"

echo ""
echo "=== JOB TASKS ==="
docker exec "$DB_CONTAINER" psql -U "$DB_USER" -d "$DB_NAME" -c "
SELECT
    id,
    agent_id,
    status,
    detailed_status,
    priority,
    chunk_number,
    keyspace_start,
    keyspace_end,
    keyspace_processed,
    effective_keyspace_start,
    effective_keyspace_end,
    effective_keyspace_processed,
    chunk_actual_keyspace,
    is_actual_keyspace,
    is_keyspace_split,
    is_rule_split_task,
    rule_start_index,
    rule_end_index,
    benchmark_speed,
    average_speed,
    progress_percent,
    crack_count,
    expected_crack_count,
    received_crack_count,
    batches_complete_signaled,
    potfile_entries_added,
    retry_count,
    error_message,
    assigned_at,
    started_at,
    completed_at,
    last_checkpoint,
    created_at,
    updated_at
FROM job_tasks
WHERE job_execution_id = '$JOB_ID'
ORDER BY chunk_number, created_at;
"

echo ""
echo "=== AGENTS INVOLVED ==="
docker exec "$DB_CONTAINER" psql -U "$DB_USER" -d "$DB_NAME" -c "
SELECT DISTINCT
    a.id,
    a.name,
    a.status,
    a.last_heartbeat,
    a.version,
    a.is_enabled,
    a.scheduling_enabled,
    a.consecutive_failures,
    a.binary_version_id,
    a.binary_override,
    a.sync_status,
    a.sync_error,
    a.device_detection_status,
    a.device_detection_error,
    a.last_error,
    a.metadata
FROM agents a
INNER JOIN job_tasks jt ON jt.agent_id = a.id
WHERE jt.job_execution_id = '$JOB_ID';
"

echo ""
echo "=== ALL AGENT STATUSES ==="
docker exec "$DB_CONTAINER" psql -U "$DB_USER" -d "$DB_NAME" -c "
SELECT
    id,
    name,
    status,
    last_heartbeat,
    is_enabled,
    consecutive_failures,
    sync_status,
    last_error,
    metadata->>'busy_status' as busy_status,
    metadata->>'current_task_id' as current_task_id
FROM agents
ORDER BY last_heartbeat DESC;
"

echo ""
echo "=== INCREMENT LAYERS (if applicable) ==="
docker exec "$DB_CONTAINER" psql -U "$DB_USER" -d "$DB_NAME" -c "
SELECT * FROM job_increment_layers
WHERE job_execution_id = '$JOB_ID'
ORDER BY mask_length;
" 2>/dev/null || echo "No increment layers found or table doesn't exist"

# ============================================
# Sensitive Data Filtering
# ============================================

# Function to redact sensitive data from log output
redact_sensitive() {
    sed -E \
        -e 's/token=eyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+/token=[REDACTED_JWT]/g' \
        -e 's/Cookie:\[token=[^\]]+\]/Cookie:[token=REDACTED_JWT]/g' \
        -e 's|/home/[^/]+/|/home/[USER]/|g' \
        -e 's/"user_id":"[a-f0-9-]+"/"user_id":"[REDACTED]"/g'
}

# ============================================
# Log Search
# ============================================
echo ""
echo "=== BACKEND LOGS ==="
echo "Searching for Job ID in logs..."
echo "(Note: Sensitive data like JWT tokens and home paths are automatically redacted)"

# Function to search logs
search_logs() {
    local pattern="$1"
    local logs_dir="$2"

    # Search uncompressed logs
    for log in "$logs_dir"/backend.log*; do
        if [ -f "$log" ] && [[ ! "$log" =~ \.gz$ ]]; then
            if grep -l "$pattern" "$log" >/dev/null 2>&1; then
                echo "--- Found in: $log ---"
                grep -n "$pattern" "$log" | tail -100 | redact_sensitive
            fi
        fi
    done

    # Search compressed logs
    for log in "$logs_dir"/backend.log*.gz; do
        if [ -f "$log" ]; then
            if zgrep -l "$pattern" "$log" >/dev/null 2>&1; then
                echo "--- Found in: $log ---"
                zgrep -n "$pattern" "$log" | tail -100 | redact_sensitive
            fi
        fi
    done
}

search_logs "$JOB_ID" "$LOGS_DIR"

# Also search for task IDs
echo ""
echo "=== TASK-SPECIFIC LOGS ==="
TASK_IDS=$(docker exec "$DB_CONTAINER" psql -U "$DB_USER" -d "$DB_NAME" -t -c "
SELECT id FROM job_tasks WHERE job_execution_id = '$JOB_ID';
" 2>/dev/null | tr -d ' ')

for TASK_ID in $TASK_IDS; do
    if [ -n "$TASK_ID" ]; then
        echo ""
        echo "--- Task: $TASK_ID ---"
        search_logs "$TASK_ID" "$LOGS_DIR" 2>/dev/null | head -50 | redact_sensitive
    fi
done

echo ""
echo "========================================"
echo "End of Troubleshooting Report"
echo "========================================"
