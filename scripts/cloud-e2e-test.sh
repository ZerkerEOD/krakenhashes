#!/usr/bin/env bash
#
# Automated end-to-end test of cloud provisioning against the $0 mock provider.
#
# Runs two phases and asserts each one, so a regression shows up as a FAIL line
# rather than as a rehearsal that "looked fine":
#
#   Phase 1  happy path — provision, commission, dispatch, complete, release,
#            and refund. This is the path everything else assumes works.
#   Phase 2  the crack handshake fails unrecoverably. Proves the task is given
#            up on immediately instead of sitting in 'processing' until the
#            30-minute stale backstop (billing a rented GPU the whole time).
#
# EVERYTHING IT OPERATES ON IS CREATED THROUGH THE REST API. That is the point.
# Every bug chased on this branch traced back to a hand-written fixture row -- a
# JSON object where an array belonged, a NULL mask, a hashlist claiming 100
# hashes while containing none -- and not to the product. SQL is used only to
# OBSERVE, and in Phase 2 to inject one deliberate, documented fault.
#
# Spends nothing: the mock provider launches a local --test-mode agent instead
# of renting hardware, and it disables the AWS provider for the duration.
#
# PREREQUISITES
#   - Backend running (docker compose -f docker-compose.dev-local.yml up -d)
#   - An agent binary; set AGENT_SRC if it is not at the default path below
#   - A configured provider with a reusable VPN credential to borrow. A VPN is
#     mandatory by design, including for the mock -- agents never reach the
#     backend over the open internet.
#   - Optionally KH_API_EMAIL / KH_API_KEY. Supply those and the harness uses
#     your credential and creates no account of its own. Without them it mints a
#     dedicated `kh-autotest` admin user so unattended runs still work.
#
# USAGE
#   scripts/cloud-e2e-test.sh            # both phases
#   scripts/cloud-e2e-test.sh 1          # happy path only
#   scripts/cloud-e2e-test.sh 2          # handshake failure only
#
set -uo pipefail

API="${API:-https://localhost/api/v1}"
AGENT_SRC="${AGENT_SRC:-$HOME/Programming/passwordCracking/agent/krakenhashes-agent}"
APP="${APP_CONTAINER:-krakenhashes-app}"
DB="${DB_CONTAINER:-krakenhashes-postgres}"
TEST_USER="kh-autotest"
TEST_EMAIL="kh-autotest@local.invalid"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

PSQL() { docker exec "$DB" psql -U krakenhashes -d krakenhashes -tAc "$1" 2>/dev/null; }
PSQLF(){ docker exec -i "$DB" psql -U krakenhashes -d krakenhashes 2>/dev/null; }

PASS=0; FAIL=0
ok()   { echo "  PASS  $1"; PASS=$((PASS+1)); }
bad()  { echo "  FAIL  $1"; FAIL=$((FAIL+1)); }
check(){ if [[ "$2" =~ $3 ]]; then ok "$1 ($2)"; else bad "$1 — got '$2', want /$3/"; fi }
step() { echo; echo "== $* =="; }

