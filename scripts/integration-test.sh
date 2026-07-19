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
    shift 5
    local body code
    body="$(curl -s -o /dev/stdout -w '\n%{http_code}' -X "$method" -H 'Content-Type: application/json' "$@" "$url" 2>/dev/null)"
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
expect_in "identity user is lumio"  "$BASE/api/v1/system/identity" '"user":{"name":"lumio"'
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

echo "== Phase 3: terminal over WebSocket =="
if "$BUILD_DIR/host/wscheck" -url "$WSURL" -mode terminal -cmd 'whoami\n' -match "lumio" -timeout 20s \
    >"$BUILD_DIR/term1.log" 2>&1; then
    ok "terminal whoami=lumio (PTY runs as service UID) + resize + exit"
else
    cat "$BUILD_DIR/term1.log"
    bad "terminal whoami=lumio"
fi
if "$BUILD_DIR/host/wscheck" -url "$WSURL" -mode terminal -cmd 'echo answer=$((40+2))\n' -match "answer=42" -timeout 20s \
    >"$BUILD_DIR/term2.log" 2>&1; then
    ok "terminal shell arithmetic (answer=42)"
else
    cat "$BUILD_DIR/term2.log"
    bad "terminal shell arithmetic"
fi
if "$BUILD_DIR/host/wscheck" -url "$WSURL" -mode terminal-reattach -cmd 'echo reattach-ok\n' -match "reattach-ok" -timeout 20s \
    >"$BUILD_DIR/term3.log" 2>&1; then
    ok "terminal reattach replays scrollback"
else
    cat "$BUILD_DIR/term3.log"
    bad "terminal reattach replays scrollback"
fi

echo "== Phase 3: files.write =="
json_field() { grep -o "\"$2\":\"[^\"]*\"" <<<"$1" | head -1 | cut -d'"' -f4; }
b64() { printf %s "$1" | base64; }

W1="$(curl -s -X PUT -H 'Content-Type: application/json' \
    -d "{\"path\":\"/home/lumio/x.txt\",\"content\":\"$(b64 hello-v1)\",\"requestId\":\"it-w1\"}" \
    "$BASE/api/v1/files/write")"
REV1="$(json_field "$W1" revision)"
if [[ -n "$REV1" ]] && grep -q '"ok":true' <<<"$W1"; then
    ok "files.write create (revision $REV1)"
else
    echo "  got: $W1"
    bad "files.write create"
fi

R1="$(curl -s "$BASE/api/v1/files/read?path=/home/lumio/x.txt")"
if [[ "$(json_field "$R1" revision)" == "$REV1" ]] && grep -q "$(b64 hello-v1)" <<<"$R1"; then
    ok "files.write read-back matches"
else
    echo "  got: $R1"
    bad "files.write read-back matches"
fi

W2="$(curl -s -X PUT -H 'Content-Type: application/json' \
    -d "{\"path\":\"/home/lumio/x.txt\",\"content\":\"$(b64 hello-v2)\",\"expectedRevision\":\"$REV1\",\"requestId\":\"it-w2\"}" \
    "$BASE/api/v1/files/write")"
REV2="$(json_field "$W2" revision)"
if [[ -n "$REV2" && "$REV2" != "$REV1" ]]; then
    ok "files.write with expectedRevision (revision $REV2)"
else
    echo "  got: $W2"
    bad "files.write with expectedRevision"
fi

R2="$(curl -s "$BASE/api/v1/files/read?path=/home/lumio/x.txt")"
if [[ "$(json_field "$R2" revision)" == "$REV2" ]] && grep -q "$(b64 hello-v2)" <<<"$R2"; then
    ok "files.write v2 read-back matches"
else
    echo "  got: $R2"
    bad "files.write v2 read-back matches"
fi

expect_status_code "files.write stale revision -> 409" PUT \
    "$BASE/api/v1/files/write" 409 '"code":"stale_revision"' \
    -d "{\"path\":\"/home/lumio/x.txt\",\"content\":\"$(b64 hello-v3)\",\"expectedRevision\":\"$REV1\",\"requestId\":\"it-w3\"}"

