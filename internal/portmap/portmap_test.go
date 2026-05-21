package portmap

import (
	"context"
	"encoding/xml"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNew(t *testing.T) {
	cfg := Config{
		InternalPort: 8080,
		ExternalPort: 8080,
		Protocols:    []string{"tcp", "udp"},
		Lifetime:     3600,
	}
	m, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	if m.cfg.InternalPort != 8080 {
		t.Errorf("InternalPort = %d, want 8080", m.cfg.InternalPort)
	}
	if m.cfg.ExternalPort != 8080 {
		t.Errorf("ExternalPort = %d, want 8080", m.cfg.ExternalPort)
	}
	if m.client == nil {
		t.Error("client is nil")
	}
	if m.done == nil {
		t.Error("done channel is nil")
	}

	_, _, ok := m.ExternalAddr()
	if ok {
		t.Error("ExternalAddr() ok should be false before Run")
	}
}

func TestNewDefaults(t *testing.T) {
	cfg := Config{
		InternalPort: 8080,
		ExternalPort: 8080,
	}
	m, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	if len(m.cfg.Protocols) != 1 || m.cfg.Protocols[0] != "tcp" {
		t.Errorf("default Protocols = %v, want [tcp]", m.cfg.Protocols)
	}
	if m.cfg.Lifetime != 3600 {
		t.Errorf("default Lifetime = %d, want 3600", m.cfg.Lifetime)
	}
}

func TestNewInvalidPorts(t *testing.T) {
	tests := []struct {
		name string
		cfg  Config
	}{
		{"internal zero", Config{InternalPort: 0, ExternalPort: 8080}},
		{"internal >65535", Config{InternalPort: 70000, ExternalPort: 8080}},
		{"external zero", Config{InternalPort: 8080, ExternalPort: 0}},
		{"external >65535", Config{InternalPort: 8080, ExternalPort: 70000}},
		{"negative", Config{InternalPort: -1, ExternalPort: 8080}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := New(tt.cfg)
			if err == nil {
				t.Error("expected error, got nil")
			}
		})
	}
}

func TestNewInvalidProtocol(t *testing.T) {
	_, err := New(Config{
		InternalPort: 8080,
		ExternalPort: 8080,
		Protocols:    []string{"icmp"},
	})
	if err == nil {
		t.Error("expected error for invalid protocol, got nil")
	}
}

