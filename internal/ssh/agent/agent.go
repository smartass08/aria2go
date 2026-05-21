// Package agent provides a stub local SSH agent IPC client.
//
// In aria2, libssh2 supports agent authentication through
// libssh2_agent_connect() / libssh2_agent_list_identities() /
// libssh2_agent_userauth(). This package provides a stub implementation
// that returns "not implemented" for all operations, matching the
// incomplete state of the SSH agent support in this codebase.
//
// When the full SSH agent implementation is ready, this package will
// communicate with the local SSH agent via the Unix domain socket at
// $SSH_AUTH_SOCK using the SSH agent protocol (RFC draft).
package agent

import (
	"fmt"
)

// ErrNotImplemented is returned by all agent operations.
var ErrNotImplemented = fmt.Errorf("ssh/agent: not implemented")

// Agent represents a connection to the local SSH agent.
type Agent struct{}

// New creates a new Agent connection stub.
func New() (*Agent, error) {
	return nil, fmt.Errorf("%w (agent connection not yet implemented)", ErrNotImplemented)
}

// Connect attempts to connect to the local SSH agent socket.
func (a *Agent) Connect(socketPath string) error {
	return fmt.Errorf("%w (agent connection not yet implemented)", ErrNotImplemented)
}

// Close closes the connection to the SSH agent.
func (a *Agent) Close() error {
	return nil
}

// List returns the list of identities available from the agent.
func (a *Agent) List() ([]Identity, error) {
	return nil, fmt.Errorf("%w (agent identity listing not yet implemented)", ErrNotImplemented)
}

// Sign signs the given data with the specified identity.
func (a *Agent) Sign(keyID string, data []byte) ([]byte, error) {
	return nil, fmt.Errorf("%w (agent signing not yet implemented)", ErrNotImplemented)
}

// Identity represents an SSH key identity in the agent.
type Identity struct {
	Comment string
	KeyType string
	KeyBlob []byte
}
