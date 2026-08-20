#!/bin/bash
#
# KrakenHashes cloud agent entrypoint.
#
# Runs as PID 1 in a rented GPU container. Responsibilities, in strict order:
#
#   1. Arm the absolute-deadline self-destruct. FIRST, before anything that can
#      hang. A VPN that never connects or an agent that never starts must still
#      result in a destroyed instance, and only a timer armed before those
#      steps can guarantee that.
#   2. Join the operator's VPN in USERSPACE mode, exposing a local SOCKS5 proxy.
#      Vast.ai containers are unprivileged: no /dev/net/tun, no NET_ADMIN, and
#      the API silently discards --cap-add/--device. Kernel-mode VPNs and
#      OpenVPN cannot work here at all.
#   3. Point the agent at that proxy and exec it.
#
set -uo pipefail

log() { echo "[kh-cloud $(date -u +%H:%M:%S)] $*"; }

# ---------------------------------------------------------------------------
# 1. Self-destruct watchdogs
# ---------------------------------------------------------------------------
#
# Two independent timers:
#
#   absolute deadline    KH_DEADLINE_EPOCH. Never reset. This is the hard cap
#                        on what this instance can ever cost.
#   heartbeat loss       KH_HEARTBEAT_LOSS_TIMEOUT. Reset whenever the agent
#                        proves it can reach the backend. Covers the case the
#                        absolute deadline is too coarse for: the control plane
#                        died an hour into a six-hour rental.
#
# self_destruct must NOT go through the VPN. On Vast.ai the destroy call is the
# whole point of the heartbeat timer — the tunnel being dead is exactly why
# we're firing — so the provider control plane is excluded via NO_PROXY.

# KH_HEARTBEAT_FILE is overridable so the watchdog can be exercised without a
# container, and KH_WATCHDOG_INTERVAL so that exercise takes seconds instead of
# the 30s poll x 900s timeout a real instance uses.
#
# EXPORTED, not just set: the agent is the only thing that knows whether it can
# still reach the backend, so the agent is what refreshes this file. Its
# presence in the environment is also what tells the agent it is running under a
# watchdog at all — an on-prem agent never sees it and writes nothing.
: "${KH_HEARTBEAT_FILE:=/tmp/kh-last-contact}"
export KH_HEARTBEAT_FILE
HEARTBEAT_FILE="$KH_HEARTBEAT_FILE"
: "${KH_DEADLINE_EPOCH:=0}"
: "${KH_HEARTBEAT_LOSS_TIMEOUT:=900}"
: "${KH_WATCHDOG_INTERVAL:=30}"
date +%s > "$HEARTBEAT_FILE"

self_destruct() {
    local reason="$1"
    log "SELF-DESTRUCT: $reason"

    # Vast.ai: every container gets $CONTAINER_ID and $CONTAINER_API_KEY, and
    # that key is scoped to destroying only this instance. DELETE, never stop:
    # a stopped instance keeps billing storage.
    if [ -n "${CONTAINER_API_KEY:-}" ] && [ -n "${CONTAINER_ID:-}" ]; then
        log "destroying Vast.ai instance $CONTAINER_ID"
        curl -fsS --max-time 30 --noproxy '*' \
            -X DELETE \
            -H "Authorization: Bearer ${CONTAINER_API_KEY}" \
            "https://console.vast.ai/api/v0/instances/${CONTAINER_ID}/" || \
            log "vast destroy call failed; falling back to poweroff"
    fi

    # AWS (and anything else with a host we can reach): ask the HOST to
    # terminate by disarming its deadline file, which is bind-mounted in.
    #
    # This exists because the obvious approach does not work. This container is
    # unprivileged — no CAP_SYS_BOOT — so `poweroff` fails, and the fallback
    # `kill -9 1` only kills the container, which `--restart=unless-stopped`
    # then restarts. The machine keeps billing and the agent keeps coming back.
    # The host watchdog polls this file every 30s and powers off on a 0.
    if [ -n "${KH_HOST_DEADLINE_FILE:-}" ] && [ -w "${KH_HOST_DEADLINE_FILE}" ]; then
        log "disarming host deadline ${KH_HOST_DEADLINE_FILE}; host watchdog will power off within 30s"
        echo 0 > "${KH_HOST_DEADLINE_FILE}"
        # Stop doing work while waiting to be terminated: a GPU that keeps
        # running for another half-minute is billed for that half-minute.
        pkill -9 hashcat 2>/dev/null
        sleep 90
    fi

    # Last resort. On a privileged host this terminates (instances are launched
    # with InstanceInitiatedShutdownBehavior=terminate); everywhere else it at
    # least stops the agent. --force twice skips a graceful shutdown that a
    # wedged GPU driver could stall indefinitely.
    poweroff --force --force 2>/dev/null || halt -f 2>/dev/null || kill -9 1
}

watchdog() {
    while true; do
        now=$(date +%s)

        if [ "${KH_DEADLINE_EPOCH}" -gt 0 ] && [ "$now" -ge "${KH_DEADLINE_EPOCH}" ]; then
            self_destruct "absolute deadline reached"
        fi

        if [ -r "$HEARTBEAT_FILE" ]; then
            last=$(cat "$HEARTBEAT_FILE" 2>/dev/null || echo "$now")
            if [ $((now - last)) -ge "${KH_HEARTBEAT_LOSS_TIMEOUT}" ]; then
                self_destruct "no contact with backend for ${KH_HEARTBEAT_LOSS_TIMEOUT}s"
            fi
        fi

        sleep "${KH_WATCHDOG_INTERVAL}"
    done
}
watchdog &
log "watchdog armed (deadline=${KH_DEADLINE_EPOCH}, heartbeat_timeout=${KH_HEARTBEAT_LOSS_TIMEOUT}s, interval=${KH_WATCHDOG_INTERVAL}s, heartbeat_file=${HEARTBEAT_FILE})"

