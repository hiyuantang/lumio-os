#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-only
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
GO="$ROOT/.tools/go/bin/go"
export GOMODCACHE="$ROOT/.tools/gomodcache"
export GOCACHE="$ROOT/.tools/gocache"
export GOPATH="$ROOT/.tools/gopath"

IMAGE="lumio-os-integration:phase5"
CONTAINER="lumio-os-it"
PORT="${PORT:-18080}"
BASE="http://127.0.0.1:${PORT}"
WSURL="ws://127.0.0.1:${PORT}/api/v1/ws"
BUILD_DIR="$ROOT/docker/.build"

PASS=0
FAIL=0

ok()  { echo "PASS: $1"; PASS=$((PASS + 1)); }
bad() { echo "FAIL: $1"; FAIL=$((FAIL + 1)); }

COOKIE_JAR="$BUILD_DIR/cookies.txt"
CSRF=""
SESSION=""

json_field() { grep -o "\"$2\":\"[^\"]*\"" <<<"$1" | head -1 | cut -d'"' -f4; }
b64() { printf %s "$1" | base64; }

expect_in() {
    local name="$1" url="$2" needle="$3" out
    if ! out="$(curl -fsS -b "$COOKIE_JAR" "$url" 2>/dev/null)"; then
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
    body="$(curl -s -o /dev/stdout -w '\n%{http_code}' -X "$method" \
        -b "$COOKIE_JAR" -H "X-Lumio-CSRF: $CSRF" -H 'Content-Type: application/json' "$@" "$url" 2>/dev/null)"
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

audit_query() {
    docker exec "$CONTAINER" sqlite3 /var/lib/lumio/audit.db "$1" 2>/dev/null
}

cleanup() {
    echo "== cleanup =="
    docker rm -f "$CONTAINER" >/dev/null 2>&1 || true
    docker rmi -f "$IMAGE" >/dev/null 2>&1 || true
    rm -rf "$BUILD_DIR"
}
trap cleanup EXIT

echo "== building wscheck (host) =="
mkdir -p "$BUILD_DIR/host"
(cd "$ROOT/server" && CGO_ENABLED=0 "$GO" build -o "$BUILD_DIR/host/wscheck" ./cmd/wscheck) || exit 1

echo "== building image $IMAGE (compiles lumiod with PAM inside) =="
docker build -q -t "$IMAGE" -f "$ROOT/docker/Dockerfile.ubuntu24" "$ROOT" >/dev/null || {
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

echo "== waiting for the gateway to answer =="
healthy=0
for _ in $(seq 1 120); do
    if curl -fsS "$BASE/api/v1/meta/version" >/dev/null 2>&1; then
        healthy=1
        break
    fi
    sleep 1
done
if [[ "$healthy" != 1 ]]; then
    echo "FAIL: gateway never became healthy; container logs:"
    docker logs "$CONTAINER" 2>&1 | tail -30
    exit 1
fi
ok "container healthy"

echo "== Phase 4 gate 1: authentication =="
CODE="$(curl -s -o /dev/null -w '%{http_code}' "$BASE/api/v1/services")"
if [[ "$CODE" == 401 ]]; then ok "GET /services without session -> 401"; else bad "GET /services without session -> 401 (got $CODE)"; fi

BAD_LOGIN="$(curl -s -o /dev/null -w '%{http_code}' -X POST -H 'Content-Type: application/json' \
    -d '{"username":"alice","password":"wrong"}' "$BASE/api/v1/auth/login")"
if [[ "$BAD_LOGIN" == 401 ]]; then ok "login wrong password -> 401"; else bad "login wrong password -> 401 (got $BAD_LOGIN)"; fi

: > "$COOKIE_JAR"
LOGIN="$(curl -s -c "$COOKIE_JAR" -X POST -H 'Content-Type: application/json' \
    -d '{"username":"alice","password":"alice-pass"}' "$BASE/api/v1/auth/login")"
CSRF="$(json_field "$LOGIN" csrf)"
SESSION="$(awk '$6 == "lumio_session" {print $NF}' "$COOKIE_JAR" | tail -1)"
if [[ -n "$CSRF" && -n "$SESSION" ]] && grep -q '"ok":true' <<<"$LOGIN" && grep -q '"name":"alice"' <<<"$LOGIN"; then
    ok "login ok -> cookies + csrf"
else
    echo "  got: $LOGIN"
    bad "login ok -> cookies + csrf"
fi
if [[ -z "$CSRF" || -z "$SESSION" ]]; then
    echo "FATAL: cannot continue without a session"
    exit 1
fi

expect_in "GET /services with session -> 200" "$BASE/api/v1/services" '"name":"cron.service"'
expect_in "identity user is alice" "$BASE/api/v1/system/identity" '"user":{"name":"alice"'

echo "== Phase 4 gate 2: CSRF =="
CODE="$(curl -s -o /dev/null -w '%{http_code}' -X POST -b "$COOKIE_JAR" -H 'Content-Type: application/json' \
    -d '{"path":"/home/alice/nope.txt","requestId":"it-csrf"}' "$BASE/api/v1/files/delete")"
if [[ "$CODE" == 403 ]]; then ok "non-GET without X-Lumio-CSRF -> 403"; else bad "non-GET without X-Lumio-CSRF -> 403 (got $CODE)"; fi
CODE="$(curl -s -o /dev/null -w '%{http_code}' -X POST -b "$COOKIE_JAR" -H "X-Lumio-CSRF: $CSRF" -H 'Content-Type: application/json' \
    -d '{"path":"/home/alice/nope.txt","requestId":"it-csrf"}' "$BASE/api/v1/files/delete")"
if [[ "$CODE" != 403 ]]; then ok "non-GET with X-Lumio-CSRF -> not 403 (got $CODE)"; else bad "non-GET with X-Lumio-CSRF still 403"; fi

echo "== REST assertions (authenticated) =="
expect_in "meta/version"            "$BASE/api/v1/meta/version"    '"protocolVersions":[1]'
expect_in "identity is ubuntu"      "$BASE/api/v1/system/identity" '"id":"ubuntu"'
expect_in "identity is 24.04"       "$BASE/api/v1/system/identity" '"versionId":"24.04"'
expect_in "identity has kernel"     "$BASE/api/v1/system/identity" '"kernel":"'
expect_in "overview"                "$BASE/api/v1/system/overview" '"uptimeSeconds"'
expect_in "metrics sample"          "$BASE/api/v1/system/metrics"  '"cpu"'
expect_in "services has cron"       "$BASE/api/v1/services"        '"name":"cron.service"'
expect_in "services has enabled"    "$BASE/api/v1/services"        '"enabledState":"enabled"'
expect_in "service detail has unit file" "$BASE/api/v1/services/detail?name=cron.service" '"content":"[Unit]'
expect_status_code "service detail rejects bad unit" GET "$BASE/api/v1/services/detail?name=../../etc/passwd" 400 '"code":"validation_failed"'
expect_in "journal entries"         "$BASE/api/v1/journal?limit=5" '"cursor"'
expect_in "journal nextCursor"      "$BASE/api/v1/journal?limit=5" '"nextCursor"'
expect_in "journal unit filter"     "$BASE/api/v1/journal?unit=cron.service&limit=5" '"ok":true'
expect_in "journal current boot filter" "$BASE/api/v1/journal?boot=current&limit=5" '"ok":true'
expect_in "journal previous boot filter" "$BASE/api/v1/journal?boot=previous&limit=5" '"entries":[]'
SINCE="$(date -u -v-1H +%Y-%m-%dT%H:%M:%SZ 2>/dev/null || date -u -d '1 hour ago' +%Y-%m-%dT%H:%M:%SZ)"
expect_in "journal since filter"    "$BASE/api/v1/journal?since=${SINCE//:/%3A}&limit=5" '"ok":true'
expect_status_code "journal bad priority" GET "$BASE/api/v1/journal?priority=bogus" 400 '"code":"validation_failed"'
expect_in "files.list /etc"         "$BASE/api/v1/files/list?path=/etc" '"name":"hostname"'
expect_in "files.read /etc/hostname" "$BASE/api/v1/files/read?path=/etc/hostname" '"revision":"sha256:'
expect_status_code "files.read missing -> 404" GET "$BASE/api/v1/files/read?path=/no/such/file" 404 '"code":"not_found"'
expect_status_code "unknown route -> 404" GET "$BASE/api/v1/nope" 404 '"code":"not_found"'

echo "== Phase 5 Updates: saved plan + progress stream =="
WSAUTH=(-cookie "lumio_session=$SESSION" -csrf "$CSRF")
UPDATE_PLAN="$(curl -s -b "$COOKIE_JAR" -H "X-Lumio-CSRF: $CSRF" -H 'Content-Type: application/json' -X POST \
    -d '{"requestId":"it-up-plan"}' "$BASE/api/v1/updates/plan")"
PLAN_ID="$(json_field "$UPDATE_PLAN" id)"
if [[ "$PLAN_ID" == pln_* ]] && grep -q '"packages":\[' <<<"$UPDATE_PLAN" && grep -q '"securityCount":' <<<"$UPDATE_PLAN"; then
    ok "updates.plan returns a saved package plan"
else
    echo "  got: $UPDATE_PLAN"
    bad "updates.plan returns a saved package plan"
fi
UPDATE_APPLY="$(curl -s -b "$COOKIE_JAR" -H "X-Lumio-CSRF: $CSRF" -H 'Content-Type: application/json' -X POST \
    -d "{\"requestId\":\"it-up-apply\",\"planId\":\"$PLAN_ID\"}" "$BASE/api/v1/updates/apply")"
if grep -q '"requestId":"it-up-apply"' <<<"$UPDATE_APPLY"; then
    ok "updates.apply accepts the exact saved plan"
else
    echo "  got: $UPDATE_APPLY"
    bad "updates.apply accepts the exact saved plan"
fi
if "$BUILD_DIR/host/wscheck" "${WSAUTH[@]}" -url "$WSURL" -mode updates -match it-up-apply -timeout 20s \
    >"$BUILD_DIR/updates.log" 2>&1; then
    ok "ws updates.progress reaches successful completion"
else
    cat "$BUILD_DIR/updates.log"
    bad "ws updates.progress reaches successful completion"
fi
UPDATE_AUDIT="$(audit_query "SELECT kind || '|' || outcome FROM audit WHERE request_id='it-up-apply' ORDER BY id")"
if grep -q 'begin|pending' <<<"$UPDATE_AUDIT" && grep -q 'end|success' <<<"$UPDATE_AUDIT"; then
    ok "packages.applyPlan audit has begin+end"
else
    echo "  audit: $UPDATE_AUDIT"
    bad "packages.applyPlan audit has begin+end"
fi

echo "== WebSocket assertions (authenticated) =="
if "$BUILD_DIR/host/wscheck" "${WSAUTH[@]}" -url "$WSURL" -mode metrics -timeout 15s >"$BUILD_DIR/metrics.log" 2>&1; then
    ok "ws system.metrics tick"
else
    cat "$BUILD_DIR/metrics.log"
    bad "ws system.metrics tick"
fi

"$BUILD_DIR/host/wscheck" "${WSAUTH[@]}" -url "$WSURL" -mode journal -unit "" -match "lumio-integration-marker" -timeout 25s \
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
"$BUILD_DIR/host/wscheck" "${WSAUTH[@]}" -url "$WSURL" -mode services -unit cron.service -expect inactive -timeout 15s \
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

echo "== Phase 4 gate 3: terminal runs as the logged-in user =="
if "$BUILD_DIR/host/wscheck" "${WSAUTH[@]}" -url "$WSURL" -mode terminal -cmd 'whoami\n' -match "alice" -timeout 20s \
    >"$BUILD_DIR/term1.log" 2>&1; then
    ok "terminal whoami=alice (per-user agent) + resize + exit"
else
    cat "$BUILD_DIR/term1.log"
    bad "terminal whoami=alice"
fi
if "$BUILD_DIR/host/wscheck" "${WSAUTH[@]}" -url "$WSURL" -mode terminal-reattach -cmd 'echo reattach-ok\n' -match "reattach-ok" -timeout 20s \
    >"$BUILD_DIR/term3.log" 2>&1; then
    ok "terminal reattach replays scrollback"
else
    cat "$BUILD_DIR/term3.log"
    bad "terminal reattach replays scrollback"
fi

echo "== Phase 4 gate 4: files.write permissions =="
W1="$(curl -s -b "$COOKIE_JAR" -H "X-Lumio-CSRF: $CSRF" -H 'Content-Type: application/json' -X PUT \
    -d "{\"path\":\"/home/alice/x.txt\",\"content\":\"$(b64 hello-v1)\",\"requestId\":\"it-w1\"}" \
    "$BASE/api/v1/files/write")"
REV1="$(json_field "$W1" revision)"
if [[ -n "$REV1" ]] && grep -q '"ok":true' <<<"$W1"; then
    ok "files.write /home/alice ok"
else
    echo "  got: $W1"
    bad "files.write /home/alice ok"
fi
R1="$(curl -s -b "$COOKIE_JAR" "$BASE/api/v1/files/read?path=/home/alice/x.txt")"
if [[ "$(json_field "$R1" revision)" == "$REV1" ]] && grep -q "$(b64 hello-v1)" <<<"$R1"; then
    ok "files.write read-back matches"
else
    echo "  got: $R1"
    bad "files.write read-back matches"
fi
expect_status_code "files.write stale revision -> 409" PUT \
    "$BASE/api/v1/files/write" 409 '"code":"stale_revision"' \
    -d "{\"path\":\"/home/alice/x.txt\",\"content\":\"$(b64 hello-v3)\",\"expectedRevision\":\"sha256:deadbeef\",\"requestId\":\"it-w3\"}"
expect_status_code "files.write /etc/shadow -> forbidden" PUT \
    "$BASE/api/v1/files/write" 403 '"code":"forbidden"' \
    -d "{\"path\":\"/etc/shadow\",\"content\":\"$(b64 x)\",\"requestId\":\"it-p1\"}"
expect_status_code "files.write traversal escape -> forbidden" PUT \
    "$BASE/api/v1/files/write" 403 '"code":"forbidden"' \
    -d "{\"path\":\"/home/alice/../../etc/hostname\",\"content\":\"$(b64 x)\",\"requestId\":\"it-p2\"}"

docker exec "$CONTAINER" sh -c 'echo trashme > /home/alice/trashme.txt' >/dev/null 2>&1
D1="$(curl -s -b "$COOKIE_JAR" -H "X-Lumio-CSRF: $CSRF" -H 'Content-Type: application/json' -X POST \
    -d '{"path":"/home/alice/trashme.txt","requestId":"it-d1"}' \
    "$BASE/api/v1/files/delete")"
if grep -q '"trashed":true' <<<"$D1" \
    && docker exec "$CONTAINER" test -f /home/alice/.local/share/Trash/files/trashme.txt \
    && docker exec "$CONTAINER" test -f /home/alice/.local/share/Trash/info/trashme.txt.trashinfo; then
    ok "files.delete moves to freedesktop trash"
else
    echo "  got: $D1"
    bad "files.delete moves to freedesktop trash"
fi
expect_status_code "files.delete /etc/hostname -> rejected" POST \
    "$BASE/api/v1/files/delete" 403 '"code":"forbidden"' \
    -d '{"path":"/etc/hostname","requestId":"it-d2"}'

echo "== Phase 4 gates 5-7: services.action through the broker =="
docker exec "$CONTAINER" systemctl start cron >/dev/null 2>&1
sleep 1
PID1="$(docker exec "$CONTAINER" systemctl show cron.service -p MainPID --value)"
A1="$(curl -s -b "$COOKIE_JAR" -H "X-Lumio-CSRF: $CSRF" -H 'Content-Type: application/json' -X POST \
    -d '{"requestId":"it-a1","action":"restart","unit":"cron.service"}' \
    "$BASE/api/v1/services/action")"
sleep 1
PID2="$(docker exec "$CONTAINER" systemctl show cron.service -p MainPID --value)"
AUDIT1="$(audit_query "SELECT kind || '|' || outcome FROM audit WHERE request_id='it-a1' ORDER BY id")"
if grep -q '"ok":true' <<<"$A1" && [[ -n "$PID1" && -n "$PID2" && "$PID1" != "$PID2" && "$PID2" != "0" ]] \
    && grep -q 'begin|pending' <<<"$AUDIT1" && grep -q 'end|success' <<<"$AUDIT1"; then
    ok "services.action restart cron -> 200, real restart, audit begin+end"
else
    echo "  action: $A1"
    echo "  pids: $PID1 -> $PID2"
    echo "  audit: $AUDIT1"
    bad "services.action restart cron"
fi

REPLAY="$(curl -s -D - -o /dev/null -b "$COOKIE_JAR" -H "X-Lumio-CSRF: $CSRF" -H 'Content-Type: application/json' -X POST \
    -d '{"requestId":"it-a1","action":"restart","unit":"cron.service"}' \
    "$BASE/api/v1/services/action")"
BEGINS="$(audit_query "SELECT count(*) FROM audit WHERE request_id='it-a1' AND kind='begin'")"
if grep -qi '^X-Lumio-Idempotent-Replay: true' <<<"$REPLAY" && [[ "$BEGINS" == 1 ]]; then
    ok "services.action idempotent replay (one begin row)"
else
    echo "  replay headers: $REPLAY"
    echo "  begin rows: $BEGINS"
    bad "services.action idempotent replay"
fi

expect_status_code "services.action expected mismatch -> 409" POST \
    "$BASE/api/v1/services/action" 409 '"code":"conflict"' \
    -d '{"requestId":"it-a3","action":"restart","unit":"cron.service","expected":{"activeState":"inactive"}}'
CONFLICT_SUCCESS="$(audit_query "SELECT count(*) FROM audit WHERE request_id='it-a3' AND outcome='success'")"
if [[ "$CONFLICT_SUCCESS" == 0 ]]; then
    ok "services.action conflict executed nothing (no success row)"
else
    bad "services.action conflict executed nothing"
fi

echo "== Phase 4 gate 8: injection and unknown actions =="
expect_status_code "services.action injection action -> 400" POST \
    "$BASE/api/v1/services/action" 400 '"code":"validation_failed"' \
    -d '{"requestId":"it-n1","action":"restart; rm -rf /","unit":"cron.service"}'
expect_status_code "services.action traversal unit -> 400" POST \
    "$BASE/api/v1/services/action" 400 '"code":"validation_failed"' \
    -d '{"requestId":"it-n2","action":"restart","unit":"../../etc"}'
expect_status_code "services.action unknown action -> 400" POST \
    "$BASE/api/v1/services/action" 400 '"code":"validation_failed"' \
    -d '{"requestId":"it-n3","action":"runRootCommand","unit":"cron.service"}'
NOEXEC="$(audit_query "SELECT count(*) FROM audit WHERE request_id IN ('it-n1','it-n2','it-n3') AND outcome='success'")"
if [[ "$NOEXEC" == 0 ]]; then
    ok "injection attempts executed nothing"
else
    bad "injection attempts executed nothing"
fi

echo "== Phase 4 gate 9: reauthentication path =="
REAUTH_NEED="$(curl -s -b "$COOKIE_JAR" -H "X-Lumio-CSRF: $CSRF" -H 'Content-Type: application/json' -X POST \
    -d '{"requestId":"it-r1","action":"restart","unit":"ssh.service"}' \
    "$BASE/api/v1/services/action")"
if grep -q '"reauthRequired":true' <<<"$REAUTH_NEED"; then
    ok "auth_admin unit -> 403 reauthRequired"
else
    echo "  got: $REAUTH_NEED"
    bad "auth_admin unit -> 403 reauthRequired"
fi
BAD_REAUTH="$(curl -s -o /dev/null -w '%{http_code}' -b "$COOKIE_JAR" -H "X-Lumio-CSRF: $CSRF" -H 'Content-Type: application/json' -X POST \
    -d '{"password":"wrong"}' "$BASE/api/v1/auth/reauth")"
if [[ "$BAD_REAUTH" == 401 ]]; then ok "reauth wrong password -> 401"; else bad "reauth wrong password -> 401 (got $BAD_REAUTH)"; fi
GOOD_REAUTH="$(curl -s -b "$COOKIE_JAR" -H "X-Lumio-CSRF: $CSRF" -H 'Content-Type: application/json' -X POST \
    -d '{"password":"alice-pass"}' "$BASE/api/v1/auth/reauth")"
if grep -q '"reauthenticatedUntil"' <<<"$GOOD_REAUTH"; then
    ok "reauth correct password -> 200"
else
    echo "  got: $GOOD_REAUTH"
    bad "reauth correct password -> 200"
fi
R2="$(curl -s -b "$COOKIE_JAR" -H "X-Lumio-CSRF: $CSRF" -H 'Content-Type: application/json' -X POST \
    -d '{"requestId":"it-r2","action":"restart","unit":"ssh.service"}' \
    "$BASE/api/v1/services/action")"
if grep -q '"ok":true' <<<"$R2"; then
    ok "action within reauth window -> 200"
else
    echo "  got: $R2"
    bad "action within reauth window -> 200"
fi

echo "== Phase 4 gate 10: polkit denial is audited =="
DENY="$(curl -s -b "$COOKIE_JAR" -H "X-Lumio-CSRF: $CSRF" -H 'Content-Type: application/json' -X POST \
    -d '{"requestId":"it-a5","action":"restart","unit":"nginx.service"}' \
    "$BASE/api/v1/services/action")"
DENY_AUDIT="$(audit_query "SELECT outcome FROM audit WHERE request_id='it-a5' AND kind='deny'")"
if grep -q '"code":"forbidden"' <<<"$DENY" && [[ "$DENY_AUDIT" == "denied" ]]; then
    ok "polkit denial -> 403 audited outcome=denied"
else
    echo "  got: $DENY / audit: $DENY_AUDIT"
    bad "polkit denial -> 403 audited"
fi

echo "== Phase 4 gate 11: unknown cookie -> 401 =="
CODE="$(curl -s -o /dev/null -w '%{http_code}' -H 'Cookie: lumio_session=deadbeef' "$BASE/api/v1/services")"
if [[ "$CODE" == 401 ]]; then ok "unknown session cookie -> 401"; else bad "unknown session cookie -> 401 (got $CODE)"; fi

echo "== Phase 5 EXIT GATE: diagnose and repair a failed web service =="
if docker exec "$CONTAINER" systemctl is-failed --quiet lumio-test-web.service; then
    ok "repair fixture starts in failed state"
else
    bad "repair fixture starts in failed state"
fi
expect_in "failed web service is visible in Services" "$BASE/api/v1/services" '"name":"lumio-test-web.service"'
expect_in "failed web service error is visible in Logs" "$BASE/api/v1/journal?unit=lumio-test-web.service&limit=20" 'port must be between 1024 and 65535'
WEB_CONFIG="$(curl -s -b "$COOKIE_JAR" "$BASE/api/v1/files/read?path=/etc/lumio-test-web.json")"
WEB_REV="$(json_field "$WEB_CONFIG" revision)"
if [[ -n "$WEB_REV" ]] && grep -q 'eyJwb3J0IjotMX0K' <<<"$WEB_CONFIG"; then
    ok "protected web-service config is readable with a revision"
else
    echo "  got: $WEB_CONFIG"
    bad "protected web-service config is readable with a revision"
fi
WEB_WRITE="$(curl -s -b "$COOKIE_JAR" -H "X-Lumio-CSRF: $CSRF" -H 'Content-Type: application/json' -X POST \
    -d "{\"path\":\"/etc/lumio-test-web.json\",\"content\":\"$(b64 '{"port":18081}')\",\"expectedRevision\":\"$WEB_REV\",\"restartUnit\":\"lumio-test-web.service\",\"requestId\":\"it-web-repair\"}" \
    "$BASE/api/v1/files/write-privileged")"
if grep -q '"validation":{"kind":"json","checked":true}' <<<"$WEB_WRITE" \
    && grep -q '"restart":{"success":true' <<<"$WEB_WRITE"; then
    ok "protected file validates, writes atomically and restarts its service"
else
    echo "  got: $WEB_WRITE"
    bad "protected file validates, writes atomically and restarts its service"
fi
WEB_HEALTHY=0
for _ in $(seq 1 20); do
    if docker exec "$CONTAINER" curl -fsS http://127.0.0.1:18081 2>/dev/null | grep -q 'lumio phase 5 web service'; then
        WEB_HEALTHY=1
        break
    fi
    sleep 0.25
done
if [[ "$WEB_HEALTHY" == 1 ]]; then
    ok "EXIT GATE: repaired web service answers HTTP"
else
    bad "EXIT GATE: repaired web service answers HTTP"
fi
WEB_AUDIT="$(audit_query "SELECT kind || '|' || outcome FROM audit WHERE request_id='it-web-repair' ORDER BY id")"
ROLLBACKS="$(docker exec "$CONTAINER" sh -c 'find /var/lib/lumio/rollback/files -type f | wc -l' 2>/dev/null)"
if grep -q 'begin|pending' <<<"$WEB_AUDIT" && grep -q 'end|success' <<<"$WEB_AUDIT" && [[ "$ROLLBACKS" -ge 1 ]]; then
    ok "protected repair is audited and has a rollback copy"
else
    echo "  audit: $WEB_AUDIT / rollbacks: $ROLLBACKS"
    bad "protected repair is audited and has a rollback copy"
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
