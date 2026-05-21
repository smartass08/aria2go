// Package transport provides HTTP and WebSocket RPC transports for
// aria2go, implementing JSON-RPC 2.0, XML-RPC, and WebSocket (RFC 6455)
// endpoints compatible with aria2 1.37.0's RPC interface.
//
// The transport layer is transport-agnostic: it accepts a Dispatcher
// interface to route method calls, leaving the transport to handle only
// protocol framing and authentication.
package transport

import "context"

// Dispatcher is the interface for dispatching RPC method calls.
type Dispatcher interface {
	Call(method string, params []any) (any, error)
	SubscribeNotifications(ctx context.Context) (<-chan Notification, error)
}

// Notification represents a server-to-client notification.
type Notification struct {
	Method string
	Params []any
}
