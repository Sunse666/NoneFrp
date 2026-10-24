package client

import (
	"context"
	"net"
	"strconv"
	"time"
)

const (
	// localProbeInterval is how often the local server is checked.
	localProbeInterval = 5 * time.Second
	// localProbeTimeout bounds one check.
	localProbeTimeout = 2 * time.Second
)

// probeLocal keeps Status.LocalOK up to date until ctx is cancelled. A host who
// started the agent before the Minecraft server, or pointed it at the wrong
// port, sees the problem in the interface instead of hearing it from a player.
func (c *Client) probeLocal(ctx context.Context) {
	c.checkLocal()

	ticker := time.NewTicker(localProbeInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.checkLocal()
		}
	}
}

func (c *Client) checkLocal() {
	target := net.JoinHostPort("127.0.0.1", strconv.Itoa(c.cfg.Local.Port))

	conn, err := net.DialTimeout("tcp", target, localProbeTimeout)
	if err != nil {
		c.status.setLocal(false)
		return
	}
	_ = conn.Close()
	c.status.setLocal(true)
}
