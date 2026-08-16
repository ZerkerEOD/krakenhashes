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

run_watchdog_case() {
    local name="$1" ; shift
    local timeout_s="$1" ; shift
    local workdir
    workdir="$(mktemp -d)"

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
# With a short timeout and nothing refreshing the file, the heartbeat watchdog
# fires. This is correct behaviour for a genuinely disconnected agent.
result="$(run_watchdog_case heartbeat_stale 6 KH_DEADLINE_EPOCH=0 KH_HEARTBEAT_LOSS_TIMEOUT=2)"
if echo "$result" | grep -q "no contact with backend"; then
    ok "a stale heartbeat self-destructs"
else
    bad "a stale heartbeat did NOT self-destruct (got: ${result:-nothing})"
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
echo "passed: $pass, failed: $fail"
[ "$fail" -eq 0 ]