# --------------------------------------------------------------- credentials --
# Prefer a credential the operator supplies; otherwise mint a dedicated one.
#
# Supply your own with:
#     KH_API_EMAIL=you@example.com KH_API_KEY=... scripts/cloud-e2e-test.sh
#
# Read from the ENVIRONMENT and never written to disk, so the key does not end
# up in a file, a log, or this repository. Note that it is still visible to
# anything that can read the process list on this machine.
#
# With no credential supplied the harness creates a dedicated `kh-autotest`
# admin user instead. That keeps unattended runs possible, but it is a real
# admin credential living in the database — if you do not want one, pass your
# own key and delete the account:
#     DELETE FROM user_teams WHERE user_id = (SELECT id FROM users WHERE username='kh-autotest');
#     DELETE FROM users WHERE username = 'kh-autotest';
ensure_api_key() {
  if [[ -n "${KH_API_KEY:-}" ]]; then
    KEY="$KH_API_KEY"
    TEST_EMAIL="${KH_API_EMAIL:-$TEST_EMAIL}"
    ok "using the API credential supplied in the environment ($TEST_EMAIL)"
    return
  fi
  # Creating an admin account is OPT-IN, never a silent fallback.
  #
  # An earlier version minted `kh-autotest` automatically whenever no credential
  # was supplied. That is a real admin API key appearing in the database as a
  # side effect of running a test — exactly the kind of account nobody remembers
  # creating and nobody audits. Refuse instead, and say what to do about it.
  if [[ "${KH_ALLOW_TEST_USER:-0}" != "1" ]]; then
    echo "  ERROR  no API credential supplied."
    echo "         Pass one:   KH_API_EMAIL=you@example.com KH_API_KEY=... $0"
    echo "         Or, to let this script create a dedicated admin user for"
    echo "         unattended runs (it will persist until you delete it):"
    echo "                     KH_ALLOW_TEST_USER=1 $0"
    exit 2
  fi
  echo "  NOTE  creating admin user '$TEST_USER' — it persists after this run."
  KEY=$(python3 -c 'import secrets;print(secrets.token_hex(24))')
  python3 - "$KEY" "$WORK/seed.sql" <<'PY'
import bcrypt, secrets, sys
key, out = sys.argv[1], sys.argv[2]
kh = bcrypt.hashpw(key.encode(), bcrypt.gensalt()).decode()
ph = bcrypt.hashpw(secrets.token_hex(16).encode(), bcrypt.gensalt()).decode()
open(out, "w").write(f"""
INSERT INTO users (username, email, password_hash, role, status, api_key)
VALUES ('kh-autotest', 'kh-autotest@local.invalid', '{ph}', 'admin', 'active', '{kh}')
ON CONFLICT (username) DO UPDATE SET api_key = EXCLUDED.api_key, role='admin', status='active';
INSERT INTO user_teams (user_id, team_id, role)
SELECT u.id, '00000000-0000-0000-0000-000000000001', 'admin'
  FROM users u WHERE u.username = 'kh-autotest'
ON CONFLICT (user_id, team_id) DO UPDATE SET role = 'admin';
""")
PY
  PSQLF < "$WORK/seed.sql" >/dev/null
}
api() { curl -sk --max-time 30 -H "X-User-Email: $TEST_EMAIL" -H "X-API-Key: $KEY" "$@"; }

# Hash import is ASYNCHRONOUS: the POST returns before the processor has written
# the rows, so an immediate count reads zero.
wait_hashlist() {
  local id="$1"
  for _ in $(seq 1 40); do
    [[ "$(PSQL "SELECT status FROM hashlists WHERE id=$id;")" == "ready" ]] && return 0
    sleep 2
  done
  return 1
}

make_hashlist() { # make_hashlist <name> <client-uuid> -> echoes hashlist id
  local name="$1" client="$2" f="$WORK/$1.txt"
  python3 - "$f" "$name" <<'PY'
import hashlib, sys
# Real MD5s of real passwords. A real agent would crack these; the mock will not
# (see the note the harness prints at the end).
words = ["password","123456","letmein","dragon","monkey","qwerty","football",
         "iloveyou","sunshine","princess","welcome","shadow"]
salt = sys.argv[2]
open(sys.argv[1], "w").write("\n".join(
    hashlib.md5((w + salt).encode()).hexdigest() for w in words) + "\n")
PY
  api -X POST -F "name=$name" -F "client_id=$client" -F "hash_type_id=0" -F "file=@$f" \
      "$API/hashlists" | python3 -c 'import json,sys;print(json.load(sys.stdin).get("id",""))'
}

