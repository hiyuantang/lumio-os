#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-only
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
GO="$ROOT/.tools/go/bin/go"
export GOMODCACHE="$ROOT/.tools/gomodcache"
export GOCACHE="$ROOT/.tools/gocache"
export GOPATH="$ROOT/.tools/gopath"

IMAGE="lumio-os-integration:phase2"
CONTAINER="lumio-os-it"
PORT="${PORT:-18080}"
BASE="http://127.0.0.1:${PORT}"
WSURL="ws://127.0.0.1:${PORT}/api/v1/ws"
BUILD_DIR="$ROOT/docker/.build"
TARGETARCH="${TARGETARCH:-arm64}"

PASS=0
FAIL=0

ok()  { echo "PASS: $1"; PASS=$((PASS + 1)); }
bad() { echo "FAIL: $1"; FAIL=$((FAIL + 1)); }

expect_in() {
    local name="$1" url="$2" needle="$3" out
    if ! out="$(curl -fsS "$url" 2>/dev/null)"; then
        bad "$name (request failed)"
        return
    fi
    if grep -qF "$needle" <<<"$out"; then
        ok "$name"
    else
        echo "  expected to find: $needle"
        echo "  got: $(head -c 400 <<<"$out")"
        bad "$name"
    fi
}

expect_status_code() {
    local name="$1" method="$2" url="$3" want_code="$4" needle="$5"
    local body code
    body="$(curl -s -o /dev/stdout -w '\n%{http_code}' -X "$method" "$url" 2>/dev/null)"
    code="$(tail -n1 <<<"$body")"
    if [[ "$code" == "$want_code" ]] && grep -qF "$needle" <<<"$body"; then
        ok "$name"
    else
        echo "  wanted HTTP $want_code and $needle, got HTTP $code: $(head -c 300 <<<"$body")"
        bad "$name"
    fi
}

wait_for_log() {
    local file="$1" needle="$2" tries="${3:-20}" i
    for ((i = 0; i < tries; i++)); do
        grep -q "$needle" "$file" 2>/dev/null && return 0
        sleep 0.5
    done
    return 1
}

cleanup() {
    echo "== cleanup =="
    docker rm -f "$CONTAINER" >/dev/null 2>&1 || true
    docker rmi -f "$IMAGE" >/dev/null 2>&1 || true
    rm -rf "$BUILD_DIR"
}
trap cleanup EXIT

echo "== building lumiod (linux/$TARGETARCH) and wscheck (host) =="
mkdir -p "$BUILD_DIR/host"
(cd "$ROOT/server" && CGO_ENABLED=0 GOOS=linux GOARCH="$TARGETARCH" "$GO" build -o "$BUILD_DIR/lumiod" ./cmd/lumiod) || exit 1
(cd "$ROOT/server" && CGO_ENABLED=0 "$GO" build -o "$BUILD_DIR/host/wscheck" ./cmd/wscheck) || exit 1

echo "== building image $IMAGE =="
docker build -q -t "$IMAGE" -f "$ROOT/docker/Dockerfile.ubuntu24" "$ROOT/docker" >/dev/null || {
    echo "FAIL: docker build failed"
    exit 1
}

echo "== starting container $CONTAINER =="
docker rm -f "$CONTAINER" >/dev/null 2>&1 || true
docker run -d --name "$CONTAINER" \
    --privileged --cgroupns=host \
    -v /sys/fs/cgroup:/sys/fs/cgroup:rw \
    --tmpfs /run --tmpfs /run/lock \
    -p "$PORT:8080" \
    "$IMAGE" >/dev/null || {
    echo "FAIL: docker run failed"
    exit 1
}

echo "== waiting for lumiod to answer =="
healthy=0
for _ in $(seq 1 90); do
    if curl -fsS "$BASE/api/v1/meta/version" >/dev/null 2>&1; then
        healthy=1
        break
    fi
    sleep 1
done
if [[ "$healthy" != 1 ]]; then
    echo "FAIL: lumiod never became healthy; container logs:"
    docker logs "$CONTAINER" 2>&1 | tail -30
    exit 1
fi
ok "container healthy"

