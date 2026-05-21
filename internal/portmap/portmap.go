// Package portmap manages UPnP IGD port mappings via SSDP discovery
// and SOAP calls to the WANIPConnection service.
package portmap

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Config holds the port mapping configuration.
type Config struct {
	InternalPort int
	ExternalPort int
	Protocols    []string
	Lifetime     int
	Interface    string
}

// Mapper manages UPnP IGD port mappings. It discovers the router via
// SSDP, adds port mappings via SOAP, and removes them on shutdown.
type Mapper struct {
	cfg Config

	mu         sync.Mutex
	client     *http.Client
	controlURL string
	externalIP net.IP
	mappedPort int
	ok         bool
	closed     bool
	done       chan struct{}
}

const (
	ssdpMulticastAddr = "239.255.255.250:1900"
	ssdpSearch        = "urn:schemas-upnp-org:service:WANIPConnection:1"
	ssdpTimeout       = 3 * time.Second
	soapContentType   = `text/xml; charset="utf-8"`

	ssdpMsearchReq = "M-SEARCH * HTTP/1.1\r\n" +
		"HOST: " + ssdpMulticastAddr + "\r\n" +
		"MAN: \"ssdp:discover\"\r\n" +
		"MX: 3\r\n" +
		"ST: " + ssdpSearch + "\r\n" +
		"\r\n"

	soapEnvelopePrefix    = `<?xml version="1.0"?>` + `<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/" s:encodingStyle="http://schemas.xmlsoap.org/soap/encoding/"><s:Body>`
	soapEnvelopeSuffix    = `</s:Body></s:Envelope>`
	soapGetExternalIP     = `<u:GetExternalIPAddress xmlns:u="urn:schemas-upnp-org:service:WANIPConnection:1"></u:GetExternalIPAddress>`
	soapAddPortMapping    = `<u:AddPortMapping xmlns:u="urn:schemas-upnp-org:service:WANIPConnection:1">`
	soapDeletePortMapping = `<u:DeletePortMapping xmlns:u="urn:schemas-upnp-org:service:WANIPConnection:1">`
	soapEndTag            = `</u:`
)

var soapBufPool = sync.Pool{
	New: func() any { return new(bytes.Buffer) },
}

// New creates a new Mapper from the given configuration.
func New(cfg Config) (*Mapper, error) {
	if cfg.InternalPort < 1 || cfg.InternalPort > 65535 {
		return nil, fmt.Errorf("portmap: invalid internal port %d", cfg.InternalPort)
	}
	if cfg.ExternalPort < 1 || cfg.ExternalPort > 65535 {
		return nil, fmt.Errorf("portmap: invalid external port %d", cfg.ExternalPort)
	}
	if len(cfg.Protocols) == 0 {
		cfg.Protocols = []string{"tcp"}
	}
	for _, p := range cfg.Protocols {
		switch strings.ToLower(p) {
		case "tcp", "udp":
		default:
			return nil, fmt.Errorf("portmap: unsupported protocol %q", p)
		}
	}
	if cfg.Lifetime == 0 {
		cfg.Lifetime = 3600
	}

	return &Mapper{
		cfg:  cfg,
		done: make(chan struct{}),
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}, nil
}

// Run starts the port mapping background process. It discovers the UPnP
// IGD device, adds port mappings, renews the lease periodically, and
// blocks until ctx is cancelled, then removes the mappings and returns nil.
func (m *Mapper) Run(ctx context.Context) error {
	controlURL, err := m.discover(ctx)
	if err != nil {
		return fmt.Errorf("portmap: discovery: %w", err)
	}

	extIP, err := m.getExternalIP(ctx, controlURL)
	if err != nil {
		return fmt.Errorf("portmap: get external IP: %w", err)
	}

	m.mu.Lock()
	m.controlURL = controlURL
	m.externalIP = extIP
	m.ok = true
	m.mu.Unlock()

	if err := m.addPortMappings(ctx); err != nil {
		return fmt.Errorf("portmap: add port mappings: %w", err)
	}

	// Renew lease at half the lifetime to avoid expiry.
	renewInterval := time.Duration(m.cfg.Lifetime) * time.Second / 2
	renewTicker := time.NewTicker(renewInterval)
	defer renewTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return m.deletePortMappings(context.Background())
		case <-m.done:
			return m.deletePortMappings(context.Background())
		case <-renewTicker.C:
			if err := m.addPortMappings(context.Background()); err != nil {
				return fmt.Errorf("portmap: lease renewal failed: %w", err)
			}
		}
	}
}

// ExternalAddr returns the externally visible IP address and mapped port
// if discovery and mapping succeeded. ok is false if not yet available.
func (m *Mapper) ExternalAddr() (ip net.IP, port int, ok bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.externalIP, m.cfg.ExternalPort, m.ok
}

