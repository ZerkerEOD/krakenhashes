#!/usr/bin/env bash
#
# Tests the in-guest watchdog logic from docker-entrypoint-cloud.sh without a
# container, a provider, or a 15-minute wait.
#
# Run:  ./agent/watchdog_test.sh
#
# The watchdog is the only teardown tier that survives the backend disappearing,
# so its two timers are the last line of defence against an instance billing
# forever. Both are asserted here.

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ENTRYPOINT="$SCRIPT_DIR/docker-entrypoint-cloud.sh"

pass=0
fail=0

ok()   { echo "  PASS: $1"; pass=$((pass + 1)); }
bad()  { echo "  FAIL: $1"; fail=$((fail + 1)); }

# extract_watchdog pulls the watchdog machinery out of the entrypoint and stubs
# self_destruct, so the timers can be driven without killing the test process.
# Everything after the watchdog block (VPN start, exec agent) is dropped.
extract_watchdog() {
    sed -n '/^HEARTBEAT_FILE=\|^: "${KH_HEARTBEAT_FILE/,/^watchdog &/p' "$ENTRYPOINT" \
        | sed 's/^watchdog &//'
}

# SEED_HEARTBEAT=1 pre-creates the heartbeat file, standing in for an agent that
# registered successfully at least once. The entrypoint deliberately no longer
# does this itself: a seeded file makes "reached the backend" and "just booted"
# indistinguishable, and under --restart=unless-stopped that reset the loss
# timer on every restart of a crash-looping agent.
run_watchdog_case() {
    local name="$1" ; shift
    local timeout_s="$1" ; shift
    local workdir
    workdir="$(mktemp -d)"

    if [ "${SEED_HEARTBEAT:-0}" = "1" ]; then
        date +%s > "$workdir/heartbeat"
    fi

    # Order matters: the extracted block defines the REAL self_destruct (which
    # curls the provider and calls poweroff), so the stub has to be defined
    # after it to win, and before watchdog is invoked.
    {
        echo 'log() { :; }'
        extract_watchdog
        echo "self_destruct() { echo \"DESTRUCT:\$1\" >> '$workdir/fired'; exit 0; }"
        echo 'log() { :; }'
        echo 'watchdog'
    } > "$workdir/wd.sh"

    ( cd "$workdir" && env "$@" \
        KH_HEARTBEAT_FILE="$workdir/heartbeat" \
        KH_WATCHDOG_INTERVAL=1 \
        bash "$workdir/wd.sh" ) >/dev/null 2>&1 &
    local pid=$!

    local waited=0
    while [ "$waited" -lt "$timeout_s" ]; do
        if [ -s "$workdir/fired" ]; then break; fi
        sleep 1
        waited=$((waited + 1))
    done
    kill "$pid" 2>/dev/null
    wait "$pid" 2>/dev/null

    if [ -s "$workdir/fired" ]; then
        cat "$workdir/fired"
    fi
    rm -rf "$workdir"
}

echo "=== absolute deadline ==="
# A deadline already in the past must fire on the first poll. This is the one
# mechanism that bounds spend when the backend is gone entirely.
result="$(run_watchdog_case past_deadline 5 KH_DEADLINE_EPOCH=1 KH_HEARTBEAT_LOSS_TIMEOUT=99999)"
if echo "$result" | grep -q "absolute deadline reached"; then
    ok "an elapsed absolute deadline self-destructs"
else
    bad "an elapsed absolute deadline did NOT self-destruct (got: ${result:-nothing})"
fi

# A deadline in the future must NOT fire.
result="$(run_watchdog_case future_deadline 4 KH_DEADLINE_EPOCH=$(( $(date +%s) + 3600 )) KH_HEARTBEAT_LOSS_TIMEOUT=99999)"
if [ -z "$result" ]; then
    ok "a future deadline does not fire early"
else
    bad "a future deadline fired early (got: $result)"
fi

echo "=== heartbeat loss ==="
# An agent that registered (file present) and then went quiet must self-destruct
# once the loss timeout elapses. This is correct behaviour for a genuinely
# disconnected agent.
result="$(SEED_HEARTBEAT=1 run_watchdog_case heartbeat_stale 6 KH_DEADLINE_EPOCH=0 KH_HEARTBEAT_LOSS_TIMEOUT=2)"
if echo "$result" | grep -q "no contact with backend"; then
    ok "a stale heartbeat self-destructs"