# --------------------------------------------------------------------- setup --
setup() {
  step "Setup"
  ensure_api_key
  check "API reachable and authenticating" \
        "$(api -o /dev/null -w '%{http_code}' "$API/health")" '^200$'

  if docker exec "$APP" test -x /usr/local/bin/krakenhashes-agent; then
    ok "agent binary already staged in $APP"
  elif [[ -x "$AGENT_SRC" ]]; then
    docker cp "$AGENT_SRC" "$APP":/usr/local/bin/krakenhashes-agent >/dev/null
    docker exec "$APP" chmod +x /usr/local/bin/krakenhashes-agent
    ok "staged agent binary from $AGENT_SRC"
  else
    bad "no agent binary at $AGENT_SRC — the mock provider has nothing to launch"
    exit 1
  fi

  # A rented machine boots with an empty disk. Backends without the per-instance
  # mock config dir share /etc/krakenhashes between agents, so a second rental
  # silently reconnects as the first agent and its own instance never registers.
  docker exec "$APP" sh -c 'rm -f /etc/krakenhashes/agent.key /etc/krakenhashes/ca.crt \
      /etc/krakenhashes/client.crt /etc/krakenhashes/client.key /etc/krakenhashes/ready.json' 2>/dev/null
  ok "cleared stale agent credentials"

  # Snapshot everything this run is about to change, so restore() can put it
  # back EXACTLY. Anything mutated without being recorded here is left behind as
  # a surprise for the operator — the first version of this harness disabled the
  # AWS provider and never re-enabled it, which would have made the next real
  # launch silently do nothing.
  PSQL "SELECT string_agg(id::text, ',') FROM cloud_provider_configs WHERE enabled AND provider <> 'mock';" > "$WORK/reenable"
  PSQL "SELECT string_agg(format('%s=%s', key, value), E'\n')
          FROM system_settings
         WHERE key IN ('cloud_default_provider_allowlist','cloud_default_burst_enabled',
                       'cloud_default_cloud_enabled','cloud_global_monthly_cap_cents',
                       'cloud_default_max_instances_per_job','cloud_global_concurrent_instance_cap',
                       'cloud_default_client_id');" > "$WORK/settings"

  PSQLF >/dev/null <<'SQL'
INSERT INTO cloud_provider_configs (id, name, provider, enabled, credentials_encrypted, settings)
VALUES (gen_random_uuid(), 'E2E Mock', 'mock', true, '',
        jsonb_build_object('agent_binary','/usr/local/bin/krakenhashes-agent',
                           'backend_host','localhost:31337',
                           'hourly_rate_cents',100,'boot_delay_seconds',5))
ON CONFLICT (name) DO UPDATE SET enabled = true, settings = EXCLUDED.settings;
-- Reusable-key VPN kinds mint offline with no provider API call, so a real
-- config's credential can be borrowed for a local run.
UPDATE cloud_provider_configs m
   SET vpn_provider = a.vpn_provider, vpn_credential_kind = a.vpn_credential_kind,
       vpn_credential_encrypted = a.vpn_credential_encrypted,
       vpn_credential_expires_at = a.vpn_credential_expires_at,
       vpn_tag_or_group = a.vpn_tag_or_group, backend_vpn_host = a.backend_vpn_host
  FROM cloud_provider_configs a
 WHERE m.name = 'E2E Mock' AND a.provider <> 'mock' AND a.vpn_credential_encrypted IS NOT NULL;
UPDATE cloud_provider_configs SET enabled = false WHERE provider <> 'mock';
UPDATE system_settings SET value='mock' WHERE key='cloud_default_provider_allowlist';
UPDATE system_settings SET value='true' WHERE key='cloud_default_burst_enabled';
UPDATE system_settings SET value='true' WHERE key='cloud_default_cloud_enabled';
UPDATE system_settings SET value='500'  WHERE key='cloud_global_monthly_cap_cents';
UPDATE system_settings SET value='1'    WHERE key='cloud_default_max_instances_per_job';
UPDATE system_settings SET value='2'    WHERE key='cloud_global_concurrent_instance_cap';
SQL
  check "mock provider has a VPN credential (mandatory, no bypass)" \
        "$(PSQL "SELECT CASE WHEN vpn_credential_encrypted IS NOT NULL THEN 'yes' ELSE 'no' END FROM cloud_provider_configs WHERE name='E2E Mock';")" '^yes$'
  ok "every non-mock provider disabled — no real spend is possible"

  PRESET=$(PSQL "SELECT id FROM preset_jobs ORDER BY (SELECT COALESCE(MIN(w.word_count), 9223372036854775807)
                   FROM wordlists w WHERE w.id::text = ANY(SELECT jsonb_array_elements_text(preset_jobs.wordlist_ids::jsonb))) ASC NULLS LAST LIMIT 1;")
  check "picked a preset job with a small wordlist" "${PRESET:0:8}" '^[0-9a-f]{8}$'

  CLIENT=$(api -X POST -H 'Content-Type: application/json' \
    -d "{\"name\":\"E2E $(date -u +%H%M%S)\",\"description\":\"automated cloud e2e\",\"team_id\":\"00000000-0000-0000-0000-000000000001\"}" \
    "$API/clients" | python3 -c 'import json,sys;print(json.load(sys.stdin).get("id",""))')
  check "client created via API" "${CLIENT:0:8}" '^[0-9a-f]{8}$'
  PSQL "UPDATE clients SET cloud_enabled=true, cloud_budget_cents=500, cloud_budget_period='monthly',
        cloud_provider_allowlist=ARRAY['mock'], max_instance_ttl_minutes=60 WHERE id='$CLIENT';" >/dev/null
  ok "client funded (500c, allowlist=mock, 60m TTL ceiling)"
}

