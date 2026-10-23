// Package channel abstracts the transport that carries player streams between
// the relay and a host agent.
//
// The relay only ever talks to a Dialer and the agent only to an Acceptor, so
// the concrete transport (a yamux session today, a direct or peer-to-peer
// connection later) can be swapped without touching room or routing logic.
package channel

import (
	"context"
	"net"

	"github.com/Sunse666/NoneFrp/internal/protocol"
)

// Dialer is the relay side of a data channel. Open returns a stream that has
// already been introduced to the agent with a preface.
type Dialer interface {
	Open(ctx context.Context, meta protocol.StreamMeta) (net.Conn, error)
	// Ready is closed once the channel can carry streams.
	Ready() <-chan struct{}
	Close() error
}

// Acceptor is the agent side of a data channel.
type Acceptor interface {
	// Accept blocks until the relay opens a stream and returns it together with
	// the preface that introduces it.
	Accept() (net.Conn, protocol.StreamMeta, error)
	Ready() <-chan struct{}
	Close() error
}
