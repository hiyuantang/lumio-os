// SPDX-License-Identifier: AGPL-3.0-only
package journal

import (
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

var priorityLabels = map[string]string{
	"0": "emerg",
	"1": "alert",
	"2": "crit",
	"3": "err",
	"4": "warning",
	"5": "notice",
	"6": "info",
	"7": "debug",
}

const (
	maxMessageBytes = 16 * 1024
	maxFieldBytes   = 4 * 1024
)

func truncateString(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max]
}

func priorityName(code string) string {
	if name, ok := priorityLabels[code]; ok {
		return name
	}
	return code
}

func parseLine(line []byte) (Entry, error) {
	if len(line) == 0 {
		return Entry{}, errors.New("empty journal line")
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(line, &raw); err != nil {
		return Entry{}, err
	}
	entry := Entry{Fields: map[string]string{}}
	for key, value := range raw {
		switch key {
		case "__CURSOR":
			entry.Cursor, _ = valueToString(value)
		case "__REALTIME_TIMESTAMP":
			micros, _ := valueToString(value)
			entry.TS = formatJournalTime(micros)
		case "PRIORITY":
			code, _ := valueToString(value)
			entry.Priority = priorityName(code)
		case "_SYSTEMD_UNIT":
			entry.Unit, _ = valueToString(value)
		case "MESSAGE":
			entry.Message, _ = valueToString(value)
		default:
			if strings.HasPrefix(key, "__") {
				continue
			}
			if s, ok := valueToString(value); ok {
				entry.Fields[key] = s
			}
		}
	}
	if entry.Cursor == "" {
		return Entry{}, errors.New("journal entry without cursor")
	}
	entry.Message = truncateString(entry.Message, maxMessageBytes)
	for key, value := range entry.Fields {
		entry.Fields[key] = truncateString(value, maxFieldBytes)
	}
	if len(entry.Fields) == 0 {
		entry.Fields = nil
	}
	return entry, nil
}

func valueToString(raw json.RawMessage) (string, bool) {
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s, true
	}
	var bytes []int
	if err := json.Unmarshal(raw, &bytes); err != nil {
		return "", false
	}
	buf := make([]byte, len(bytes))
	for i, b := range bytes {
		if b < 0 || b > 255 {
			return "", false
		}
		buf[i] = byte(b)
	}
	if !utf8.Valid(buf) {
		return "", false
	}
	return string(buf), true
}

func formatJournalTime(micros string) string {
	us, err := strconv.ParseInt(micros, 10, 64)
	if err != nil {
		return ""
	}
	return time.UnixMicro(us).UTC().Format("2006-01-02T15:04:05.999Z07:00")
}