# ---------------------------------------------------------------------------
# 2. VPN, userspace mode
# ---------------------------------------------------------------------------

SOCKS_PORT=1055
: "${KH_VPN_PROVIDER:=none}"

start_tailscale() {
    [ -n "${KH_VPN_AUTH_KEY:-}" ] || { log "FATAL: tailscale selected but KH_VPN_AUTH_KEY is empty"; return 1; }
    local login_args=()
    [ -n "${KH_VPN_LOGIN_SERVER:-}" ] && login_args+=(--login-server "${KH_VPN_LOGIN_SERVER}")

    # --tun=userspace-networking runs WireGuard and the TCP/IP stack in-process
    # (gVisor netstack): no tun device, no NET_ADMIN, no root.
    # --state=mem: keeps the node ephemeral even if the auth key were not.
    tailscaled --tun=userspace-networking --state=mem: \
        --socks5-server=127.0.0.1:${SOCKS_PORT} \
        --outbound-http-proxy-listen=127.0.0.1:${SOCKS_PORT} &

    local up_args=(--auth-key="${KH_VPN_AUTH_KEY}" --hostname="kh-${HOSTNAME}" --shields-up)
    [ -n "${KH_VPN_TAG:-}" ] && up_args+=(--advertise-tags="${KH_VPN_TAG}")
    [ ${#login_args[@]} -gt 0 ] && up_args+=("${login_args[@]}")

    for _ in $(seq 1 30); do
        if tailscale "${up_args[@]}" >/dev/null 2>&1; then
            log "tailscale connected"
            return 0
        fi
        sleep 2
    done
    return 1
}

start_netbird() {
    [ -n "${KH_VPN_AUTH_KEY:-}" ] || { log "FATAL: netbird selected but KH_VPN_AUTH_KEY is empty"; return 1; }
    # netstack mode is NetBird's userspace equivalent. Note it provides NO DNS,
    # so KH_HOST must be an overlay IP for NetBird deployments and the server
    # certificate needs a matching IP SAN.
    export NB_USE_NETSTACK_MODE=true
    export NB_SOCKS5_LISTENER_PORT=${SOCKS_PORT}
    local args=(--setup-key "${KH_VPN_AUTH_KEY}" --hostname "kh-${HOSTNAME}" -F)
    [ -n "${KH_VPN_LOGIN_SERVER:-}" ] && args+=(--management-url "${KH_VPN_LOGIN_SERVER}")
    netbird up "${args[@]}" &
    sleep 10
    log "netbird netstack started"
}

start_wireguard() {
    [ -n "${KH_VPN_CONFIG:-}" ] || { log "FATAL: wireguard selected but KH_VPN_CONFIG is empty"; return 1; }
    # wireproxy is userspace WireGuard exposing SOCKS5 — no tun, no NET_ADMIN.
    # wireguard-go and boringtun do NOT qualify: they still create a tun device.
    mkdir -p /etc/wireproxy
    printf '%s\n' "${KH_VPN_CONFIG}" > /etc/wireproxy/wireproxy.conf
    printf '\n[Socks5]\nBindAddress = 127.0.0.1:%s\n' "${SOCKS_PORT}" >> /etc/wireproxy/wireproxy.conf
    chmod 600 /etc/wireproxy/wireproxy.conf
    wireproxy -c /etc/wireproxy/wireproxy.conf &
    sleep 5
    log "wireproxy started"
}

case "${KH_VPN_PROVIDER}" in
    tailscale) start_tailscale || { log "FATAL: tailscale failed to connect"; self_destruct "VPN unavailable"; } ;;
    netbird)   start_netbird   || { log "FATAL: netbird failed";              self_destruct "VPN unavailable"; } ;;
    wireguard) start_wireguard || { log "FATAL: wireproxy failed";            self_destruct "VPN unavailable"; } ;;
    none)      log "no VPN configured; assuming direct reachability" ;;
    *)         log "FATAL: unknown KH_VPN_PROVIDER '${KH_VPN_PROVIDER}'"; self_destruct "bad VPN configuration" ;;
esac

# ---------------------------------------------------------------------------
# 3. Point the agent at the tunnel and run it
# ---------------------------------------------------------------------------
#
# HTTPS_PROXY, not ALL_PROXY: Go's net/http does not read ALL_PROXY, and a
# wss:// URL is rewritten to https before the proxy function is consulted, so
# HTTPS_PROXY is the variable that governs the WebSocket. Go treats socks5 as
# socks5h, sending the HOSTNAME to the proxy, which is what lets the VPN
# resolve the backend's private name for us.
if [ "${KH_VPN_PROVIDER}" != "none" ]; then
    export HTTPS_PROXY="socks5://127.0.0.1:${SOCKS_PORT}"
    export HTTP_PROXY="socks5://127.0.0.1:${SOCKS_PORT}"
    # localhost must bypass, and so must the provider control plane, or the
    # self-destruct call would be routed through a tunnel that may be dead.
    export NO_PROXY="127.0.0.1,localhost,${KH_NO_PROXY_EXTRA:-}"
    log "agent will use SOCKS5 proxy on 127.0.0.1:${SOCKS_PORT} (NO_PROXY=${NO_PROXY})"
fi

# Ephemeral mode: configuration from the environment, never a .env file. The
# claim code must not be written to a disk the host operator can read.
export KH_EPHEMERAL=true

log "starting agent"
exec /app/krakenhashes-agent "$@"