restore() {
  step "Restore"
  PSQL "UPDATE cloud_provider_configs SET enabled = false WHERE provider = 'mock';" >/dev/null

  # Put every setting back to the exact value it had before this run.
  local restored=0
  while IFS='=' read -r k v; do
    [[ -z "$k" ]] && continue
    PSQL "UPDATE system_settings SET value='${v//\'/\'\'}' WHERE key='$k';" >/dev/null
    restored=$((restored+1))
  done < "$WORK/settings"
  check "cloud settings restored to their pre-run values" "$restored" '^[1-9][0-9]*$'

  local back; back="$(cat "$WORK/reenable" 2>/dev/null)"
  if [[ -n "$back" ]]; then
    PSQL "UPDATE cloud_provider_configs SET enabled = true WHERE id IN ('${back//,/\',\'}');" >/dev/null
    ok "re-enabled the provider(s) this run disabled"
  else
    ok "no non-mock provider was enabled before this run; none re-enabled"
  fi

  # WAIT for the reaper rather than racing it. Deleting a job leaves its
  # instance live until the next sweep (default 60s), which is correct
  # behaviour — but a restore check that fires immediately reports a leak that
  # is not there, and a harness that cries wolf about a leaked GPU is worse than
  # one that says nothing. Give it three sweeps, then fail for real.
  local interval; interval=$(PSQL "SELECT COALESCE(value::int,60) FROM system_settings WHERE key='cloud_reaper_interval_seconds';")
  local live=1
  for _ in $(seq 1 $(( 3 * (interval > 0 ? interval : 60) / 10 ))); do
    live=$(PSQL "SELECT count(*) FROM cloud_instances WHERE state NOT IN ('terminated','failed');")
    [[ "$live" == "0" ]] && break
    sleep 10
  done
  check "every instance torn down (waited up to 3 reaper sweeps)" "$live" '^0$'
  [[ "$live" != "0" ]] && PSQL "SELECT '  still live: '||label||' state='||state FROM cloud_instances WHERE state NOT IN ('terminated','failed');"
}