func TestCloseIdempotent(t *testing.T) {
	m, err := New(Config{
		InternalPort: 8080,
		ExternalPort: 8080,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Close(); err != nil {
		t.Errorf("first Close: %v", err)
	}
	if err := m.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
}

func TestParseROOTXML(t *testing.T) {
	rootXML := `<?xml version="1.0"?>
<root xmlns="urn:schemas-upnp-org:device-1-0">
  <URLBase>http://192.168.1.1:1900/</URLBase>
  <device>
    <deviceType>urn:schemas-upnp-org:device:InternetGatewayDevice:1</deviceType>
    <deviceList>
      <device>
        <deviceType>urn:schemas-upnp-org:device:WANDevice:1</deviceType>
        <deviceList>
          <device>
            <deviceType>urn:schemas-upnp-org:device:WANConnectionDevice:1</deviceType>
            <serviceList>
              <service>
                <serviceType>urn:schemas-upnp-org:service:Layer3Forwarding:1</serviceType>
                <controlURL>/upnp/control/Layer3Forwarding</controlURL>
              </service>
              <service>
                <serviceType>urn:schemas-upnp-org:service:WANIPConnection:1</serviceType>
                <controlURL>/upnp/control/WANIPConn1</controlURL>
              </service>
            </serviceList>
          </device>
        </deviceList>
      </device>
    </deviceList>
  </device>
</root>`

	type service struct {
		ServiceType string `xml:"serviceType"`
		ControlURL  string `xml:"controlURL"`
	}
	type device struct {
		DeviceList struct {
			Devices []device `xml:"device"`
		} `xml:"deviceList"`
		ServiceList struct {
			Services []service `xml:"service"`
		} `xml:"serviceList"`
	}
	type root struct {
		URLBase string `xml:"URLBase"`
		Device  device `xml:"device"`
	}

	var r root
	if err := xml.Unmarshal([]byte(rootXML), &r); err != nil {
		t.Fatalf("xml.Unmarshal: %v", err)
	}

	var controlURL string
	var find func(d device) bool
	find = func(d device) bool {
		for _, s := range d.ServiceList.Services {
			if s.ServiceType == "urn:schemas-upnp-org:service:WANIPConnection:1" {
				controlURL = s.ControlURL
				return true
			}
		}
		for _, child := range d.DeviceList.Devices {
			if find(child) {
				return true
			}
		}
		return false
	}

	if !find(r.Device) {
		t.Fatal("WANIPConnection service not found")
	}
	if controlURL != "/upnp/control/WANIPConn1" {
		t.Errorf("controlURL = %q, want /upnp/control/WANIPConn1", controlURL)
	}
}

func TestParseSOAPResponse(t *testing.T) {
	getExternalIPResp := `<?xml version="1.0"?>
<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/" s:encodingStyle="http://schemas.xmlsoap.org/soap/encoding/">
<s:Body>
<u:GetExternalIPAddressResponse xmlns:u="urn:schemas-upnp-org:service:WANIPConnection:1">
<NewExternalIPAddress>203.0.113.1</NewExternalIPAddress>
</u:GetExternalIPAddressResponse>
</s:Body>
</s:Envelope>`

	var result struct {
		Body struct {
			GetExternalIPAddressResponse struct {
				NewExternalIPAddress string `xml:"NewExternalIPAddress"`
			} `xml:"GetExternalIPAddressResponse"`
		} `xml:"Body"`
	}
	if err := xml.Unmarshal([]byte(getExternalIPResp), &result); err != nil {
		t.Fatalf("xml.Unmarshal: %v", err)
	}
	ip := net.ParseIP(result.Body.GetExternalIPAddressResponse.NewExternalIPAddress)
	if ip == nil || ip.String() != "203.0.113.1" {
		t.Errorf("parsed IP = %v, want 203.0.113.1", ip)
	}
}

func TestParseSSDPResponse(t *testing.T) {
	resp := "HTTP/1.1 200 OK\r\n" +
		"CACHE-CONTROL: max-age=1800\r\n" +
		"EXT:\r\n" +
		"LOCATION: http://192.168.1.1:54321/dyndev/uuid:abc\r\n" +
		"SERVER: Linux/2.6 UPnP/1.0 Router/1.0\r\n" +
		"ST: urn:schemas-upnp-org:service:WANIPConnection:1\r\n" +
		"USN: uuid:abc::urn:schemas-upnp-org:service:WANIPConnection:1\r\n" +
		"\r\n"

	loc := parseHeader(resp, "LOCATION")
	if loc != "http://192.168.1.1:54321/dyndev/uuid:abc" {
		t.Errorf("LOCATION = %q, want http://192.168.1.1:54321/dyndev/uuid:abc", loc)
	}

	st := parseHeader(resp, "ST")
	if st != "urn:schemas-upnp-org:service:WANIPConnection:1" {
		t.Errorf("ST = %q, want urn:schemas-upnp-org:service:WANIPConnection:1", st)
	}
}

func TestParseSSDPResponseWithFoldedHeader(t *testing.T) {
	resp := "HTTP/1.1 200 OK\r\n" +
		"LOCATION: http://192.168.1.1:54321/\r\n" +
		"  dyndev/uuid:abc\r\n" +
		"\r\n"

	loc := parseHeader(resp, "LOCATION")
	if loc != "http://192.168.1.1:54321/ dyndev/uuid:abc" {
		t.Errorf("LOCATION = %q, want 'http://192.168.1.1:54321/ dyndev/uuid:abc'", loc)
	}
}

func TestMSEARCHRequestFormat(t *testing.T) {
	req := "M-SEARCH * HTTP/1.1\r\n" +
		"HOST: " + ssdpMulticastAddr + "\r\n" +
		"MAN: \"ssdp:discover\"\r\n" +
		"MX: 3\r\n" +
		"ST: " + ssdpSearch + "\r\n" +
		"\r\n"

	if !strings.Contains(req, "M-SEARCH * HTTP/1.1") {
		t.Error("missing M-SEARCH request line")
	}
	if !strings.Contains(req, "HOST: "+ssdpMulticastAddr) {
		t.Error("missing HOST header")
	}
	if !strings.Contains(req, "MAN: \"ssdp:discover\"") {
		t.Error("missing MAN header")
	}
	if !strings.Contains(req, "ST: "+ssdpSearch) {
		t.Error("missing ST header")
	}
}

func TestAddPortMappingBody(t *testing.T) {
	localIP := "192.168.1.100"
	body := `<?xml version="1.0"?>` +
		`<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/" s:encodingStyle="http://schemas.xmlsoap.org/soap/encoding/">` +
		`<s:Body>` +
		`<u:AddPortMapping xmlns:u="urn:schemas-upnp-org:service:WANIPConnection:1">` +
		`<NewRemoteHost></NewRemoteHost>` +
		`<NewExternalPort>8080</NewExternalPort>` +
		`<NewProtocol>TCP</NewProtocol>` +
		`<NewInternalPort>8080</NewInternalPort>` +
		`<NewInternalClient>` + localIP + `</NewInternalClient>` +
		`<NewEnabled>1</NewEnabled>` +
		`<NewPortMappingDescription>aria2go</NewPortMappingDescription>` +
		`<NewLeaseDuration>3600</NewLeaseDuration>` +
		`</u:AddPortMapping>` +
		`</s:Body>` +
		`</s:Envelope>`

	if !strings.Contains(body, "<NewExternalPort>8080</NewExternalPort>") {
		t.Error("missing NewExternalPort")
	}
	if !strings.Contains(body, "<NewProtocol>TCP</NewProtocol>") {
		t.Error("missing NewProtocol")
	}
	if !strings.Contains(body, "<NewInternalPort>8080</NewInternalPort>") {
		t.Error("missing NewInternalPort")
	}
	if !strings.Contains(body, "<NewInternalClient>"+localIP+"</NewInternalClient>") {
		t.Error("missing NewInternalClient")
	}
	if !strings.Contains(body, "<NewPortMappingDescription>aria2go</NewPortMappingDescription>") {
		t.Error("missing NewPortMappingDescription")
	}
	if !strings.Contains(body, "<NewLeaseDuration>3600</NewLeaseDuration>") {
		t.Error("missing NewLeaseDuration")
	}
}

func TestFetchControlURL(t *testing.T) {
	deviceXML := `<?xml version="1.0"?>
<root xmlns="urn:schemas-upnp-org:device-1-0">
  <URLBase>http://192.168.1.1:54321/</URLBase>
  <device>
    <deviceType>urn:schemas-upnp-org:device:InternetGatewayDevice:1</deviceType>
    <serviceList>
      <service>
        <serviceType>urn:schemas-upnp-org:service:WANIPConnection:1</serviceType>
        <controlURL>/upnp/control/WANIPConn1</controlURL>
      </service>
    </serviceList>
  </device>
</root>`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/xml")
		w.Write([]byte(deviceXML))
	}))
	defer srv.Close()

	m := &Mapper{
		client: srv.Client(),
	}

	controlURL, err := m.fetchControlURL(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("fetchControlURL: %v", err)
	}
	if controlURL != "http://192.168.1.1:54321/upnp/control/WANIPConn1" {
		t.Errorf("controlURL = %q, want http://192.168.1.1:54321/upnp/control/WANIPConn1", controlURL)
	}
}

