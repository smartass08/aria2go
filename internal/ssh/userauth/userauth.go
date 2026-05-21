// Package userauth implements the SSH Authentication Protocol (RFC 4252).
//
// It provides client-side authentication over an established SSH transport
// connection. Supported methods: password, publickey (RSA, Ed25519),
// keyboard-interactive.
package userauth

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/sha512"
	"fmt"
	"math/big"

	"github.com/smartass08/aria2go/internal/core"
	"github.com/smartass08/aria2go/internal/ssh/wire"
)

// TransportConn is the interface the SSH transport layer must satisfy
// for user authentication.
type TransportConn interface {
	Send(payload []byte) error
	Receive() ([]byte, error)
	SessionID() []byte
	Close() error
}

// Message codes (RFC 4252 §6).
const (
	SSH_MSG_SERVICE_REQUEST        = 5
	SSH_MSG_SERVICE_ACCEPT         = 6
	SSH_MSG_USERAUTH_REQUEST       = 50
	SSH_MSG_USERAUTH_FAILURE       = 51
	SSH_MSG_USERAUTH_SUCCESS       = 52
	SSH_MSG_USERAUTH_BANNER        = 53
	SSH_MSG_USERAUTH_PK_OK         = 60
	SSH_MSG_USERAUTH_INFO_REQUEST  = 60
	SSH_MSG_USERAUTH_INFO_RESPONSE = 61
)

var (
	ErrAuthFailed = core.NewError(core.ExitHTTPAuthFailed, "SSH authentication failed")
	ErrBadPacket  = core.NewError(core.ExitUnknownError, "bad SSH authentication packet")
)

// AuthMethod is the interface for SSH authentication methods.
type AuthMethod interface {
	Name() string
}

// PasswordAuth implements the "password" authentication method (RFC 4252 §8).
type PasswordAuth struct {
	Username string
	Password string
}

func (a *PasswordAuth) Name() string { return "password" }

// PublicKeyAuth implements the "publickey" authentication method (RFC 4252 §7).
type PublicKeyAuth struct {
	Username string
	Key      any
}

func (a *PublicKeyAuth) Name() string { return "publickey" }

// KeyboardInteractiveAuth implements the "keyboard-interactive"
// authentication method (RFC 4256).
type KeyboardInteractiveAuth struct {
	Username  string
	Challenge func(name, instruction string, questions []string) ([]string, error)
}

func (a *KeyboardInteractiveAuth) Name() string { return "keyboard-interactive" }

// Client performs SSH client authentication.
type Client struct {
	conn           TransportConn
	BannerCallback func(message, language string) // optional, called on SSH_MSG_USERAUTH_BANNER (RFC 4252 §5.4)
}

// NewClient creates a new SSH authentication client.
func NewClient(conn TransportConn) *Client {
	return &Client{conn: conn}
}

// Authenticate performs SSH authentication by trying each
// authentication method in sequence until one succeeds (aria2 behavior).
// The transport layer must have already requested and been granted the
// "ssh-userauth" service before calling this method.
func (c *Client) Authenticate(sessionID []byte, methods []AuthMethod) error {
	for _, m := range methods {
		if err := c.authenticate(m, sessionID); err == nil {
			return nil
		}
	}
	return core.NewError(core.ExitHTTPAuthFailed, "SSH authentication: all methods exhausted")
}

func (c *Client) authenticate(m AuthMethod, sessionID []byte) error {
	switch m := m.(type) {
	case *PasswordAuth:
		return c.authenticatePassword(m)
	case *PublicKeyAuth:
		return c.authenticatePublicKey(m, sessionID)
	case *KeyboardInteractiveAuth:
		return c.authenticateKeyboardInteractive(m)
	default:
		return fmt.Errorf("userauth: unsupported auth method %q", m.Name())
	}
}

