package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func loadTest(t *testing.T, c Config) (Config, error) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.json")
	b, _ := json.Marshal(c)
	if err := os.WriteFile(p, b, 0600); err != nil {
		t.Fatal(err)
	}
	return Load(p)
}

func TestRejectUnsafeAgentTransport(t *testing.T) {
	c := Config{Mode: "agent", ID: "edge", Name: "Edge", HubURL: "http://203.0.113.1:8789/report", AgentKey: strings.Repeat("a", 48)}
	if _, err := loadTest(t, c); err == nil {
		t.Fatal("plaintext public hub accepted")
	}
	c.HubURL = "http://100.64.0.1:8789/report"
	if _, err := loadTest(t, c); err != nil {
		t.Fatal(err)
	}
}
func TestRejectPublicIngestListener(t *testing.T) {
	c := Config{Mode: "hub", ID: "hub", Name: "Hub", ListenWeb: "0.0.0.0:8443", ListenIngest: "0.0.0.0:8789", TLSCert: "cert", TLSKey: "key", WebUser: "admin", WebPasswordHash: "hash", StoragePath: "store.db"}
	if _, err := loadTest(t, c); err == nil {
		t.Fatal("public ingest bind accepted")
	}
	c.ListenIngest = "100.64.0.1:8789"
	if _, err := loadTest(t, c); err != nil {
		t.Fatal(err)
	}
}