func TestFetchControlURLNestedDevices(t *testing.T) {
	deviceXML := `<?xml version="1.0"?>
<root xmlns="urn:schemas-upnp-org:device-1-0">
  <URLBase>http://192.168.1.1:1900/</URLBase>
  <device>
    <deviceType>urn:schemas-upnp-org:device:InternetGatewayDevice:1</deviceType>
    <deviceList>
      <device>
        <deviceType>urn:schemas-upnp-org:device:WANDevice:1</deviceType>
        <deviceList>
          <device>
            <deviceType>urn:schemas-upnp-org:device:WANConnectionDevice:1</deviceType>
            <serviceList>
              <service>
                <serviceType>urn:schemas-upnp-org:service:Layer3Forwarding:1</serviceType>
                <controlURL>/upnp/control/Layer3Forwarding</controlURL>
              </service>
              <service>
                <serviceType>urn:schemas-upnp-org:service:WANIPConnection:1</serviceType>
                <controlURL>/upnp/control/WANIPConn1</controlURL>
              </service>
            </serviceList>
          </device>
        </deviceList>
      </device>
    </deviceList>
  </device>
</root>`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/xml")
		w.Write([]byte(deviceXML))
	}))
	defer srv.Close()

	m := &Mapper{
		client: srv.Client(),
	}

	controlURL, err := m.fetchControlURL(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("fetchControlURL: %v", err)
	}
	if controlURL != "http://192.168.1.1:1900/upnp/control/WANIPConn1" {
		t.Errorf("controlURL = %q, want http://192.168.1.1:1900/upnp/control/WANIPConn1", controlURL)
	}
}

