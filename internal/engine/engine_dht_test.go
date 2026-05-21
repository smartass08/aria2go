package engine

import (
	"testing"

	"github.com/smartass08/aria2go/internal/config"
)

func TestEngineDHTConfigWiresIPv4PersistencePath(t *testing.T) {
	got := engineDHTConfig(6881, &config.Options{
		DHTFilePath:  "/tmp/dht.dat",
		DHTFilePath6: "/tmp/dht6.dat",
	})
	if got.Addr != ":6881" {
		t.Fatalf("Addr = %q, want :6881", got.Addr)
	}
	if got.PersistTo != "/tmp/dht.dat" {
		t.Fatalf("PersistTo = %q, want /tmp/dht.dat", got.PersistTo)
	}
}
