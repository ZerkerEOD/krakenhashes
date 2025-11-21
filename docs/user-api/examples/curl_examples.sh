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

# 2. Create a Client
echo -e "\n2. Create a Client"
echo "Command: api_request POST /clients -d '{\"name\":\"Example Corp\"}'"
CLIENT_RESPONSE=$(api_request POST /clients -d '{
  "name": "Example Corp",
  "description": "Example client for testing"
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
  "description": "Updated description"
}' | jq .

# 6. List Hash Types
echo -e "\n6. List Hash Types (enabled only)"
echo "Command: api_request GET /hash-types?enabled_only=true"
api_request GET "/hash-types?enabled_only=true" | jq '.hash_types[:5]'  # Show first 5

# 7. Upload a Hashlist
echo -e "\n7. Upload a Hashlist"
echo "Note: This requires a hash file. Example command:"
cat << 'EOF'
curl -X POST \
  -H "X-User-Email: $USER_EMAIL" \
  -H "X-API-Key: $API_KEY" \
  -F "file=@/path/to/hashes.txt" \
  -F "name=Example Hashes" \
  -F "client_id=$CLIENT_ID" \
  -F "hash_type=1000" \
  "${BASE_URL}/hashlists"
EOF

# Uncomment to actually upload:
# HASHLIST_RESPONSE=$(curl -s -X POST \
#   -H "X-User-Email: $USER_EMAIL" \
#   -H "X-API-Key: $API_KEY" \
#   -F "file=@/path/to/hashes.txt" \
#   -F "name=Example Hashes" \
#   -F "client_id=$CLIENT_ID" \
#   -F "hash_type=1000" \
#   "${BASE_URL}/hashlists")
# echo "$HASHLIST_RESPONSE" | jq .
# HASHLIST_ID=$(echo "$HASHLIST_RESPONSE" | jq -r .id)

# 8. List Hashlists
echo -e "\n8. List Hashlists"
echo "Command: api_request GET /hashlists?page=1&page_size=20"
api_request GET "/hashlists?page=1&page_size=20" | jq .

# 9. Filter Hashlists by Client
echo -e "\n9. Filter Hashlists by Client"
echo "Command: api_request GET /hashlists?client_id=$CLIENT_ID"
api_request GET "/hashlists?client_id=$CLIENT_ID" | jq .

# 10. Generate Agent Voucher
echo -e "\n10. Generate Agent Registration Voucher"
echo "Command: api_request POST /agents/vouchers -d '{\"expires_in\":604800,\"is_continuous\":false}'"
VOUCHER_RESPONSE=$(api_request POST /agents/vouchers -d '{
  "expires_in": 604800,
  "is_continuous": false
}')
echo "$VOUCHER_RESPONSE" | jq .
VOUCHER_CODE=$(echo "$VOUCHER_RESPONSE" | jq -r .code)
echo -e "\nTo register an agent with this voucher, run:"
echo "./agent --host your-server:31337 --claim $VOUCHER_CODE"

# 11. List Agents
echo -e "\n11. List Agents"
echo "Command: api_request GET /agents?page=1&page_size=20"
api_request GET "/agents?page=1&page_size=20" | jq .

# 12. Filter Agents by Status
echo -e "\n12. Filter Agents by Status (active only)"
echo "Command: api_request GET /agents?status=active"
api_request GET "/agents?status=active" | jq .

# 13. Get Specific Agent
echo -e "\n13. Get Specific Agent (if any exist)"
AGENT_ID=$(api_request GET "/agents" | jq -r '.agents[0].id // empty')
if [ -n "$AGENT_ID" ]; then
    echo "Command: api_request GET /agents/$AGENT_ID"
    api_request GET "/agents/$AGENT_ID" | jq .
else
    echo "No agents found. Register an agent first."
fi

# 14. Update Agent
if [ -n "$AGENT_ID" ]; then
    echo -e "\n14. Update Agent"
    echo "Command: api_request PATCH /agents/$AGENT_ID -d '{\"name\":\"Updated Agent Name\"}'"
    api_request PATCH "/agents/$AGENT_ID" -d '{
      "name": "Updated Agent Name"
    }' | jq .
fi

# 15. List Workflows
echo -e "\n15. List Workflows"
echo "Command: api_request GET /workflows"
api_request GET "/workflows" | jq .

# 16. List Preset Jobs
echo -e "\n16. List Preset Jobs"
echo "Command: api_request GET /preset-jobs"
api_request GET "/preset-jobs" | jq .

# 17. Cleanup - Delete Client
echo -e "\n17. Cleanup - Delete Client"
echo "Command: api_request DELETE /clients/$CLIENT_ID"
# Uncomment to actually delete:
# api_request DELETE "/clients/$CLIENT_ID"
# echo "Client deleted (if no hashlists were associated)"

echo -e "\n======================================"
echo "✓ Examples completed!"
echo ""
echo "Note: Some commands are commented out to prevent accidental data modification."
echo "Uncomment them as needed for your testing."
