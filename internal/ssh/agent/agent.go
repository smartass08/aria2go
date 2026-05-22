// Package agent provides a local SSH agent client used for SFTP public-key
// authentication.
package agent

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os"

	xssh "golang.org/x/crypto/ssh"
	xagent "golang.org/x/crypto/ssh/agent"
)

var ErrUnavailable = errors.New("ssh/agent: unavailable")

// Agent represents a connection to the local SSH agent.
type Agent struct {
	conn   net.Conn
	client xagent.ExtendedAgent
}

// New creates a new Agent using SSH_AUTH_SOCK.
func New() (*Agent, error) {
	socketPath := os.Getenv("SSH_AUTH_SOCK")
	if socketPath == "" {
		return nil, fmt.Errorf("%w: SSH_AUTH_SOCK is not set", ErrUnavailable)
	}
	a := &Agent{}
	if err := a.Connect(socketPath); err != nil {
		return nil, err
	}
	return a, nil
}

// Connect connects to the local SSH agent socket.
func (a *Agent) Connect(socketPath string) error {
	if socketPath == "" {
		return fmt.Errorf("%w: empty socket path", ErrUnavailable)
	}
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	a.conn = conn
	a.client = xagent.NewClient(conn)
	return nil
}

// Close closes the agent connection.
func (a *Agent) Close() error {
	if a.conn == nil {
		return nil
	}
	err := a.conn.Close()
	a.conn = nil
	a.client = nil
	return err
}

// List returns the identities available from the agent.
func (a *Agent) List() ([]Identity, error) {
	if a.client == nil {
		return nil, fmt.Errorf("%w: not connected", ErrUnavailable)
	}
	keys, err := a.client.List()
	if err != nil {
		return nil, err
	}
	out := make([]Identity, 0, len(keys))
	for _, key := range keys {
		out = append(out, Identity{
			Comment: key.Comment,
			KeyType: key.Format,
			KeyBlob: append([]byte(nil), key.Blob...),
		})
	}
	return out, nil
}

// Sign signs data with the identity whose key blob exactly matches keyID.
func (a *Agent) Sign(keyID string, data []byte) ([]byte, error) {
	if a.client == nil {
		return nil, fmt.Errorf("%w: not connected", ErrUnavailable)
	}
	pub, err := xssh.ParsePublicKey([]byte(keyID))
	if err != nil {
		return nil, fmt.Errorf("ssh/agent: parse key id: %w", err)
	}
	sig, err := a.client.Sign(pub, data)
	if err != nil {
		return nil, err
	}
	return marshalSignature(sig), nil
}

// Identity represents an SSH key identity in the agent.
type Identity struct {
	Comment string
	KeyType string
	KeyBlob []byte
}

// Signer implements ssh.Signer/ssh.AlgorithmSigner over the agent protocol.
type Signer struct {
	agent    *Agent
	identity Identity
	pub      xssh.PublicKey
}

// NewSigner returns a signer wrapper for the given identity.
func NewSigner(agent *Agent, identity Identity) *Signer {
	pub, _ := xssh.ParsePublicKey(identity.KeyBlob)
	return &Signer{
		agent:    agent,
		identity: identity,
		pub:      pub,
	}
}

// PublicKey returns the signer's public key.
func (s *Signer) PublicKey() xssh.PublicKey {
	return s.pub
}

// Sign signs data using the agent's default signature selection.
func (s *Signer) Sign(_ io.Reader, data []byte) (*xssh.Signature, error) {
	if s.agent == nil || s.agent.client == nil {
		return nil, fmt.Errorf("%w: not connected", ErrUnavailable)
	}
	if s.pub == nil {
		return nil, errors.New("ssh/agent: public key unavailable")
	}
	return s.agent.client.Sign(s.pub, data)
}

// SignWithAlgorithm signs data using the requested SSH signature algorithm.
func (s *Signer) SignWithAlgorithm(_ io.Reader, data []byte, algorithm string) (*xssh.Signature, error) {
	if s.agent == nil || s.agent.client == nil {
		return nil, fmt.Errorf("%w: not connected", ErrUnavailable)
	}
	if s.pub == nil {
		return nil, errors.New("ssh/agent: public key unavailable")
	}

	flags := xagent.SignatureFlags(0)
	switch algorithm {
	case xssh.KeyAlgoRSASHA256:
		flags = xagent.SignatureFlagRsaSha256
	case xssh.KeyAlgoRSASHA512:
		flags = xagent.SignatureFlagRsaSha512
	}
	return s.agent.client.SignWithFlags(s.pub, data, flags)
}

func marshalSignature(sig *xssh.Signature) []byte {
	if sig == nil {
		return nil
	}
	out := make([]byte, 0, 8+len(sig.Format)+len(sig.Blob))
	out = appendUint32(out, uint32(len(sig.Format)))
	out = append(out, sig.Format...)
	out = appendUint32(out, uint32(len(sig.Blob)))
	out = append(out, sig.Blob...)
	return out
}

func appendUint32(dst []byte, v uint32) []byte {
	return append(dst, byte(v>>24), byte(v>>16), byte(v>>8), byte(v))
}

var _ xssh.Signer = (*Signer)(nil)
var _ xssh.AlgorithmSigner = (*Signer)(nil)