else
    bad "a stale heartbeat did NOT self-destruct (got: ${result:-nothing})"
fi

echo "=== ready deadline (agent never registered) ==="
# The gap found on a live AWS run: the VPN came up so the "VPN unavailable" rail
# passed, but the agent could not reach the backend, never registered and never
# wrote a heartbeat file. With no last contact to measure from, the loss timeout
# is silent — the instance was bounded only by its full TTL.
result="$(run_watchdog_case never_registered 6 KH_DEADLINE_EPOCH=0 KH_HEARTBEAT_LOSS_TIMEOUT=99999 KH_READY_DEADLINE_EPOCH=1)"
if echo "$result" | grep -q "never registered"; then
    ok "an agent that never registered self-destructs at the ready deadline"
else
    bad "an agent that never registered did NOT self-destruct (got: ${result:-nothing})"
fi

# The control: still inside the window, so nothing fires. Registration takes
# minutes (image pull, VPN join, file sync) and killing during it would make the
# rail unable to ever succeed.
result="$(run_watchdog_case ready_window_open 4 KH_DEADLINE_EPOCH=0 KH_HEARTBEAT_LOSS_TIMEOUT=99999 KH_READY_DEADLINE_EPOCH=$(( $(date +%s) + 3600 )))"
if [ -z "$result" ]; then
    ok "a ready deadline still in the future does not fire early"
else
    bad "a future ready deadline fired early (got: $result)"
fi

# A registered agent must be judged by the loss timeout, never by a ready
# deadline that has since passed — otherwise every instance would be destroyed
# the moment it outlived its registration window, mid-job.
result="$(SEED_HEARTBEAT=1 run_watchdog_case registered_past_ready 4 KH_DEADLINE_EPOCH=0 KH_HEARTBEAT_LOSS_TIMEOUT=99999 KH_READY_DEADLINE_EPOCH=1)"
if [ -z "$result" ]; then
    ok "an elapsed ready deadline does not fire once the agent has registered"
else
    bad "an elapsed ready deadline killed a registered agent (got: $result)"
fi

echo
echo "=== the entrypoint must not seed the heartbeat file ==="
echo "Seeding it on start makes 'reached the backend' and 'just booted' the same"
echo "state. Under --restart=unless-stopped, a crash-looping agent then rewrites"
echo "the timestamp every few seconds and the loss timer can never elapse."
echo
if grep -qE '^[[:space:]]*date \+%s > "\$HEARTBEAT_FILE"' "$ENTRYPOINT"; then
    bad "the entrypoint seeds the heartbeat file; the loss rail is defeated by the restart loop"
else
    ok "the heartbeat file is written only by the agent, on real contact"
fi

echo
echo "=== C1 regression guard ==="
echo "The heartbeat file must be refreshed by the agent on every successful"
echo "backend contact. If nothing writes it, a HEALTHY agent self-destructs"
echo "KH_HEARTBEAT_LOSS_TIMEOUT seconds after boot — which at the shipped 900s"
echo "default is shorter than one 3600s chunk, so no work can ever complete."
echo
if grep -rq "kh-last-contact\|KH_HEARTBEAT_FILE" "$SCRIPT_DIR/internal" 2>/dev/null; then
    ok "agent code refreshes the heartbeat file"
else
    bad "NOTHING in agent/internal refreshes the heartbeat file (defect C1)"
fi

echo
echo "=== GPU is stopped before any teardown branch ==="
echo "Every provider branch in self_destruct() can fail. If hashcat is only"
echo "killed inside one of them, a failed teardown keeps billing GPU rates the"
echo "whole way down to the last-resort kill."
echo
# The first pkill must appear before the first provider-specific branch.
pkill_line=$(grep -n 'pkill -9 hashcat' "$ENTRYPOINT" | head -1 | cut -d: -f1)
vast_line=$(grep -n 'CONTAINER_API_KEY' "$ENTRYPOINT" | head -1 | cut -d: -f1)
if [ -n "$pkill_line" ] && [ -n "$vast_line" ] && [ "$pkill_line" -lt "$vast_line" ]; then
    ok "hashcat is killed before the first provider branch"
else
    bad "hashcat is killed inside a provider branch (pkill@${pkill_line:-none}, vast@${vast_line:-none});"
    bad "  a failed teardown on any other provider bills GPU rates until the process dies"
fi

echo
echo "passed: $pass, failed: $fail"
[ "$fail" -eq 0 ]
