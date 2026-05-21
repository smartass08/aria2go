package engine

import (
	"context"
	"strings"

	rpc_transport "github.com/smartass08/aria2go/internal/rpc/transport"
)

// RPCBackend is the interface the rpcAdapter bridges to the transport layer.
type RPCBackend interface {
	Call(ctx context.Context, token, method string, params []any) (any, error)
	SubscribeNotifications(sink func(name string, params map[string]any)) (cancel func())
}

// rpcAdapter bridges an RPCBackend to transport.Dispatcher.
type rpcAdapter struct {
	b RPCBackend
}

func (a *rpcAdapter) Call(method string, params []any) (any, error) {
	token, remaining := extractRPCSecretToken(params)
	return a.b.Call(context.TODO(), token, method, remaining)
}

func extractRPCSecretToken(params []any) (string, []any) {
	if len(params) == 0 {
		return "", params
	}
	s, ok := params[0].(string)
	if !ok || !strings.HasPrefix(s, "token:") {
		return "", params
	}
	return strings.TrimPrefix(s, "token:"), params[1:]
}

func (a *rpcAdapter) SubscribeNotifications(ctx context.Context) (<-chan rpc_transport.Notification, error) {
	ch := make(chan rpc_transport.Notification, 64)
	cancel := a.b.SubscribeNotifications(func(name string, params map[string]any) {
		notif := rpc_transport.Notification{
			Method: name,
			Params: []any{params},
		}
		select {
		case ch <- notif:
		case <-ctx.Done():
		}
	})
	go func() {
		<-ctx.Done()
		cancel()
		close(ch)
	}()
	return ch, nil
}
