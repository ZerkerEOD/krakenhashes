#!/usr/bin/env bash
#
# Build the documentation site locally, exactly as CI does, so a broken link or
# a bad config is caught before it is pushed.
#
# CI installs requirements-docs.txt onto a clean runner and runs mkdocs. That is
# not reproducible by hand on a modern distro: Arch, Debian 12+, Fedora and
# Ubuntu 23.10+ all ship PEP 668 "externally managed" Pythons, so
# `pip install -r requirements-docs.txt` is refused outright. This script owns a
# virtualenv instead, so there is nothing to remember and nothing to break.
#
# USAGE
#   scripts/docs-build.sh              # strict build; this is the pre-push check
#   scripts/docs-build.sh serve        # live-reloading preview on :8000
#   scripts/docs-build.sh clean        # delete the venv and the built site
#
# The venv lives OUTSIDE the repository (see KH_DOCS_VENV below) so it cannot be
# committed, cannot be wiped by `git clean -xdf`, and does not need a .gitignore
# entry to stay invisible. The built site DOES land in ./site, which .gitignore
# covers.
#
# Requirements are re-installed only when requirements-docs.txt actually
# changes, tracked by a hash stamp inside the venv. A normal run is a no-op
# setup plus a ~10 second build.
#
# ENVIRONMENT
#   KH_DOCS_VENV   where the virtualenv lives
#                  (default: ~/.local/share/venvs/krakenhashes-docs)
#   KH_DOCS_PORT   port for `serve` (default: 8000)
#
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
VENV="${KH_DOCS_VENV:-$HOME/.local/share/venvs/krakenhashes-docs}"
PORT="${KH_DOCS_PORT:-8000}"
REQS="$REPO_ROOT/requirements-docs.txt"
STAMP="$VENV/.requirements-sha256"
CMD="${1:-build}"

log() { printf '\033[36m[docs]\033[0m %s\n' "$*"; }
die() { printf '\033[31m[docs] %s\033[0m\n' "$*" >&2; exit 1; }

# Validate the argument BEFORE doing any setup. Otherwise a typo spends a
# minute building a virtualenv and installing dependencies only to then refuse
# the command, which reads like the tool is broken rather than the input.
case "$CMD" in
build | serve | clean) ;;
*) die "unknown command '$CMD' (expected: build, serve, clean)" ;;
esac

if [ "$CMD" = "clean" ]; then
    log "removing $VENV"
    rm -rf "$VENV"
    log "removing $REPO_ROOT/site"
    rm -rf "$REPO_ROOT/site"
    log "clean"
    exit 0
fi

[ -f "$REQS" ] || die "requirements-docs.txt not found at $REQS"

# ---------------------------------------------------------------------------
# 1. The virtualenv
# ---------------------------------------------------------------------------
if [ ! -x "$VENV/bin/python" ]; then
    log "creating virtualenv at $VENV"
    command -v python3 >/dev/null || die "python3 not found"
    # Debian/Ubuntu split venv into a separate package and the failure message
    # is unhelpful, so say what to install rather than letting it fall through.
    python3 -m venv "$VENV" 2>/dev/null || die \
        "could not create a virtualenv. On Debian/Ubuntu: apt install python3-venv"
fi

# ---------------------------------------------------------------------------
# 2. Dependencies, installed only when the requirements file changed
# ---------------------------------------------------------------------------
WANT="$(sha256sum "$REQS" | cut -d' ' -f1)"
HAVE="$(cat "$STAMP" 2>/dev/null || true)"

if [ "$WANT" != "$HAVE" ]; then
    log "installing documentation requirements (this runs once per change)"
    "$VENV/bin/pip" install --quiet --upgrade pip
    "$VENV/bin/pip" install --quiet -r "$REQS" || die "pip install failed"
    printf '%s' "$WANT" > "$STAMP"
    log "requirements installed"
fi

cd "$REPO_ROOT"

# ---------------------------------------------------------------------------
# 3. Build or serve
# ---------------------------------------------------------------------------
case "$CMD" in
build)
    # --strict turns mkdocs WARNINGs into a non-zero exit, which is what makes
    # this a gate rather than a report. Note that unresolved link ANCHORS are
    # logged at INFO and so do NOT fail the build -- mkdocs only warns about
    # links whose target FILE is missing. The repo carries a number of
    # pre-existing INFO anchor complaints; a new one will not break this, so
    # read the output as well as the exit code when you add cross-references.
    log "building (strict)"
    "$VENV/bin/mkdocs" build --strict
    log "OK -- site written to $REPO_ROOT/site"
    log "open it with: scripts/docs-build.sh serve"
    ;;
serve)
    log "serving on http://127.0.0.1:$PORT (ctrl-c to stop)"
    "$VENV/bin/mkdocs" serve --dev-addr "127.0.0.1:$PORT"
    ;;
esac