func TestFetchControlURLNoURLBase(t *testing.T) {
	deviceXML := `<?xml version="1.0"?>
<root xmlns="urn:schemas-upnp-org:device-1-0">
  <device>
    <serviceList>
      <service>
        <serviceType>urn:schemas-upnp-org:service:WANIPConnection:1</serviceType>
        <controlURL>/upnp/control/WANIPConn1</controlURL>
      </service>
    </serviceList>
  </device>
</root>`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/xml")
		w.Write([]byte(deviceXML))
	}))
	defer srv.Close()

	m := &Mapper{
		client: srv.Client(),
	}

	controlURL, err := m.fetchControlURL(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("fetchControlURL: %v", err)
	}
	if !strings.HasPrefix(controlURL, srv.URL) {
		t.Errorf("controlURL = %q, want prefix %q", controlURL, srv.URL)
	}
}

func TestFetchControlURLMissing(t *testing.T) {
	deviceXML := `<?xml version="1.0"?>
<root xmlns="urn:schemas-upnp-org:device-1-0">
  <device>
    <serviceList>
      <service>
        <serviceType>urn:schemas-upnp-org:service:Layer3Forwarding:1</serviceType>
        <controlURL>/upnp/control/Layer3Forwarding</controlURL>
      </service>
    </serviceList>
  </device>
</root>`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/xml")
		w.Write([]byte(deviceXML))
	}))
	defer srv.Close()

	m := &Mapper{
		client: srv.Client(),
	}

	_, err := m.fetchControlURL(context.Background(), srv.URL)
	if err == nil {
		t.Fatal("expected error for missing WANIPConnection service, got nil")
	}
}

func TestResolveURL(t *testing.T) {
	tests := []struct {
		name        string
		locationURL string
		urlBase     string
		controlURL  string
		want        string
	}{
		{
			name:        "relative with URLBase",
			locationURL: "http://192.168.1.1:54321/dyndev/uuid:abc",
			urlBase:     "http://192.168.1.1:54321/",
			controlURL:  "/upnp/control/WANIPConn1",
			want:        "http://192.168.1.1:54321/upnp/control/WANIPConn1",
		},
		{
			name:        "relative with location as base",
			locationURL: "http://192.168.1.1:54321/dyndev/uuid:abc",
			urlBase:     "",
			controlURL:  "/upnp/control/WANIPConn1",
			want:        "http://192.168.1.1:54321/upnp/control/WANIPConn1",
		},
		{
			name:        "absolute controlURL",
			locationURL: "http://192.168.1.1:54321/xml",
			urlBase:     "",
			controlURL:  "http://10.0.0.1:5000/control",
			want:        "http://10.0.0.1:5000/control",
		},
		{
			name:        "URLBase with trailing path",
			locationURL: "http://192.168.1.1:54321/dyndev/uuid:abc",
			urlBase:     "http://192.168.1.1:54321/prefix/",
			controlURL:  "/upnp/control/WANIPConn1",
			want:        "http://192.168.1.1:54321/upnp/control/WANIPConn1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveURL(tt.locationURL, tt.urlBase, tt.controlURL)
			if err != nil {
				t.Fatalf("resolveURL: %v", err)
			}
			if got != tt.want {
				t.Errorf("resolveURL = %q, want %q", got, tt.want)
			}
		})
	}
}