func (c *Client) authenticatePassword(a *PasswordAuth) error {
	b := wire.GetBuilder()
	b.PutByte(SSH_MSG_USERAUTH_REQUEST)
	b.WriteString(a.Username)
	b.WriteString("ssh-connection")
	b.WriteString("password")
	b.WriteBool(false)
	b.WriteString(a.Password)

	if err := c.conn.Send(b.Payload()); err != nil {
		wire.PutBuilder(b)
		return fmt.Errorf("userauth: password send: %w", err)
	}
	wire.PutBuilder(b)

	return c.receiveAuthResponse()
}

func (c *Client) authenticatePublicKey(a *PublicKeyAuth, sessionID []byte) error {
	keyAlg, pubBlob, privData, err := extractKey(a.Key)
	if err != nil {
		return err
	}

	sigAlg := signatureAlgorithm(keyAlg, privData)

	// Probe if the server accepts this key (RFC 4252 §7).
	{
		b := wire.GetBuilder()
		b.PutByte(SSH_MSG_USERAUTH_REQUEST)
		b.WriteString(a.Username)
		b.WriteString("ssh-connection")
		b.WriteString("publickey")
		b.WriteBool(false)
		b.WriteString(keyAlg)
		b.WriteString(string(pubBlob))

		if err := c.conn.Send(b.Payload()); err != nil {
			wire.PutBuilder(b)
			return fmt.Errorf("userauth: publickey probe: %w", err)
		}
		wire.PutBuilder(b)
		resp, err := c.readAuthPacket()
		if err != nil {
			return err
		}
		switch resp[0] {
		case SSH_MSG_USERAUTH_PK_OK:
			if err := validatePKOK(resp, keyAlg, pubBlob); err != nil {
				return err
			}
		case SSH_MSG_USERAUTH_FAILURE:
			return newAuthFailedError(resp)
		default:
			return fmt.Errorf("userauth: unexpected probe response %d", resp[0])
		}
	}

	// Build the signature blob (RFC 4252 §7).
	sigBlob := buildSignatureBlob(sessionID, a.Username, keyAlg, pubBlob)

	signature, err := signPayload(sigAlg, privData, sigBlob)
	if err != nil {
		return err
	}

	// Send authenticated request with signature.
	{
		b := wire.GetBuilder()
		b.PutByte(SSH_MSG_USERAUTH_REQUEST)
		b.WriteString(a.Username)
		b.WriteString("ssh-connection")
		b.WriteString("publickey")
		b.WriteBool(true)
		b.WriteString(keyAlg)
		b.WriteString(string(pubBlob))
		b.WriteString(string(signature))

		if err := c.conn.Send(b.Payload()); err != nil {
			wire.PutBuilder(b)
			return fmt.Errorf("userauth: publickey sign: %w", err)
		}
		wire.PutBuilder(b)
		return c.receiveAuthResponse()
	}
}

func (c *Client) authenticateKeyboardInteractive(a *KeyboardInteractiveAuth) error {
	{
		b := wire.GetBuilder()
		b.PutByte(SSH_MSG_USERAUTH_REQUEST)
		b.WriteString(a.Username)
		b.WriteString("ssh-connection")
		b.WriteString("keyboard-interactive")
		b.WriteString("")
		b.WriteString("")

		if err := c.conn.Send(b.Payload()); err != nil {
			wire.PutBuilder(b)
			return fmt.Errorf("userauth: kbd-int request: %w", err)
		}
		wire.PutBuilder(b)
	}

	for {
		resp, err := c.readAuthPacket()
		if err != nil {
			return err
		}
		switch resp[0] {
		case SSH_MSG_USERAUTH_SUCCESS:
			return nil
		case SSH_MSG_USERAUTH_FAILURE:
			return newAuthFailedError(resp)
		case SSH_MSG_USERAUTH_INFO_REQUEST:
			ansPayload, err := c.processKbdInteractiveChallenge(a, resp)
			if err != nil {
				return err
			}
			if err := c.conn.Send(ansPayload); err != nil {
				return fmt.Errorf("userauth: kbd-int response: %w", err)
			}
		default:
			return fmt.Errorf("userauth: unexpected kbd-int response %d", resp[0])
		}
	}
}

