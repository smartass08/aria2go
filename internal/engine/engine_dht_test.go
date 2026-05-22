package engine

import (
	"testing"

	"github.com/smartass08/aria2go/internal/config"
)

func TestEngineDHTConfigWiresIPv4PersistencePath(t *testing.T) {
	got := engineDHTConfig(6881, &config.Options{
		DHTFilePath:   "/tmp/dht.dat",
		DHTFilePath6:  "/tmp/dht6.dat",
		DHTEntryPoint: []string{"router.bittorrent.com:6881"},
	})
	if got.Addr != ":6881" {
		t.Fatalf("Addr = %q, want :6881", got.Addr)
	}
	if got.PersistTo != "/tmp/dht.dat" {
		t.Fatalf("PersistTo = %q, want /tmp/dht.dat", got.PersistTo)
	}
	if len(got.Bootstrap) != 1 || got.Bootstrap[0] != "router.bittorrent.com:6881" {
		t.Fatalf("Bootstrap = %v, want router.bittorrent.com:6881", got.Bootstrap)
	}
}

func TestEngineDHTConfigPrefersSplitEntryPointPrefs(t *testing.T) {
	got := engineDHTConfig(6881, &config.Options{
		DHTEntryPoint:     []string{"stale.example:1"},
		DHTEntryPointHost: "router.bittorrent.com",
		DHTEntryPointPort: "6881",
	})
	if len(got.Bootstrap) != 1 || got.Bootstrap[0] != "router.bittorrent.com:6881" {
		t.Fatalf("Bootstrap = %v, want router.bittorrent.com:6881", got.Bootstrap)
	}
}
