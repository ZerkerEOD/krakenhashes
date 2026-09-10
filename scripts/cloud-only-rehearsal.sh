#!/usr/bin/env bash
#
# Cloud-only rehearsal: configure a $0 mock-provider deployment with zero
# on-prem GPUs, following the operator checklist in
# docs/admin-guide/system-setup/cloud-providers.md.
#
# This is the dry run of the instructions the RunPod/Vast testers will follow.
# It spends nothing: the mock provider launches a local agent process in
# --test-mode instead of renting hardware.
#
# PREREQUISITES (both need a build, which this script does NOT do):
#   1. Backend built from a commit containing the cloud-only work. Verify with:
#        docker exec krakenhashes-app sh -c \
#          'strings /usr/local/bin/krakenhashes | grep -c cloud_default_client_id'
#      A 0 means the image is stale and everything below will be inert.
#   2. An agent binary INSIDE the backend container. MockProvider.Launch execs
#      it directly (mockprovider.go:129), so a host path is not enough:
#        cd agent && make clean && make linux-amd64
#        docker cp ../bin/agent/krakenhashes-agent-linux-amd64 \
#          krakenhashes-app:/usr/local/bin/krakenhashes-agent
#      Without it, Preflight reports "agent binary not found" and no instance
#      ever registers — which still exercises the refusal paths, but not the
#      chunk floor or the drain rung.
#
# Safe to re-run. Creates a dedicated client and provider config; touches no
# existing rows beyond the cloud_* system settings it must set.

set -euo pipefail

PSQL() { docker exec -i krakenhashes-postgres psql -U krakenhashes -d krakenhashes "$@"; }

AGENT_BIN="${AGENT_BIN:-/usr/local/bin/krakenhashes-agent}"
BACKEND_HOST="${BACKEND_HOST:-localhost:31337}"

echo "==> 1. Confirm there are no ACTIVE on-prem agents"
PSQL -tAc "SELECT count(*) FROM agents WHERE status = 'active' AND cloud_instance_id IS NULL;" \
  | awk '{ if ($1+0 > 0) { print "    WARNING: "$1" active on-prem agent(s). The autoscaler refuses"; \
                           print "    to rent while any on-prem agent is idle, so provisioning will"; \
                           print "    never trigger. Disconnect them for a true cloud-only rehearsal."; } \
          else print "    OK: zero active on-prem agents" }'

echo "==> 2. Mock provider config (\$0 — spawns a local --test-mode agent)"
PSQL -v ON_ERROR_STOP=1 <<SQL
INSERT INTO cloud_provider_configs (id, name, provider, enabled, credentials_encrypted, settings)
VALUES (gen_random_uuid(), 'Rehearsal Mock', 'mock', true, '',
        jsonb_build_object(
          'agent_binary', '${AGENT_BIN}',
          'backend_host', '${BACKEND_HOST}',
          'hourly_rate_cents', 100,
          'boot_delay_seconds', 5
        ))
ON CONFLICT (name) DO UPDATE
   SET enabled = true, settings = EXCLUDED.settings;
SQL
PSQL -tAc "SELECT '    provider: '||name||' ('||provider||', enabled='||enabled||')' FROM cloud_provider_configs WHERE provider='mock';"

echo "==> 3. Billing client with a small budget"
PSQL -v ON_ERROR_STOP=1 <<'SQL'
INSERT INTO clients (id, name, description, cloud_enabled, cloud_budget_cents,
                     cloud_budget_period, cloud_provider_allowlist, max_instance_ttl_minutes)
VALUES (gen_random_uuid(), 'Unassigned Work',
        'Rehearsal billing client; also the cloud_default_client_id target.',
        true, 500, 'monthly', ARRAY['mock'], 60)
ON CONFLICT (name) DO UPDATE
   SET cloud_enabled = true, cloud_budget_cents = 500,
       cloud_provider_allowlist = ARRAY['mock'], max_instance_ttl_minutes = 60;
SQL

echo "==> 4. System settings for a cloud-only deployment"
PSQL -v ON_ERROR_STOP=1 <<'SQL'
-- The allowlist must admit the mock provider, or every job is ineligible.
UPDATE system_settings SET value = 'mock'  WHERE key = 'cloud_default_provider_allowlist';
UPDATE system_settings SET value = 'true'  WHERE key = 'cloud_default_cloud_enabled';
-- Every job opted in, so no per-preset ticking. This is the setting a
-- cloud-only operator would otherwise have to discover the hard way.
UPDATE system_settings SET value = 'true'  WHERE key = 'cloud_default_burst_enabled';
-- Unassigned work bills to the rehearsal client.
UPDATE system_settings SET value = (SELECT id::text FROM clients WHERE name = 'Unassigned Work')
  WHERE key = 'cloud_default_client_id';
-- 0 would disable cloud entirely; the UI warns about this but the default is 0.
UPDATE system_settings SET value = '500' WHERE key = 'cloud_global_monthly_cap_cents';
UPDATE system_settings SET value = '2'   WHERE key = 'cloud_global_concurrent_instance_cap';
SQL
PSQL -c "SELECT key, value FROM system_settings WHERE key LIKE 'cloud_%' ORDER BY key;"

echo
echo "==> Setup complete. What to verify, in order:"
cat <<'EOF'
    A. REFUSALS REACH THE UI  (needs only the backend rebuild)
       - Create a hashlist with NO client and a job on it. It should provision
         via the default billing client rather than being silently skipped.
       - Clear cloud_default_client_id, create another. The job detail page
         must now SAY why, and "Provision now" must name the missing client.
       - Set cloud_global_monthly_cap_cents = 0. Every job must report the
         cap, not sit silent.

    B. CHUNK FLOOR - Fix A  (needs the agent binary too)
       Let an instance register and take a task, then:
         SELECT chunk_duration FROM job_tasks ORDER BY assigned_at DESC LIMIT 1;
       Expect 3600 (cloud_chunk_duration_seconds), NOT the job's own value.
       Before the fix this was always the per-job value.

    C. DRAIN RUNG - Fix B  (needs the agent binary too)
       With an instance working a chunk, push the client to >=99% of cap:
         INSERT INTO cloud_spend_ledger (client_id, cents, kind, recorded_at)
         VALUES ('<client-id>', 495, 'reservation', NOW());
       Within one reaper sweep (60s) expect:
         - cloud_instances.state = 'draining', drain_started_at stamped
         - the agent NOT offered new work (no new job_tasks rows)
         - destroyed once the in-flight task completes
       Then release the reservation and confirm a fresh instance resumes.
EOF
