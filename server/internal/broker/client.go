// SPDX-License-Identifier: AGPL-3.0-only
package broker

import (
	"bytes"
	"context"
	"io"
	"net/http"

	"lumio-os/server/internal/ipc"
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