// Close stops the mapper and removes port mappings. Safe to call
// multiple times.
func (m *Mapper) Close() error {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil
	}
	m.closed = true
	close(m.done)
	m.mu.Unlock()

	if m.controlURL != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return m.deleteMappingsWithURL(ctx, m.controlURL)
	}
	return nil
}

// discover sends an M-SEARCH SSDP request, fetches the device description
// XML from the LOCATION URL, and extracts the WANIPConnection controlURL.
func (m *Mapper) discover(ctx context.Context) (string, error) {
	location, err := m.ssdpDiscover(ctx)
	if err != nil {
		return "", err
	}
	return m.fetchControlURL(ctx, location)
}

// ssdpDiscover sends an M-SEARCH request on all non-loopback IPv4
// interfaces and returns the first LOCATION URL from a WANIPConnection
// response.
func (m *Mapper) ssdpDiscover(ctx context.Context) (string, error) {
	addrs, err := m.ssdpLocalAddrs()
	if err != nil {
		return "", err
	}

	multicastAddr, err := net.ResolveUDPAddr("udp", ssdpMulticastAddr)
	if err != nil {
		return "", fmt.Errorf("resolve multicast: %w", err)
	}

	req := ssdpMsearchReq

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	type result struct {
		loc string
		err error
	}
	resultCh := make(chan result, len(addrs))

	for _, addr := range addrs {
		go func(localIP net.IP) {
			loc, err := m.ssdpOnAddr(ctx, localIP, multicastAddr, req)
			resultCh <- result{loc, err}
		}(addr)
	}

	var firstErr error
	for range addrs {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case r := <-resultCh:
			if r.err != nil {
				if firstErr == nil {
					firstErr = r.err
				}
				continue
			}
			cancel()
			return r.loc, nil
		}
	}
	if firstErr != nil {
		return "", firstErr
	}
	return "", fmt.Errorf("portmap: no UPnP IGD device found")
}

func (m *Mapper) ssdpOnAddr(ctx context.Context, localIP net.IP, multicastAddr *net.UDPAddr, req string) (string, error) {
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: localIP})
	if err != nil {
		return "", fmt.Errorf("listen UDP on %s: %w", localIP, err)
	}
	defer conn.Close()

	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(ssdpTimeout)
	}
	conn.SetDeadline(deadline)

	if _, err := conn.WriteToUDP([]byte(req), multicastAddr); err != nil {
		return "", fmt.Errorf("send M-SEARCH on %s: %w", localIP, err)
	}

	buf := make([]byte, 4096)
	for {
		n, _, err := conn.ReadFromUDP(buf)
		if err != nil {
			return "", fmt.Errorf("read M-SEARCH response on %s: %w", localIP, err)
		}
		resp := string(buf[:n])
		if !strings.Contains(resp, ssdpSearch) {
			continue
		}
		loc := parseHeader(resp, "LOCATION")
		if loc == "" {
			continue
		}
		return loc, nil
	}
}

// ssdpLocalAddrs returns the IPv4 addresses to use for SSDP discovery.
func (m *Mapper) ssdpLocalAddrs() ([]net.IP, error) {
	if m.cfg.Interface != "" {
		iface, err := net.InterfaceByName(m.cfg.Interface)
		if err != nil {
			return nil, fmt.Errorf("interface %q: %w", m.cfg.Interface, err)
		}
		addrs, err := iface.Addrs()
		if err != nil {
			return nil, fmt.Errorf("interface addrs: %w", err)
		}
		for _, a := range addrs {
			if ipnet, ok := a.(*net.IPNet); ok && ipnet.IP.To4() != nil {
				return []net.IP{ipnet.IP}, nil
			}
		}
		return nil, fmt.Errorf("interface %q has no IPv4 address", m.cfg.Interface)
	}

	allAddrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil, fmt.Errorf("interface addrs: %w", err)
	}
	var addrs []net.IP
	for _, a := range allAddrs {
		if ipnet, ok := a.(*net.IPNet); ok && ipnet.IP.To4() != nil && !ipnet.IP.IsLoopback() {
			addrs = append(addrs, ipnet.IP)
		}
	}
	if len(addrs) == 0 {
		return nil, fmt.Errorf("no non-loopback IPv4 address found")
	}
	return addrs, nil
}

// upnpDevice represents a parsed UPnP device element.
type upnpDevice struct {
	Services []upnpService `xml:"serviceList>service"`
	Devices  []upnpDevice  `xml:"deviceList>device"`
}

type upnpService struct {
	ServiceType string `xml:"serviceType"`
	ControlURL  string `xml:"controlURL"`
}

