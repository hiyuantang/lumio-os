// SPDX-License-Identifier: AGPL-3.0-only
package broker

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"

	"lumio-os/server/internal/ipc"
	"lumio-os/server/internal/updates"
)

func CallAction(ctx context.Context, socketPath string, payload []byte) (int, http.Header, []byte, error) {
	client := ipc.HTTPClient(socketPath)
	req, err := http.NewRequestWithContext(ctx, "POST", "http://broker/action", bytes.NewReader(payload))
	if err != nil {
		return 0, nil, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return 0, nil, nil, err
	}
	return resp.StatusCode, resp.Header, body, nil
}

func UpdateProgress(ctx context.Context, socketPath, requestID string) (updates.Progress, error) {
	client := ipc.HTTPClient(socketPath)
	endpoint := "http://broker/updates/progress?requestId=" + url.QueryEscape(requestID)
	req, err := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
	if err != nil {
		return updates.Progress{}, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return updates.Progress{}, err
	}
	defer resp.Body.Close()
	var body struct {
		OK    bool             `json:"ok"`
		Data  updates.Progress `json:"data"`
		Error *apiError        `json:"error"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&body); err != nil {
		return updates.Progress{}, err
	}
	if !body.OK {
		if body.Error != nil {
			return updates.Progress{}, body.Error
		}
		return updates.Progress{}, io.ErrUnexpectedEOF
	}
	return body.Data, nil
}

func (e *apiError) Error() string {
	return e.Code + ": " + e.Message
}
