#!/bin/bash
#
# KrakenHashes User API - cURL Examples
#
# This script demonstrates how to use the KrakenHashes User API with cURL.
# Set your credentials below before running.

# Configuration
BASE_URL="http://localhost:31337/api/v1"
USER_EMAIL="user@example.com"
API_KEY="your-64-character-api-key-here"

# Helper function for API requests
api_request() {
    local method="$1"
    local endpoint="$2"
    shift 2

    curl -s -X "$method" \
        -H "X-User-Email: $USER_EMAIL" \
        -H "X-API-Key: $API_KEY" \
        -H "Content-Type: application/json" \
        "$@" \
        "${BASE_URL}${endpoint}"
}

echo "KrakenHashes User API - cURL Examples"
echo "======================================"

# 1. Health Check
echo -e "\n1. Health Check"
echo "Command: curl -X GET ${BASE_URL}/health"
curl -s -X GET "${BASE_URL}/health" | jq .

# ============================================
# CLIENT MANAGEMENT
# ============================================

# 2. Create a Client
echo -e "\n2. Create a Client"
echo "Command: api_request POST /clients -d '{\"name\":\"Example Corp\",\"domain\":\"example.com\"}'"
CLIENT_RESPONSE=$(api_request POST /clients -d '{
  "name": "Example Corp",
  "description": "Example client for testing",
  "domain": "example.com",
  "data_retention_months": 12
}')
echo "$CLIENT_RESPONSE" | jq .
CLIENT_ID=$(echo "$CLIENT_RESPONSE" | jq -r .id)
echo "Created client ID: $CLIENT_ID"

# 3. List Clients
echo -e "\n3. List Clients"
echo "Command: api_request GET /clients?page=1&page_size=20"
api_request GET "/clients?page=1&page_size=20" | jq .

# 4. Get Specific Client
echo -e "\n4. Get Specific Client"
echo "Command: api_request GET /clients/$CLIENT_ID"
api_request GET "/clients/$CLIENT_ID" | jq .

# 5. Update Client
echo -e "\n5. Update Client"
echo "Command: api_request PATCH /clients/$CLIENT_ID -d '{\"description\":\"Updated description\"}'"
api_request PATCH "/clients/$CLIENT_ID" -d '{
  "description": "Updated description",
  "domain": "updated.example.com"
}' | jq .

# ============================================
# METADATA ENDPOINTS
# ============================================

# 6. List Hash Types
echo -e "\n6. List Hash Types (enabled only)"
echo "Command: api_request GET /hash-types?enabled_only=true"
api_request GET "/hash-types?enabled_only=true" | jq '.hash_types[:5]'  # Show first 5

# 7. List Workflows
echo -e "\n7. List Workflows"
echo "Command: api_request GET /workflows"
api_request GET "/workflows" | jq .

# 8. List Preset Jobs
echo -e "\n8. List Preset Jobs"
echo "Command: api_request GET /preset-jobs"
api_request GET "/preset-jobs" | jq .
PRESET_JOB_ID=$(api_request GET "/preset-jobs" | jq -r '.preset_jobs[0].id // empty')

# ============================================
# HASHLIST MANAGEMENT
# ============================================

# 9. Upload a Hashlist
echo -e "\n9. Upload a Hashlist"
echo "Note: This requires a hash file. Example command:"
cat << 'EOF'
curl -X POST \
  -H "X-User-Email: $USER_EMAIL" \
  -H "X-API-Key: $API_KEY" \
  -F "file=@/path/to/hashes.txt" \
  -F "name=Example Hashes" \
  -F "client_id=$CLIENT_ID" \
  -F "hash_type_id=1000" \
  "${BASE_URL}/hashlists"
EOF

# Uncomment to actually upload:
# HASHLIST_RESPONSE=$(curl -s -X POST \
#   -H "X-User-Email: $USER_EMAIL" \
#   -H "X-API-Key: $API_KEY" \
#   -F "file=@/path/to/hashes.txt" \
#   -F "name=Example Hashes" \
#   -F "client_id=$CLIENT_ID" \
#   -F "hash_type_id=1000" \
#   "${BASE_URL}/hashlists")
# echo "$HASHLIST_RESPONSE" | jq .
# HASHLIST_ID=$(echo "$HASHLIST_RESPONSE" | jq -r .id)

# 10. List Hashlists
echo -e "\n10. List Hashlists"
echo "Command: api_request GET /hashlists?page=1&page_size=20"
api_request GET "/hashlists?page=1&page_size=20" | jq .

# 11. Filter Hashlists by Client
echo -e "\n11. Filter Hashlists by Client"
echo "Command: api_request GET /hashlists?client_id=$CLIENT_ID"
api_request GET "/hashlists?client_id=$CLIENT_ID" | jq .

# 12. Search Hashlists by Name
echo -e "\n12. Search Hashlists by Name"
echo "Command: api_request GET '/hashlists?search=domain'"
api_request GET "/hashlists?search=domain" | jq .

# ============================================
# JOB MANAGEMENT
# ============================================

# 13. Create a Job
echo -e "\n13. Create a Job"
echo "Note: Requires an existing hashlist. Example command:"
cat << 'EOF'
curl -X POST \
  -H "X-User-Email: $USER_EMAIL" \
  -H "X-API-Key: $API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Attack Domain Hashes",
    "hashlist_id": 1,
    "preset_job_id": 1,
    "priority": 100,
    "max_agents": 5
  }' \
  "${BASE_URL}/jobs"
EOF

