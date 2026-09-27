package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/LynnGuo666/chainwatch/internal/core"
	"golang.org/x/crypto/bcrypt"
)

func TestSignedReportAndReplay(t *testing.T) {
	store, err := core.OpenStore(filepath.Join(t.TempDir(), "test.sqlite3"), 32, 128)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	key := strings.Repeat("a", 48)
	a := &app{cfg: core.Config{Nodes: []core.Node{{ID: "edge", Name: "Edge", SourceIP: "100.64.0.2", Key: key}}}, store: store}
	report := core.Report{NodeID: "edge", NodeName: "Edge", TS: time.Now().Unix(), Metrics: []core.Metric{{LinkID: "hub", LinkName: "Hub", Target: "hub", Address: "100.64.0.1", Protocol: "tcp", TCP: &core.TCP{SuccessPct: 100}}}}
	body, _ := json.Marshal(report)
	stamp := strconv.FormatInt(time.Now().Unix(), 10)
	nonce := strings.Repeat("a", 32)
	makeRequest := func(payload []byte, source string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(http.MethodPost, "/report", strings.NewReader(string(payload)))
		r.RemoteAddr = source
		r.Header.Set("X-Node-ID", "edge")
		r.Header.Set("X-Stamp", stamp)
		r.Header.Set("X-Nonce", nonce)
		mac := hmac.New(sha256.New, []byte(key))
		mac.Write([]byte("edge\n" + stamp + "\n" + nonce + "\n"))
		mac.Write(body)
		r.Header.Set("X-Signature", hex.EncodeToString(mac.Sum(nil)))
		w := httptest.NewRecorder()
		a.ingest(w, r)
		return w
	}
	if w := makeRequest(body, "100.64.0.2:1234"); w.Code != 204 {
		t.Fatalf("valid report: %d %s", w.Code, w.Body.String())
	}
	if w := makeRequest(body, "100.64.0.2:1234"); w.Code != 409 {
		t.Fatalf("replay: %d", w.Code)
	}
	if w := makeRequest([]byte(`{"node_id":"bad"}`), "100.64.0.2:1234"); w.Code != 401 {
		t.Fatalf("tamper: %d", w.Code)
	}
	if w := makeRequest(body, "203.0.113.2:1234"); w.Code != 403 {
		t.Fatalf("source: %d", w.Code)
	}
}

func TestDashboardRequiresPassword(t *testing.T) {
	hash, err := bcrypt.GenerateFromPassword([]byte("correct-long-password-for-test"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	a := &app{cfg: core.Config{WebUser: "admin", WebPasswordHash: string(hash)}, rate: map[string][]time.Time{}}
	handler := a.auth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }))
	wrong := httptest.NewRequest("GET", "/", nil)
	wrong.RemoteAddr = "127.0.0.1:1000"
	wrong.SetBasicAuth("admin", "wrong")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, wrong)
	if w.Code != 401 {
		t.Fatalf("wrong password: %d", w.Code)
	}
	good := httptest.NewRequest("GET", "/", nil)
	good.RemoteAddr = "127.0.0.1:1000"
	good.SetBasicAuth("admin", "correct-long-password-for-test")
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, good)
	if w.Code != 200 {
		t.Fatalf("correct password: %d", w.Code)
	}
}
