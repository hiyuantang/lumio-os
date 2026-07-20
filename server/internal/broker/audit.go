// SPDX-License-Identifier: AGPL-3.0-only
package broker

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

var errInvalidReply = errors.New("unexpected reply shape")

type Audit struct {
	db *sql.DB
}

func OpenAudit(path string) (*Audit, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=synchronous(NORMAL)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS audit (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		ts TEXT NOT NULL,
		kind TEXT NOT NULL,
		request_id TEXT NOT NULL,
		uid INTEGER NOT NULL,
		user_name TEXT NOT NULL,
		action TEXT NOT NULL,
		arguments_hash TEXT NOT NULL,
		polkit_result TEXT NOT NULL,
		outcome TEXT NOT NULL,
		error TEXT NOT NULL,
		duration_ms INTEGER NOT NULL,
		result_json TEXT NOT NULL
	)`)
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_audit_request ON audit(request_id)`); err != nil {
		return nil, err
	}
	return &Audit{db: db}, nil
}

func (a *Audit) insert(kind string, req ActionRequest, uid uint32, userName, polkitResult, outcome, errText string, resultJSON string, duration time.Duration) int64 {
	res, err := a.db.Exec(`INSERT INTO audit
		(ts, kind, request_id, uid, user_name, action, arguments_hash, polkit_result, outcome, error, duration_ms, result_json)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		time.Now().UTC().Format(time.RFC3339Nano), kind, req.RequestID, uid, userName,
		req.Action, argumentsHash(req), polkitResult, outcome, errText, duration.Milliseconds(), resultJSON)
	if err != nil {
		return 0
	}
	id, _ := res.LastInsertId()
	return id
}

func (a *Audit) Begin(req ActionRequest, uid uint32, userName, polkitResult string) int64 {
	return a.insert("begin", req, uid, userName, polkitResult, "pending", "", "", 0)
}

func (a *Audit) End(beginID int64, req ActionRequest, uid uint32, userName, polkitResult, outcome, errText string, result map[string]any, duration time.Duration) {
	encoded := ""
	if result != nil {
		if b, err := json.Marshal(result); err == nil {
			encoded = string(b)
		}
	}
	a.insert("end", req, uid, userName, polkitResult, outcome, errText, encoded, duration)
}

func (a *Audit) Deny(req ActionRequest, uid uint32, userName, polkitResult string, apiErr *apiError) {
	a.insert("deny", req, uid, userName, polkitResult, "denied", apiErr.Message, "", 0)
}

func (a *Audit) StoredResult(requestID string) (int, string, bool) {
	rows, err := a.db.Query(`SELECT kind, outcome, error, result_json FROM audit WHERE request_id = ? ORDER BY id`, requestID)
	if err != nil {
		return 0, "", false
	}
	defer rows.Close()
	for rows.Next() {
		var kind, outcome, errText, resultJSON string
		if err := rows.Scan(&kind, &outcome, &errText, &resultJSON); err != nil {
			continue
		}
		switch kind {
		case "end":
			if outcome == "success" {
				return 200, fmt.Sprintf(`{"ok":true,"data":%s}`, resultJSON), true
			}
			return 500, fmt.Sprintf(`{"ok":false,"error":{"code":"internal","message":%q}}`, errText), true
		case "deny":
			return 403, fmt.Sprintf(`{"ok":false,"error":{"code":"forbidden","message":%q}}`, errText), true
		}
	}
	return 0, "", false
}

func (a *Audit) RequestOwned(requestID string, uid uint32) bool {
	var found int
	err := a.db.QueryRow(`SELECT 1 FROM audit WHERE request_id = ? AND uid = ? LIMIT 1`, requestID, uid).Scan(&found)
	return err == nil && found == 1
}