# Uncomment when you have a hashlist:
# JOB_RESPONSE=$(api_request POST /jobs -d "{
#   \"name\": \"Attack Domain Hashes\",
#   \"hashlist_id\": $HASHLIST_ID,
#   \"preset_job_id\": $PRESET_JOB_ID,
#   \"priority\": 100,
#   \"max_agents\": 5
# }")
# echo "$JOB_RESPONSE" | jq .
# JOB_ID=$(echo "$JOB_RESPONSE" | jq -r .id)

# 14. List Jobs
echo -e "\n14. List Jobs"
echo "Command: api_request GET /jobs?page=1&page_size=20"
api_request GET "/jobs?page=1&page_size=20" | jq .

# 15. Filter Jobs by Status
echo -e "\n15. Filter Jobs by Status (running only)"
echo "Command: api_request GET /jobs?status=running"
api_request GET "/jobs?status=running" | jq .

# 16. Get Specific Job
echo -e "\n16. Get Specific Job (if any exist)"
JOB_ID=$(api_request GET "/jobs" | jq -r '.jobs[0].id // empty')
if [ -n "$JOB_ID" ]; then
    echo "Command: api_request GET /jobs/$JOB_ID"
    api_request GET "/jobs/$JOB_ID" | jq .
else
    echo "No jobs found. Create a job first."
fi

# 17. Update Job Priority
if [ -n "$JOB_ID" ]; then
    echo -e "\n17. Update Job Priority"
    echo "Command: api_request PATCH /jobs/$JOB_ID -d '{\"priority\":500}'"
    api_request PATCH "/jobs/$JOB_ID" -d '{
      "priority": 500
    }' | jq .
fi

# 18. Get Job Layers (for increment mode jobs)
if [ -n "$JOB_ID" ]; then
    echo -e "\n18. Get Job Layers"
    echo "Command: api_request GET /jobs/$JOB_ID/layers"
    api_request GET "/jobs/$JOB_ID/layers" | jq .
fi

# 19. Get Tasks for a Job Layer
if [ -n "$JOB_ID" ]; then
    echo -e "\n19. Get Tasks for a Job Layer"
    echo "Command: api_request GET /jobs/$JOB_ID/layers/1"
    api_request GET "/jobs/$JOB_ID/layers/1" | jq .
fi

# ============================================
# AGENT MANAGEMENT
# ============================================

# 20. Generate Agent Voucher
echo -e "\n20. Generate Agent Registration Voucher"
echo "Command: api_request POST /agents/vouchers -d '{\"is_continuous\":false}'"
VOUCHER_RESPONSE=$(api_request POST /agents/vouchers -d '{
  "is_continuous": false
}')
echo "$VOUCHER_RESPONSE" | jq .
VOUCHER_CODE=$(echo "$VOUCHER_RESPONSE" | jq -r .code)
echo -e "\nTo register an agent with this voucher, run:"
echo "./agent --host your-server:31337 --claim $VOUCHER_CODE"

# 21. Generate Continuous Voucher (for multiple agents)
echo -e "\n21. Generate Continuous Voucher"
echo "Command: api_request POST /agents/vouchers -d '{\"is_continuous\":true}'"
api_request POST /agents/vouchers -d '{
  "is_continuous": true
}' | jq .

# 22. List Agents
echo -e "\n22. List Agents"
echo "Command: api_request GET /agents?page=1&page_size=20"
api_request GET "/agents?page=1&page_size=20" | jq .

# 23. Filter Agents by Status
echo -e "\n23. Filter Agents by Status (active only)"
echo "Command: api_request GET /agents?status=active"
api_request GET "/agents?status=active" | jq .

# 24. Get Specific Agent
echo -e "\n24. Get Specific Agent (if any exist)"
AGENT_ID=$(api_request GET "/agents" | jq -r '.agents[0].id // empty')
if [ -n "$AGENT_ID" ]; then
    echo "Command: api_request GET /agents/$AGENT_ID"
    api_request GET "/agents/$AGENT_ID" | jq .
else
    echo "No agents found. Register an agent first."
fi

# 25. Update Agent
if [ -n "$AGENT_ID" ]; then
    echo -e "\n25. Update Agent"
    echo "Command: api_request PATCH /agents/$AGENT_ID -d '{\"name\":\"Updated Agent Name\"}'"
    api_request PATCH "/agents/$AGENT_ID" -d '{
      "name": "Updated Agent Name"
    }' | jq .
fi

# 26. Disable Agent
if [ -n "$AGENT_ID" ]; then
    echo -e "\n26. Disable Agent (commented out)"
    echo "Command: api_request DELETE /agents/$AGENT_ID"
    # Uncomment to actually disable:
    # api_request DELETE "/agents/$AGENT_ID"
    # echo "Agent disabled"
fi

# ============================================
# CLEANUP
# ============================================

# 27. Delete Hashlist
echo -e "\n27. Delete Hashlist (commented out)"
echo "Command: api_request DELETE /hashlists/\$HASHLIST_ID"
# Uncomment when you have a hashlist to delete:
# api_request DELETE "/hashlists/$HASHLIST_ID"
# echo "Hashlist deleted (if no active jobs)"

# 28. Delete Client
echo -e "\n28. Delete Client (commented out)"
echo "Command: api_request DELETE /clients/$CLIENT_ID"
# Uncomment to actually delete:
# api_request DELETE "/clients/$CLIENT_ID"
# echo "Client deleted (if no hashlists were associated)"

echo -e "\n======================================"
echo "✓ Examples completed!"
echo ""
echo "Note: Some commands are commented out to prevent accidental data modification."
echo "Uncomment them as needed for your testing."
echo ""
echo "Key fields to note:"
echo "  - Hashlist uploads use 'hash_type_id' (not 'hash_type')"
echo "  - Vouchers use 'is_continuous' (no 'expires_in' field)"
echo "  - Job priority max is configurable (default: 1000)"
echo "  - Client 'client_id' may be required based on system settings"