echo "== REST assertions =="
expect_in "meta/version"            "$BASE/api/v1/meta/version"    '"protocolVersions":[1]'
expect_in "identity is ubuntu"      "$BASE/api/v1/system/identity" '"id":"ubuntu"'
expect_in "identity is 24.04"       "$BASE/api/v1/system/identity" '"versionId":"24.04"'
expect_in "identity has kernel"     "$BASE/api/v1/system/identity" '"kernel":"'
expect_in "overview"                "$BASE/api/v1/system/overview" '"uptimeSeconds"'
expect_in "overview rebootRequired" "$BASE/api/v1/system/overview" '"rebootRequired":false'
expect_in "metrics sample"          "$BASE/api/v1/system/metrics"  '"cpu"'
expect_in "services has cron"       "$BASE/api/v1/services"        '"name":"cron.service"'
expect_in "services has enabled"    "$BASE/api/v1/services"        '"enabledState":"enabled"'
expect_in "journal entries"         "$BASE/api/v1/journal?limit=5" '"cursor"'
expect_in "journal nextCursor"      "$BASE/api/v1/journal?limit=5" '"nextCursor"'
expect_in "journal unit filter"     "$BASE/api/v1/journal?unit=cron.service&limit=5" '"ok":true'
SINCE="$(date -u -v-1H +%Y-%m-%dT%H:%M:%SZ 2>/dev/null || date -u -d '1 hour ago' +%Y-%m-%dT%H:%M:%SZ)"
expect_in "journal since filter"    "$BASE/api/v1/journal?since=${SINCE//:/%3A}&limit=5" '"ok":true'
expect_status_code "journal bad priority" GET "$BASE/api/v1/journal?priority=bogus" 400 '"code":"validation_failed"'
expect_in "files.list /etc"         "$BASE/api/v1/files/list?path=/etc" '"name":"hostname"'
expect_in "files.read /etc/hostname" "$BASE/api/v1/files/read?path=/etc/hostname" '"revision":"sha256:'
expect_status_code "files.read missing -> 404" GET "$BASE/api/v1/files/read?path=/no/such/file" 404 '"code":"not_found"'
expect_status_code "files.write -> unavailable" PUT "$BASE/api/v1/files/write" 503 '"code":"unavailable"'
expect_status_code "services.action -> unavailable" POST "$BASE/api/v1/services/action" 503 '"code":"unavailable"'
expect_status_code "updates.apply -> unavailable" POST "$BASE/api/v1/updates/apply" 503 '"code":"unavailable"'
expect_status_code "unknown route -> 404" GET "$BASE/api/v1/nope" 404 '"code":"not_found"'

echo "== WebSocket assertions =="
if "$BUILD_DIR/host/wscheck" -url "$WSURL" -mode metrics -timeout 15s >"$BUILD_DIR/metrics.log" 2>&1; then
    ok "ws system.metrics tick"
else
    cat "$BUILD_DIR/metrics.log"
    bad "ws system.metrics tick"
fi

"$BUILD_DIR/host/wscheck" -url "$WSURL" -mode journal -unit "" -match "lumio-integration-marker" -timeout 25s \
    >"$BUILD_DIR/journal.log" 2>&1 &
JPID=$!
if wait_for_log "$BUILD_DIR/journal.log" "subscribed" 20; then
    sleep 1
    for _ in 1 2 3 4 5; do
        docker exec "$CONTAINER" systemd-cat -t lumio-it echo "lumio-integration-marker" >/dev/null 2>&1
        sleep 1
    done
else
    kill "$JPID" 2>/dev/null || true
fi
if wait "$JPID"; then
    ok "ws journal.stream entry"
else
    cat "$BUILD_DIR/journal.log"
    bad "ws journal.stream entry"
fi

echo "== EXIT GATE: services.subscribe sees systemctl stop cron =="
"$BUILD_DIR/host/wscheck" -url "$WSURL" -mode services -unit cron.service -expect inactive -timeout 15s \
    >"$BUILD_DIR/services.log" 2>&1 &
SPID=$!
if ! wait_for_log "$BUILD_DIR/services.log" "snapshot" 30; then
    echo "  services log so far:"
    cat "$BUILD_DIR/services.log"
    kill "$SPID" 2>/dev/null || true
fi
docker exec "$CONTAINER" systemctl stop cron >/dev/null 2>&1
gate=0
wait "$SPID" || gate=$?
if [[ "$gate" == 0 ]]; then
    ok "EXIT GATE: changed event for cron.service observed"
else
    echo "  services log:"
    cat "$BUILD_DIR/services.log"
    bad "EXIT GATE: changed event for cron.service observed"
fi

echo
echo "======================================"
echo "integration summary: $PASS passed, $FAIL failed"
if [[ "$FAIL" == 0 ]]; then
    echo "ALL CHECKS PASSED"
    exit 0
fi
echo "INTEGRATION FAILURES"
exit 1