REPLAY="$(curl -s -D - -o /dev/null -X PUT -H 'Content-Type: application/json' \
    -d "{\"path\":\"/home/lumio/x.txt\",\"content\":\"$(b64 hello-v2)\",\"expectedRevision\":\"$REV1\",\"requestId\":\"it-w2\"}" \
    "$BASE/api/v1/files/write")"
R3="$(curl -s "$BASE/api/v1/files/read?path=/home/lumio/x.txt")"
if grep -qi '^X-Lumio-Idempotent-Replay: true' <<<"$REPLAY" && grep -q "$(b64 hello-v2)" <<<"$R3"; then
    ok "files.write idempotent replay (no second mutation)"
else
    echo "  replay headers: $REPLAY"
    bad "files.write idempotent replay"
fi

docker exec "$CONTAINER" chmod 600 /home/lumio/x.txt >/dev/null 2>&1
W4="$(curl -s -X PUT -H 'Content-Type: application/json' \
    -d "{\"path\":\"/home/lumio/x.txt\",\"content\":\"$(b64 hello-v4)\",\"expectedRevision\":\"$REV2\",\"requestId\":\"it-w4\"}" \
    "$BASE/api/v1/files/write")"
L4="$(curl -s "$BASE/api/v1/files/list?path=/home/lumio")"
if grep -q '"ok":true' <<<"$W4" && grep -q '"name":"x.txt"[^}]*"mode":"0600"' <<<"$L4"; then
    ok "files.write preserves mode 0600"
else
    echo "  write: $W4"
    echo "  list: $(grep -o '"name":"x.txt"[^}]*}' <<<"$L4")"
    bad "files.write preserves mode"
fi

echo "== Phase 3: write traversal and permissions =="
expect_status_code "files.write /etc/shadow -> forbidden" PUT \
    "$BASE/api/v1/files/write" 403 '"code":"forbidden"' \
    -d "{\"path\":\"/etc/shadow\",\"content\":\"$(b64 x)\",\"requestId\":\"it-p1\"}"
expect_status_code "files.write traversal escape -> forbidden" PUT \
    "$BASE/api/v1/files/write" 403 '"code":"forbidden"' \
    -d "{\"path\":\"/home/lumio/../../etc/hostname\",\"content\":\"$(b64 x)\",\"requestId\":\"it-p2\"}"
expect_status_code "files.write relative path -> validation_failed" PUT \
    "$BASE/api/v1/files/write" 400 '"code":"validation_failed"' \
    -d '{"path":"tmp/x.txt","content":"eA==","requestId":"it-p3"}'

echo "== Phase 3: files.delete (trash) =="
docker exec "$CONTAINER" sh -c 'echo trashme > /home/lumio/trashme.txt' >/dev/null 2>&1
D1="$(curl -s -X POST -H 'Content-Type: application/json' \
    -d '{"path":"/home/lumio/trashme.txt","requestId":"it-d1"}' \
    "$BASE/api/v1/files/delete")"
if grep -q '"trashed":true' <<<"$D1" \
    && docker exec "$CONTAINER" test -f /home/lumio/.local/share/Trash/files/trashme.txt \
    && docker exec "$CONTAINER" test -f /home/lumio/.local/share/Trash/info/trashme.txt.trashinfo; then
    ok "files.delete moves to freedesktop trash"
else
    echo "  got: $D1"
    bad "files.delete moves to freedesktop trash"
fi
expect_status_code "files.delete /etc/hostname -> rejected" POST \
    "$BASE/api/v1/files/delete" 403 '"code":"forbidden"' \
    -d '{"path":"/etc/hostname","requestId":"it-d2"}'
expect_status_code "files.delete missing requestId -> 400" POST \
    "$BASE/api/v1/files/delete" 400 '"code":"validation_failed"' \
    -d '{"path":"/home/lumio/x.txt"}'

echo
echo "======================================"
echo "integration summary: $PASS passed, $FAIL failed"
if [[ "$FAIL" == 0 ]]; then
    echo "ALL CHECKS PASSED"
    exit 0
fi
echo "INTEGRATION FAILURES"
exit 1
