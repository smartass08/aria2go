// Package tlsx provides TLS configuration builders for aria2go clients and
// servers.  ClientConfig builds a tls.Config for outbound connections;
// ServerConfig builds a tls.Config for inbound connections.
//
// PKCS12 files are not supported by this package. Go's standard library does
// not include PKCS12 parsing. Convert PKCS12 files to PEM format before use.
//
// SSL_OP_SINGLE_ECDH_USE (OpenSSL option for single-use ECDH keys) is handled
// automatically by Go TLS 1.3, which always generates ephemeral keys per
// handshake. No configuration is needed.
//
// Go's default curve preferences (X25519, P-256, P-384, P-521) provide modern
// ECDHE support equivalent to OpenSSL's EC_KEY_new_by_curve_name for P-256.
package tlsx

import (
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"strings"
)

// ClientOpts holds parameters for building a client tls.Config.
// Set ServerName to enable hostname verification (required when SkipVerify is
// false for secure connections).
type ClientOpts struct {
	ServerName   string   // SNI hostname for certificate validation
	CACerts      [][]byte // PEM-encoded CA certificates
	ClientCert   []byte   // PEM client certificate
	ClientKey    []byte   // PEM client private key
	SkipVerify   bool     // skips server certificate verification
	Pinned       []byte   // DER-encoded pinned server leaf certificate
	ALPN         []string // ALPN protocol identifiers (e.g. ["h2","http/1.1"])
	MinVersion   uint16   // minimum TLS version (defaults to tls.VersionTLS12)
	CipherSuites []uint16 // TLS 1.0-1.2 cipher suites (Go secure defaults when empty)
	SystemCAs    bool     // merge system trust store into custom RootCAs
}

// ServerOpts holds parameters for building a server tls.Config.
type ServerOpts struct {
	CertFile     string             // path to PEM certificate file
	KeyFile      string             // path to PEM private key file
	ClientAuth   tls.ClientAuthType // client certificate authentication mode
	ClientCAs    [][]byte           // PEM-encoded CA certificates for verifying peer clients
	CipherSuites []uint16           // TLS 1.0-1.2 cipher suites (Go secure defaults when empty)
	SystemCAs    bool               // use/merge system trust store into ClientCAs
}

// TLSVersion maps aria2 option strings to Go TLS version constants. The empty
// string returns 0 so callers can keep package defaults.
func TLSVersion(version string) (uint16, error) {
	switch strings.ToUpper(strings.TrimSpace(version)) {
	case "":
		return 0, nil
	case "TLSV1.1":
		return tls.VersionTLS11, nil
	case "TLSV1.2":
		return tls.VersionTLS12, nil
	case "TLSV1.3":
		return tls.VersionTLS13, nil
	default:
		return 0, fmt.Errorf("tlsx: unsupported TLS version %q", version)
	}
}

// ClientConfig builds a tls.Config suitable for outbound TLS connections.  It
// returns an error when any provided certificate material cannot be parsed.
func ClientConfig(opts ClientOpts) (*tls.Config, error) {
	cfg := &tls.Config{
		MinVersion: tls.VersionTLS12,
	}

	if opts.MinVersion != 0 {
		cfg.MinVersion = opts.MinVersion
	}

	cfg.InsecureSkipVerify = opts.SkipVerify

	if opts.ServerName != "" {
		cfg.ServerName = opts.ServerName
	}

	if len(opts.ALPN) > 0 {
		cfg.NextProtos = append([]string{}, opts.ALPN...)
	}

	if len(opts.CipherSuites) > 0 {
		cfg.CipherSuites = append([]uint16{}, opts.CipherSuites...)
	}

	var pool *x509.CertPool
	if opts.SystemCAs {
		pool = systemCertPool()
	}

	if len(opts.CACerts) > 0 {
		if pool == nil {
			pool = x509.NewCertPool()
		}
		for i, pem := range opts.CACerts {
			if !pool.AppendCertsFromPEM(pem) {
				return nil, fmt.Errorf("tlsx: failed to parse CA certificate at index %d", i)
			}
		}
	}

	if pool != nil {
		cfg.RootCAs = pool
	}

	if (len(opts.ClientCert) > 0) != (len(opts.ClientKey) > 0) {
		return nil, fmt.Errorf("tlsx: client certificate and key must be provided together")
	}

	if len(opts.ClientCert) > 0 {
		cert, err := tls.X509KeyPair(opts.ClientCert, opts.ClientKey)
		if err != nil {
			return nil, fmt.Errorf("tlsx: failed to load client certificate: %w", err)
		}
		cfg.Certificates = []tls.Certificate{cert}
	}

	if len(opts.Pinned) > 0 {
		pinned := make([]byte, len(opts.Pinned))
		copy(pinned, opts.Pinned)
		cfg.VerifyConnection = func(state tls.ConnectionState) error {
			if len(state.PeerCertificates) == 0 {
				return fmt.Errorf("tlsx: no peer certificates presented")
			}
			leaf := state.PeerCertificates[0].Raw
			if len(leaf) != len(pinned) || subtle.ConstantTimeCompare(leaf, pinned) != 1 {
				return fmt.Errorf("tlsx: pinned certificate does not match peer leaf certificate")
			}
			return nil
		}
	}

	return cfg, nil
}

// ServerConfig builds a tls.Config suitable for accepting TLS connections.  It
// loads the certificate from CertFile and the private key from KeyFile, and
// sets the ClientAuth mode.  An error is returned when the files cannot be
// read or the certificate cannot be parsed.
func ServerConfig(opts ServerOpts) (*tls.Config, error) {
	certPEM, err := os.ReadFile(opts.CertFile)
	if err != nil {
		return nil, fmt.Errorf("tlsx: failed to read certificate file %q: %w", opts.CertFile, err)
	}
	keyPEM, err := os.ReadFile(opts.KeyFile)
	if err != nil {
		return nil, fmt.Errorf("tlsx: failed to read key file %q: %w", opts.KeyFile, err)
	}

	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, fmt.Errorf("tlsx: failed to load server certificate: %w", err)
	}

	cfg := &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
		ClientAuth:   opts.ClientAuth,
	}

	if len(opts.CipherSuites) > 0 {
		cfg.CipherSuites = append([]uint16{}, opts.CipherSuites...)
	}

	var clientPool *x509.CertPool
	if opts.SystemCAs {
		clientPool = systemCertPool()
	}

	if len(opts.ClientCAs) > 0 {
		if clientPool == nil {
			clientPool = x509.NewCertPool()
		}
		for i, pem := range opts.ClientCAs {
			if !clientPool.AppendCertsFromPEM(pem) {
				return nil, fmt.Errorf("tlsx: failed to parse client CA certificate at index %d", i)
			}
		}
	}

	if clientPool != nil {
		cfg.ClientCAs = clientPool
	}

	return cfg, nil
}

func systemCertPool() *x509.CertPool {
	pool, err := x509.SystemCertPool()
	if err != nil {
		return nil
	}
	return pool
}