// fetchControlURL fetches the UPnP device description XML from locationURL
// and walks the device tree to find the WANIPConnection service's controlURL.
func (m *Mapper) fetchControlURL(ctx context.Context, locationURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, locationURL, nil)
	if err != nil {
		return "", fmt.Errorf("create device desc request: %w", err)
	}

	resp, err := m.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch device description: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read device description: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("device description returned %d: %s", resp.StatusCode, string(body))
	}

	var root struct {
		XMLName xml.Name   `xml:"root"`
		URLBase string     `xml:"URLBase"`
		Device  upnpDevice `xml:"device"`
	}
	if err := xml.Unmarshal(body, &root); err != nil {
		return "", fmt.Errorf("parse device description: %w", err)
	}

	controlURL := findWANIPControlURL([]upnpDevice{root.Device})
	if controlURL == "" {
		return "", fmt.Errorf("portmap: WANIPConnection service not found in device description")
	}

	// Resolve relative URLs against URLBase or the location URL.
	return resolveURL(locationURL, root.URLBase, controlURL)
}

// findWANIPControlURL recursively searches device trees for a
// WANIPConnection:1 service and returns its controlURL.
func findWANIPControlURL(devices []upnpDevice) string {
	for _, d := range devices {
		for _, s := range d.Services {
			if s.ServiceType == ssdpSearch {
				return s.ControlURL
			}
		}
		if c := findWANIPControlURL(d.Devices); c != "" {
			return c
		}
	}
	return ""
}

// resolveURL resolves a potentially relative controlURL against the
// device description's URLBase (or the location URL if URLBase is empty).
func resolveURL(locationURL, urlBase, controlURL string) (string, error) {
	cu, err := url.Parse(controlURL)
	if err != nil {
		return "", fmt.Errorf("parse controlURL %q: %w", controlURL, err)
	}
	if cu.IsAbs() {
		return controlURL, nil
	}

	baseStr := urlBase
	if baseStr == "" {
		baseStr = locationURL
	}

	base, err := url.Parse(baseStr)
	if err != nil {
		return "", fmt.Errorf("parse base URL %q: %w", baseStr, err)
	}
	resolved := base.ResolveReference(cu)
	return resolved.String(), nil
}

// parseHeader extracts a header value from a raw HTTP-like response.
// Handles folded header lines (continuation lines starting with LWS).
func parseHeader(raw, header string) string {
	prefix := strings.ToUpper(header) + ":"
	lines := strings.Split(raw, "\r\n")
	var value string
	inHeader := false

	for _, line := range lines {
		if inHeader && (strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t")) {
			value += " " + strings.TrimSpace(line)
			continue
		}
		inHeader = false

		upper := strings.ToUpper(line)
		if strings.HasPrefix(upper, prefix) {
			value = strings.TrimSpace(line[len(prefix):])
			inHeader = true
		}
	}
	return value
}

// getExternalIP fetches the external IP via a GetExternalIPAddress SOAP
// call to the WANIPConnection service.
func (m *Mapper) getExternalIP(ctx context.Context, controlURL string) (net.IP, error) {
	const getExternalIPBody = soapEnvelopePrefix + soapGetExternalIP + soapEnvelopeSuffix

	resp, err := m.soapRequest(ctx, controlURL, "urn:schemas-upnp-org:service:WANIPConnection:1#GetExternalIPAddress", getExternalIPBody)
	if err != nil {
		return nil, err
	}

	var result struct {
		Body struct {
			GetExternalIPAddressResponse struct {
				NewExternalIPAddress string `xml:"NewExternalIPAddress"`
			} `xml:"GetExternalIPAddressResponse"`
		} `xml:"Body"`
	}
	if err := xml.Unmarshal(resp, &result); err != nil {
		return nil, fmt.Errorf("parse GetExternalIPAddress: %w", err)
	}
	ipStr := result.Body.GetExternalIPAddressResponse.NewExternalIPAddress
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return nil, fmt.Errorf("invalid external IP: %q", ipStr)
	}
	return ip, nil
}

// addPortMappings adds port mappings for all configured protocols.
func (m *Mapper) addPortMappings(ctx context.Context) error {
	for _, proto := range m.cfg.Protocols {
		if err := m.addPortMapping(ctx, proto); err != nil {
			return err
		}
	}
	return nil
}