# ------------------------------------------------------------------ phase 1 --
phase1() {
  step "Phase 1 — happy path"
  local HL JOB
  HL=$(make_hashlist "e2e-$(date -u +%H%M%S)" "$CLIENT")
  wait_hashlist "$HL" || bad "hashlist $HL never became ready"
  check "hashlist actually contains hashes (not a phantom)" \
        "$(PSQL "SELECT count(*) FROM hashlist_hashes WHERE hashlist_id=$HL;")" '^[1-9][0-9]*$'

  JOB=$(api -X POST -H 'Content-Type: application/json' \
    -d "{\"name\":\"e2e\",\"hashlist_id\":$HL,\"preset_job_id\":\"$PRESET\",\"priority\":900,\"max_agents\":1}" \
    "$API/jobs" | python3 -c 'import json,sys;print(json.load(sys.stdin).get("id",""))')
  check "job created via API" "${JOB:0:8}" '^[0-9a-f]{8}$'
  check "job row well-formed (arrays, non-NULL mask)" \
        "$(PSQL "SELECT (jsonb_typeof(wordlist_ids::jsonb)='array')::text||'/'||(mask IS NOT NULL)::text FROM job_executions WHERE id='$JOB';")" \
        '^true/true$'

  echo "  watching (autoscaler 60s, mock task ~5m)…"
  local DL=$(( $(date +%s) + 1500 )) INST="" JS="" IS=""
  while [[ $(date +%s) -lt $DL ]]; do
    INST=$(PSQL "SELECT id FROM cloud_instances WHERE job_execution_id='$JOB' ORDER BY created_at DESC LIMIT 1;")
    JS=$(PSQL "SELECT status FROM job_executions WHERE id='$JOB';")
    IS=$(PSQL "SELECT state FROM cloud_instances WHERE id='$INST';")
    [[ "$JS" == "completed" && "$IS" == "terminated" ]] && break
    sleep 10
  done

  [[ -n "$INST" ]] || { bad "no instance was provisioned for the job"; return; }
  local TTL CEIL CHUNK SLACK HELD RES EST REL
  TTL=$(PSQL "SELECT ROUND(EXTRACT(EPOCH FROM (ttl_epoch-created_at))/60)::int FROM cloud_instances WHERE id='$INST';")
  CEIL=$(PSQL "SELECT COALESCE(max_instance_ttl_minutes,240) FROM clients WHERE id='$CLIENT';")
  check "TTL within the client's ceiling"            "$(( TTL<=CEIL?1:0 ))" '^1$'
  check "TTL at least the minimum useful rental"     "$(( TTL>=60?1:0 ))"   '^1$'
  check "agent registered against its own instance"  "$(PSQL "SELECT CASE WHEN ready_at IS NULL THEN 'never' ELSE 'yes' END FROM cloud_instances WHERE id='$INST';")" '^yes$'

  CHUNK=$(PSQL "SELECT chunk_duration FROM job_tasks WHERE job_execution_id='$JOB' ORDER BY created_at DESC LIMIT 1;")
  SLACK=$(PSQL "SELECT value::int FROM system_settings WHERE key='cloud_teardown_slack_seconds';")
  check "a chunk was dispatched"                     "${CHUNK:-none}" '^[0-9]+$'
  [[ "$CHUNK" =~ ^[0-9]+$ ]] && check "chunk fits inside TTL minus teardown slack" "$(( CHUNK <= TTL*60-SLACK ?1:0 ))" '^1$'

  local HS; HS=$(PSQL "SELECT status||'|'||COALESCE(received_crack_count,0)||'|'||COALESCE(expected_crack_count,0)||'|'||COALESCE(unrecoverable_crack_count,0) FROM job_tasks WHERE job_execution_id='$JOB' ORDER BY created_at DESC LIMIT 1;")
  local TS TR TE TU; IFS='|' read -r TS TR TE TU <<< "$HS"
  check "task completed"                             "$TS" '^completed$'
  check "handshake satisfied (received >= expected)" "$(( TR>=TE?1:0 ))" '^1$'
  check "no cracks rejected"                         "$TU" '^0$'
  check "job completed"                              "$JS" '^completed$'
  check "instance released, not left running"        "$IS" '^terminated$'
  check "released because the job finished"          "$(PSQL "SELECT COALESCE(termination_reason,'-') FROM cloud_instances WHERE id='$INST';")" 'job finished'

  HELD=$(PSQL "SELECT ROUND(EXTRACT(EPOCH FROM (terminated_at-created_at))/60)::int FROM cloud_instances WHERE id='$INST';")
  check "released promptly, well under its TTL"      "$(( HELD<TTL?1:0 ))" '^1$'
  RES=$(PSQL "SELECT COALESCE(reserved_cents,0) FROM cloud_instances WHERE id='$INST';")
  EST=$(PSQL "SELECT COALESCE(estimated_cost_cents,0) FROM cloud_instances WHERE id='$INST';")
  REL=$(PSQL "SELECT COALESCE(SUM(cents),0) FROM cloud_spend_ledger WHERE kind='release' AND note LIKE '%'||(SELECT label FROM cloud_instances WHERE id='$INST')||'%';")
  # The property the whole TTL design rests on: an over-long rental is refunded,
  # so reserving generously is cheap and under-reserving is what costs money.
  check "unused reservation refunded exactly"        "$(( RES-EST+REL ))" '^0$'
  echo "        TTL=${TTL}m held=${HELD}m reserved=${RES}c incurred=${EST}c released=${REL}c chunk=${CHUNK}s"
  check "agent retired with its instance"            "$(PSQL "SELECT CASE WHEN retired_at IS NULL THEN 'no' ELSE 'yes' END FROM agents WHERE cloud_instance_id='$INST';")" '^yes$'

  api -X DELETE "$API/hashlists/$HL" >/dev/null 2>&1
}