func (c *Client) processKbdInteractiveChallenge(a *KeyboardInteractiveAuth, payload []byte) ([]byte, error) {
	r := &wire.Reader{Buf: payload}
	_ = r.GetByte() // msg code 60
	name := r.ReadString()
	instruction := r.ReadString()
	_ = r.ReadString() // language tag
	numPrompts := r.ReadUint32()
	if r.Err != nil {
		return nil, fmt.Errorf("userauth: parse kbd-int challenge: %w", r.Err)
	}
	if numPrompts > 1024 {
		return nil, fmt.Errorf("userauth: too many prompts: %d", numPrompts)
	}

	questions := make([]string, numPrompts)
	for i := uint32(0); i < numPrompts; i++ {
		questions[i] = r.ReadString()
		_ = r.ReadBool() // echo
	}
	if r.Err != nil {
		return nil, fmt.Errorf("userauth: parse kbd-int prompts: %w", r.Err)
	}

	answers, err := a.Challenge(name, instruction, questions)
	if err != nil {
		return nil, fmt.Errorf("userauth: kbd-int challenge callback: %w", err)
	}
	if len(answers) != int(numPrompts) {
		return nil, fmt.Errorf("userauth: kbd-int expected %d answers, got %d", numPrompts, len(answers))
	}

	b := wire.NewBuilder()
	b.PutByte(SSH_MSG_USERAUTH_INFO_RESPONSE)
	b.WriteUint32(numPrompts)
	for _, ans := range answers {
		b.WriteString(ans)
	}
	return b.Payload(), nil
}

// readAuthPacket reads one packet, skipping SSH_MSG_USERAUTH_BANNER (RFC 4252 §5.4).
// If BannerCallback is set, it is invoked with the banner message and language tag.
func (c *Client) readAuthPacket() ([]byte, error) {
	for {
		resp, err := c.conn.Receive()
		if err != nil {
			return nil, fmt.Errorf("userauth: read: %w", err)
		}
		if len(resp) < 1 {
			return nil, ErrBadPacket
		}
		if resp[0] == SSH_MSG_USERAUTH_BANNER {
			if c.BannerCallback != nil {
				r := &wire.Reader{Buf: resp}
				_ = r.GetByte()        // msg code 53
				msg := r.ReadString()  // banner message
				lang := r.ReadString() // language tag
				if r.Err == nil {
					c.BannerCallback(msg, lang)
				}
			}
			continue
		}
		return resp, nil
	}
}

// receiveAuthResponse blocks until SSH_MSG_USERAUTH_SUCCESS or FAILURE,
// skipping banners (RFC 4252 §5.4). On failure, it parses the failure
// payload to extract the list of acceptable methods.
func (c *Client) receiveAuthResponse() error {
	resp, err := c.readAuthPacket()
	if err != nil {
		return err
	}
	switch resp[0] {
	case SSH_MSG_USERAUTH_SUCCESS:
		return nil
	case SSH_MSG_USERAUTH_FAILURE:
		return newAuthFailedError(resp)
	default:
		return fmt.Errorf("userauth: unexpected auth response %d", resp[0])
	}
}

// newAuthFailedError parses SSH_MSG_USERAUTH_FAILURE (RFC 4252 §5.1)
// to include the server's list of acceptable methods.
func newAuthFailedError(payload []byte) error {
	r := &wire.Reader{Buf: payload}
	_ = r.GetByte() // msg code
	methods := r.ReadString()
	if methods != "" {
		return core.NewError(core.ExitHTTPAuthFailed,
			fmt.Sprintf("SSH authentication failed (server accepts: %s)", methods))
	}
	return ErrAuthFailed
}

// validatePKOK checks that SSH_MSG_USERAUTH_PK_OK echoes the algorithm
// and public key we probed (RFC 4252 §7).
func validatePKOK(payload []byte, expectedAlg string, expectedBlob []byte) error {
	r := &wire.Reader{Buf: payload}
	_ = r.GetByte() // msg code 60
	alg := r.ReadString()
	blob := r.ReadBytes()
	if r.Err != nil {
		return fmt.Errorf("userauth: malformed PK_OK: %w", r.Err)
	}
	if alg != expectedAlg {
		return fmt.Errorf("userauth: PK_OK algorithm mismatch: got %q, want %q", alg, expectedAlg)
	}
	if string(blob) != string(expectedBlob) {
		return fmt.Errorf("userauth: PK_OK public key mismatch")
	}
	return nil
}

