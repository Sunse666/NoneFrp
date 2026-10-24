package client

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/websocket"

	"github.com/Sunse666/NoneFrp/internal/protocol"
)

// Probe checks a credential against a relay without registering a room, so the
// interface can answer "will this invite work?" without spending it.
func Probe(ctx context.Context, relayURL, token, deviceID string) (protocol.ProbeOK, error) {
	var result protocol.ProbeOK

	endpoint := strings.TrimSuffix(relayURL, "/") + "/v1/agent"
	header := http.Header{}
	header.Set("Authorization", "Bearer "+token)
	header.Set("X-Device-ID", deviceID)
	header.Set("X-Protocol-Version", strconv.Itoa(protocol.Version))

	dialer := &websocket.Dialer{HandshakeTimeout: registerTimeout}
	conn, resp, err := dialer.DialContext(ctx, endpoint, header)
	if err != nil {
		status := 0
		if resp != nil {
			status = resp.StatusCode
		}
		return result, fmt.Errorf("dial %s: %w (http %d)", endpoint, err, status)
	}
	defer func() { _ = conn.Close() }()

	if err := conn.SetReadDeadline(time.Now().Add(registerTimeout)); err != nil {
		return result, err
	}

	raw, err := protocol.Encode(protocol.TypeProbe, protocol.Probe{DeviceID: deviceID})
	if err != nil {
		return result, err
	}
	if err := conn.WriteMessage(websocket.TextMessage, raw); err != nil {
		return result, fmt.Errorf("send probe: %w", err)
	}

	_, reply, err := conn.ReadMessage()
	if err != nil {
		return result, fmt.Errorf("read probe reply: %w", err)
	}
	env, err := protocol.Decode(reply)
	if err != nil {
		return result, err
	}

	switch env.Type {
	case protocol.TypeProbeOK:
		return protocol.As[protocol.ProbeOK](env)
	case protocol.TypeError:
		body, _ := protocol.As[protocol.ErrorBody](env)
		return result, fmt.Errorf("%s: %s", body.Code, body.Message)
	default:
		return result, fmt.Errorf("unexpected reply %q", env.Type)
	}
}