# ------------------------------------------------------------------ phase 2 --
phase2() {
  step "Phase 2 — crack handshake that can never be satisfied"
  docker exec "$APP" sh -c 'rm -f /etc/krakenhashes/agent.key /etc/krakenhashes/ca.crt \
      /etc/krakenhashes/client.crt /etc/krakenhashes/client.key /etc/krakenhashes/ready.json' 2>/dev/null

  local HL JOB
  HL=$(make_hashlist "p2-$(date -u +%H%M%S)" "$CLIENT")
  wait_hashlist "$HL" || bad "hashlist $HL never became ready"
  JOB=$(api -X POST -H 'Content-Type: application/json' \
    -d "{\"name\":\"p2\",\"hashlist_id\":$HL,\"preset_job_id\":\"$PRESET\",\"priority\":900,\"max_agents\":1}" \
    "$API/jobs" | python3 -c 'import json,sys;print(json.load(sys.stdin).get("id",""))')
  check "job created via API" "${JOB:0:8}" '^[0-9a-f]{8}$'

  # THE FAULT: wordlist_ids becomes a JSON object where models.IDArray expects an
  # array, so GetJobExecution cannot scan the row and every crack batch fails
  # with a non-transient error. This is the exact shape a hand-built fixture had.
  PSQL "UPDATE job_executions SET wordlist_ids='{}' WHERE id='$JOB';" >/dev/null
  check "fault injected (wordlist_ids is now an object)" \
        "$(PSQL "SELECT jsonb_typeof(wordlist_ids::jsonb) FROM job_executions WHERE id='$JOB';")" '^object$'

  echo "  watching…"
  local DL=$(( $(date +%s) + 1500 )) ST="" PROC="" TERM=""
  while [[ $(date +%s) -lt $DL ]]; do
    ST=$(PSQL "SELECT status FROM job_tasks WHERE job_execution_id='$JOB' ORDER BY created_at ASC LIMIT 1;")
    [[ "$ST" == "processing" && -z "$PROC" ]] && PROC=$(date +%s)
    [[ "$ST" == "completed" || "$ST" == "cancelled" ]] && { TERM=$(date +%s); break; }
    sleep 5
  done

  local R; R=$(PSQL "SELECT COALESCE(received_crack_count,0)||'|'||COALESCE(expected_crack_count,0)||'|'||COALESCE(unrecoverable_crack_count,0)||'|'||COALESCE(batches_complete_signaled,false)::text FROM job_tasks WHERE job_execution_id='$JOB' ORDER BY created_at ASC LIMIT 1;")
  local RECV EXP UNREC SIG; IFS='|' read -r RECV EXP UNREC SIG <<< "$R"
  check "rejected cracks recorded separately"          "$UNREC" '^[1-9][0-9]*$'
  check "they were NOT counted as received"            "$(( RECV<EXP?1:0 ))" '^1$'
  check "shortfall fully accounted (recv+rejected>=exp)" "$(( RECV+UNREC>=EXP?1:0 ))" '^1$'
  check "task did NOT wedge in 'processing'"           "$ST" '^(completed|cancelled)$'
  [[ -n "$PROC" && -n "$TERM" ]] && \
    check "given up on in seconds, not the 30-minute timeout" "$(( TERM-PROC<300?1:0 ))" '^1$'
  check "backend logged why it gave up" \
        "$(docker exec "$APP" sh -c "grep -c 'crack handshake cannot be satisfied' /var/log/krakenhashes/backend/backend.log" 2>/dev/null)" '^[1-9][0-9]*$'
  echo "        received=$RECV expected=$EXP unrecoverable=$UNREC signalled=$SIG status=$ST"

  # Abandonment re-opens the keyspace, so this job would be re-dispatched and
  # fail identically forever. Remove it.
  api -X DELETE "$API/hashlists/$HL" >/dev/null 2>&1
  check "faulted job removed (no re-dispatch loop left behind)" \
        "$(PSQL "SELECT count(*) FROM job_executions WHERE id='$JOB';")" '^0$'
}

# ---------------------------------------------------------------------- main --
WHICH="${1:-all}"
setup
[[ "$WHICH" == "all" || "$WHICH" == "1" ]] && phase1
[[ "$WHICH" == "all" || "$WHICH" == "2" ]] && phase2
api -X DELETE "$API/clients/$CLIENT" >/dev/null 2>&1
restore

echo
echo "================================================"
echo " PASS=$PASS  FAIL=$FAIL"
echo "================================================"
echo
echo "NOTE: the mock agent never runs hashcat. It emits synthetic hash values"
echo "that match nothing, so a hashlist will read 0 cracked no matter what."
echo "This harness proves provisioning, TTL sizing, chunking, the crack"
echo "HANDSHAKE and teardown. It cannot prove crack RECORDING — only a real"
echo "agent running real hashcat can do that."
[[ $FAIL -eq 0 ]]