// extractKey decomposes a private key into its SSH algorithm name, public key
// blob, and the raw private key data. The returned algorithm name is the key
// type (e.g., "ssh-rsa", "ssh-ed25519"), not the signature algorithm.
func extractKey(key any) (alg string, pubBlob []byte, privData any, err error) {
	switch k := key.(type) {
	case *rsa.PrivateKey:
		pub := &k.PublicKey
		alg = "ssh-rsa"
		b := wire.NewBuilder()
		b.WriteString(alg)
		b.WriteMpint(big.NewInt(int64(pub.E)))
		b.WriteMpint(pub.N)
		return alg, b.Payload(), k, nil
	case ed25519.PrivateKey:
		pub := k.Public().(ed25519.PublicKey)
		alg = "ssh-ed25519"
		b := wire.NewBuilder()
		b.WriteString(alg)
		b.WriteString(string(pub))
		return alg, b.Payload(), k, nil
	case *ecdsa.PrivateKey:
		return "", nil, nil, fmt.Errorf("userauth: ECDSA keys not yet implemented")
	default:
		return "", nil, nil, fmt.Errorf("userauth: unsupported key type %T", key)
	}
}

// signatureAlgorithm returns the SSH signature algorithm name for the given
// key algorithm and private key data. For RSA, this is rsa-sha2-256 or
// rsa-sha2-512 (RFC 8332) instead of the key type "ssh-rsa".
func signatureAlgorithm(keyAlg string, privData any) string {
	if keyAlg == "ssh-rsa" {
		pk := privData.(*rsa.PrivateKey)
		if pk.N.BitLen() >= 3072 {
			return "rsa-sha2-512"
		}
		return "rsa-sha2-256"
	}
	return keyAlg
}

// signPayload produces an SSH signature blob [string algorithm, string signature]
// for the given data using the private key.
func signPayload(sigAlg string, privData any, data []byte) ([]byte, error) {
	switch pk := privData.(type) {
	case *rsa.PrivateKey:
		var hash crypto.Hash
		switch sigAlg {
		case "rsa-sha2-512":
			hash = crypto.SHA512
			sum := sha512.Sum512(data)
			sig, err := rsa.SignPKCS1v15(rand.Reader, pk, hash, sum[:])
			if err != nil {
				return nil, fmt.Errorf("userauth: rsa sign: %w", err)
			}
			sb := wire.NewBuilder()
			sb.WriteString(sigAlg)
			sb.WriteString(string(sig))
			return sb.Payload(), nil
		default:
			hash = crypto.SHA256
			sum := sha256.Sum256(data)
			sig, err := rsa.SignPKCS1v15(rand.Reader, pk, hash, sum[:])
			if err != nil {
				return nil, fmt.Errorf("userauth: rsa sign: %w", err)
			}
			sb := wire.NewBuilder()
			sb.WriteString(sigAlg)
			sb.WriteString(string(sig))
			return sb.Payload(), nil
		}
	case ed25519.PrivateKey:
		sig := ed25519.Sign(pk, data)
		sb := wire.NewBuilder()
		sb.WriteString(sigAlg)
		sb.WriteString(string(sig))
		return sb.Payload(), nil
	default:
		return nil, fmt.Errorf("userauth: unsupported private key type %T", privData)
	}
}

func buildSignatureBlob(sessionID []byte, username, keyAlg string, pubKeyBlob []byte) []byte {
	b := wire.NewBuilder()
	b.WriteString(string(sessionID))
	b.PutByte(SSH_MSG_USERAUTH_REQUEST)
	b.WriteString(username)
	b.WriteString("ssh-connection")
	b.WriteString("publickey")
	b.WriteBool(true)
	b.WriteString(keyAlg)
	b.WriteString(string(pubKeyBlob))
	return b.Payload()
}