// addPortMapping sends a single AddPortMapping SOAP request.
func (m *Mapper) addPortMapping(ctx context.Context, proto string) error {
	localIP, err := m.localIP()
	if err != nil {
		return err
	}

	buf := soapBufPool.Get().(*bytes.Buffer)
	buf.Reset()
	buf.WriteString(soapEnvelopePrefix)
	buf.WriteString(soapAddPortMapping)
	buf.WriteString(`<NewRemoteHost></NewRemoteHost>`)
	buf.WriteString(`<NewExternalPort>`)
	buf.WriteString(strconv.Itoa(m.cfg.ExternalPort))
	buf.WriteString(`</NewExternalPort>`)
	buf.WriteString(`<NewProtocol>`)
	buf.WriteString(strings.ToUpper(proto))
	buf.WriteString(`</NewProtocol>`)
	buf.WriteString(`<NewInternalPort>`)
	buf.WriteString(strconv.Itoa(m.cfg.InternalPort))
	buf.WriteString(`</NewInternalPort>`)
	buf.WriteString(`<NewInternalClient>`)
	buf.WriteString(localIP)
	buf.WriteString(`</NewInternalClient>`)
	buf.WriteString(`<NewEnabled>1</NewEnabled>`)
	buf.WriteString(`<NewPortMappingDescription>aria2go</NewPortMappingDescription>`)
	buf.WriteString(`<NewLeaseDuration>`)
	buf.WriteString(strconv.Itoa(m.cfg.Lifetime))
	buf.WriteString(`</NewLeaseDuration>`)
	buf.WriteString(soapEndTag)
	buf.WriteString(`AddPortMapping>`)
	buf.WriteString(soapEnvelopeSuffix)
	body := buf.String()
	soapBufPool.Put(buf)

	m.mu.Lock()
	controlURL := m.controlURL
	m.mu.Unlock()

	_, err = m.soapRequest(ctx, controlURL, "urn:schemas-upnp-org:service:WANIPConnection:1#AddPortMapping", body)
	return err
}

// deletePortMappings removes port mappings using the current controlURL.
func (m *Mapper) deletePortMappings(ctx context.Context) error {
	m.mu.Lock()
	url := m.controlURL
	m.mu.Unlock()
	if url == "" {
		return nil
	}
	return m.deleteMappingsWithURL(ctx, url)
}

// deleteMappingsWithURL removes port mappings via the given controlURL.
func (m *Mapper) deleteMappingsWithURL(ctx context.Context, controlURL string) error {
	var errs []error
	for _, proto := range m.cfg.Protocols {
		buf := soapBufPool.Get().(*bytes.Buffer)
		buf.Reset()
		buf.WriteString(soapEnvelopePrefix)
		buf.WriteString(soapDeletePortMapping)
		buf.WriteString(`<NewRemoteHost></NewRemoteHost>`)
		buf.WriteString(`<NewExternalPort>`)
		buf.WriteString(strconv.Itoa(m.cfg.ExternalPort))
		buf.WriteString(`</NewExternalPort>`)
		buf.WriteString(`<NewProtocol>`)
		buf.WriteString(strings.ToUpper(proto))
		buf.WriteString(`</NewProtocol>`)
		buf.WriteString(soapEndTag)
		buf.WriteString(`DeletePortMapping>`)
		buf.WriteString(soapEnvelopeSuffix)
		body := buf.String()
		soapBufPool.Put(buf)

		if _, err := m.soapRequest(ctx, controlURL, "urn:schemas-upnp-org:service:WANIPConnection:1#DeletePortMapping", body); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("portmap: delete failures: %w", errors.Join(errs...))
	}
	return nil
}

// soapRequest sends a SOAP request and returns the response body.
func (m *Mapper) soapRequest(ctx context.Context, controlURL, soapAction, body string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, controlURL, bytes.NewReader([]byte(body)))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", soapContentType)
	req.Header.Set("SOAPAction", `"`+soapAction+`"`)

	resp, err := m.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("SOAP request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("SOAP request returned %d: %s", resp.StatusCode, string(respBody))
	}

	return respBody, nil
}

// localIP returns the local IPv4 address used to reach the gateway.
func (m *Mapper) localIP() (string, error) {
	if m.cfg.Interface != "" {
		iface, err := net.InterfaceByName(m.cfg.Interface)
		if err != nil {
			return "", fmt.Errorf("interface %q: %w", m.cfg.Interface, err)
		}
		addrs, err := iface.Addrs()
		if err != nil {
			return "", fmt.Errorf("interface addrs: %w", err)
		}
		for _, a := range addrs {
			if ipnet, ok := a.(*net.IPNet); ok && ipnet.IP.To4() != nil {
				return ipnet.IP.String(), nil
			}
		}
		return "", fmt.Errorf("interface %q has no IPv4 address", m.cfg.Interface)
	}

	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return "", fmt.Errorf("interface addrs: %w", err)
	}
	for _, a := range addrs {
		if ipnet, ok := a.(*net.IPNet); ok && ipnet.IP.To4() != nil && !ipnet.IP.IsLoopback() {
			return ipnet.IP.String(), nil
		}
	}
	return "", fmt.Errorf("no non-loopback IPv4 address found")
}
