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
# LOOKING FOR THE AUTOMATED TEST? Use scripts/cloud-e2e-test.sh. It creates its
# own client, hashlist and job THROUGH THE REST API, drives a full rental to
# teardown, and asserts each step (TTL sizing, chunk clamp, crack handshake,
# release reason, reservation refund) with PASS/FAIL lines. It also injects a
# deliberate crack-processing fault to prove an unsatisfiable handshake is given
# up on immediately rather than waiting out the stale-processing timeout.
#
# THIS script is the manual counterpart: it sets a deployment up and then hands
# you a checklist. Prefer the automated one for regression checking; use this
# when you want to poke at the system by hand, or to rehearse the operator
# experience a tester will actually have.
#
# PREREQUISITES (both need a build, which this script does NOT do):
#   1. Backend built from a commit containing the cloud-only work. There is NO
#      `strings` binary in the container, so use grep -a — and ALWAYS include a
#      positive control, or a stale image reads the same as a missing key:
#        docker exec krakenhashes-app sh -c \
#          'grep -c -aF cloud_global_monthly_cap_cents /usr/local/bin/krakenhashes'  # control, expect >0
#        docker exec krakenhashes-app sh -c \
#          'grep -c -aF cloud_default_client_id /usr/local/bin/krakenhashes'         # expect >0
#      A 0 on the control means your grep is wrong, not the binary.
#   2. An agent binary INSIDE the backend container. MockProvider.Launch execs
#      it directly, so a host path is not enough:
#        cd agent && make clean && make linux-amd64
#        docker cp ../bin/agent/krakenhashes-agent-linux-amd64 \
#          krakenhashes-app:/usr/local/bin/krakenhashes-agent
#      Without it, Preflight reports "agent binary not found" and no instance
#      ever registers — which still exercises the refusal paths, but not the
#      chunk floor or the drain rung.
#
# A VPN PROVIDER IS MANDATORY, including for the mock. Provisioning fails closed
# with "no VPN provider configured; refusing to launch an instance that cannot
# reach the backend", by design — agents never talk to the backend over the open
# internet. Reusable-key kinds mint offline with no API call, so a real config's
# NetBird credential can be copied onto the mock config for local runs:
#
#   UPDATE cloud_provider_configs m
#      SET vpn_provider = a.vpn_provider, vpn_credential_kind = a.vpn_credential_kind,
#          vpn_credential_encrypted = a.vpn_credential_encrypted,
#          vpn_credential_expires_at = a.vpn_credential_expires_at,
#          vpn_tag_or_group = a.vpn_tag_or_group, backend_vpn_host = a.backend_vpn_host
#     FROM cloud_provider_configs a
#    WHERE m.provider = 'mock' AND a.provider = 'aws';
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

    D. CLEAN RELEASE - the happy path  (needs the agent binary too)
       This is the one everything else assumes and nothing had ever shown:
       work finishes, and the instance is given back PROMPTLY rather than
       billing to its TTL. Every teardown rung below it (TTL, drain, crack
       drain) is a backstop for when this does not happen.

       Use a job small enough that ONE chunk covers it, or the agent simply
       takes another chunk and never goes idle:
         UPDATE scheduling_units SET base_keyspace = 1000000 WHERE id = '<unit>';

       Let it run to completion, then expect, in order:
         - job_tasks.status = 'completed', received_crack_count >= expected,
           batches_complete_signaled = true
         - job_executions.status = 'completed'
         - within one reaper sweep (60s), cloud_instances.state = 'terminated'
           with termination_reason 'job finished; instance is no longer needed'
           (the job-finished rung fires first; the 5-minute idle drain is the
            fallback for an instance whose job is still open but has no work)
         - agents.retired_at stamped
         - cloud_spend_ledger has a NEGATIVE 'release' row for the instance,
           equal to reserved_cents minus the sum of its 'incurred' rows

       That last line is the one worth checking carefully. It is what makes an
       over-long TTL cheap: the unused reservation comes back, so only the time
       actually used is ever charged. If it is missing, every TTL decision in
       the system is more expensive than it looks.

    TRAPS THAT WILL COST YOU AN HOUR

      * ONE RENTAL PER RUN, unless your backend has the per-instance mock config
        directory. The agent resolves its config from KH_CONFIG_DIR, and the
        mock passes the BACKEND's environment, so on older builds every mock
        agent shares /etc/krakenhashes. The second rental then silently reuses
        the first agent's credentials, reconnects as that agent, and its own
        instance sits at ready_at IS NULL until the ready deadline kills it --
        which looks exactly like a provisioning failure and is not one.
        Between rentals on such a build:
          docker exec krakenhashes-app sh -c \
            'rm -f /etc/krakenhashes/{agent.key,ca.crt,client.crt,client.key,ready.json}'
        Delete ONLY those. /etc/krakenhashes/certs/ is the BACKEND's own TLS
        material and /etc/krakenhashes/.env is its config -- a plain `ls` hides
        the dotfile, so it is easy to wipe by accident.

      * DO NOT reset a running job by deleting its job_tasks rows. The agent
        keeps the task in memory and rejects every re-dispatch ("Agent rejected
        task assignment (race)"). Expire the instance's TTL instead and let the
        reaper take the agent down with it:
          UPDATE cloud_instances SET ttl_epoch = NOW() - INTERVAL '1 minute'
           WHERE state NOT IN ('terminated','failed');

      * A hand-built job_executions row must carry the columns the crack path
        scans. wordlist_ids/rule_ids are JSON ARRAYS ('["44"]', not '{}') and
        mask must be '' rather than NULL, or every crack batch fails with
        "failed to get job execution" and the task wedges in 'processing'
        with received_crack_count stuck at 0. Copy a real job's row shape.
EOF
