package main

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/LynnGuo666/chainwatch/internal/core"
	"golang.org/x/crypto/bcrypt"
)

//go:embed web/out
var site embed.FS

type app struct {
	cfg        core.Config
	store      *core.Store
	httpClient *http.Client
	lastMTR    map[string]time.Time
	mu         sync.Mutex
	rate       map[string][]time.Time
	rateMu     sync.Mutex
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "gen-key" {
		b := make([]byte, 48)
		if _, err := rand.Read(b); err != nil {
			log.Fatal(err)
		}
		fmt.Println(base64.RawURLEncoding.EncodeToString(b))
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "hash-password" {
		b, err := io.ReadAll(io.LimitReader(os.Stdin, 4096))
		if err != nil {
			log.Fatal(err)
		}
		p := strings.TrimSpace(string(b))
		if len(p) < 20 {
			log.Fatal("password must be at least 20 characters")
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(p), bcrypt.DefaultCost)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Println(string(hash))
		return
	}
	flags := flag.NewFlagSet("chainwatch", flag.ExitOnError)
	path := flags.String("config", "config.json", "configuration file")
	_ = flags.Parse(os.Args[1:])
	cfg, err := core.Load(*path)
	if err != nil {
		log.Fatal(err)
	}
	a := &app{cfg: cfg, lastMTR: map[string]time.Time{}, rate: map[string][]time.Time{}, httpClient: &http.Client{Timeout: 15 * time.Second, Transport: &http.Transport{Proxy: nil}}}
	if cfg.Mode == "hub" {
		a.store, err = core.OpenStore(cfg.StoragePath, cfg.StorageMaxMB, cfg.MinFreeMB)
		if err != nil {
			log.Fatal(err)
		}
		defer a.store.Close()
		go a.collectLoop()
		go a.cleanupLoop()
		go a.serveIngest()
		a.serveWeb()
		return
	}
	a.agentLoop()
}

func (a *app) collect() core.Report {
	r := core.Report{NodeID: a.cfg.ID, NodeName: a.cfg.Name, TS: time.Now().Unix(), System: core.HostSystem(a.cfg.NetworkInterfaces), Metrics: make([]core.Metric, len(a.cfg.Links))}
	sem := make(chan struct{}, 4)
	var wg sync.WaitGroup
	for i, l := range a.cfg.Links {
		wg.Add(1)
		go func(i int, l core.Link) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			r.Metrics[i] = core.ProbeLink(l)
		}(i, l)
	}
	wg.Wait()
	mtrCount := 0
	for i, l := range a.cfg.Links {
		if !l.MTR || mtrCount >= 2 {
			continue
		}
		m := &r.Metrics[i]
		interval := 6 * time.Hour
		reason := "baseline"
		if core.IsAnomaly(*m) {
			interval = 30 * time.Minute
			reason = "anomaly"
		}
		a.mu.Lock()
		due := time.Since(a.lastMTR[l.ID]) >= interval
		a.mu.Unlock()
		if due {
			m.MTR = core.RunMTR(l.Address, l.Port, reason)
			if m.MTR != nil {
				a.mu.Lock()
				a.lastMTR[l.ID] = time.Now()
				a.mu.Unlock()
				mtrCount++
			}
		}
	}
	return r
}
func (a *app) collectLoop() {
	for {
		start := time.Now()
		r := a.collect()
		if err := a.store.Insert(r); err != nil && !errors.Is(err, core.ErrDuplicate) {
			log.Printf("local insert: %v", err)
		}
		time.Sleep(max(time.Second, a.cfg.Interval()-time.Since(start)))
	}
}
func (a *app) cleanupLoop() {
	for {
		time.Sleep(time.Hour)
		if err := a.store.Cleanup(); err != nil {
			log.Printf("cleanup: %v", err)
		}
	}
}
func (a *app) agentLoop() {
	for {
		start := time.Now()
		r := a.collect()
		if err := a.send(r); err != nil {
			log.Printf("report: %v", err)
		}
		time.Sleep(max(time.Second, a.cfg.Interval()-time.Since(start)))
	}
}
func (a *app) send(r core.Report) error {
	body, err := json.Marshal(r)
	if err != nil {
		return err
	}
	stamp := strconv.FormatInt(time.Now().Unix(), 10)
	nonceRaw := make([]byte, 16)
	if _, err = rand.Read(nonceRaw); err != nil {
		return err
	}
	nonce := hex.EncodeToString(nonceRaw)
	mac := hmac.New(sha256.New, []byte(a.cfg.AgentKey))
	mac.Write([]byte(a.cfg.ID + "\n" + stamp + "\n" + nonce + "\n"))
	mac.Write(body)
	req, err := http.NewRequest(http.MethodPost, a.cfg.HubURL, strings.NewReader(string(body)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Node-ID", a.cfg.ID)
	req.Header.Set("X-Stamp", stamp)
	req.Header.Set("X-Nonce", nonce)
	req.Header.Set("X-Signature", hex.EncodeToString(mac.Sum(nil)))
	resp, err := a.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("hub returned %s", resp.Status)
	}
	return nil
}
func (a *app) serveIngest() {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /report", a.ingest)
	srv := &http.Server{Addr: a.cfg.ListenIngest, Handler: mux, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 20 * time.Second, MaxHeaderBytes: 8192}
	log.Printf("ingest listening on %s", srv.Addr)
	log.Fatal(srv.ListenAndServe())
}
func (a *app) ingest(w http.ResponseWriter, r *http.Request) {
	nodeID := r.Header.Get("X-Node-ID")
	stamp := r.Header.Get("X-Stamp")
	nonce := r.Header.Get("X-Nonce")
	signature := r.Header.Get("X-Signature")
	var node *core.Node
	for i := range a.cfg.Nodes {
		if a.cfg.Nodes[i].ID == nodeID {
			node = &a.cfg.Nodes[i]
			break
		}
	}
	if node == nil {
		http.Error(w, "unauthorized", 401)
		return
	}
	if node.SourceIP != "" {
		remote, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil || remote != node.SourceIP {
			http.Error(w, "source not allowed", 403)
			return
		}
	}
	ts, err := strconv.ParseInt(stamp, 10, 64)
	if err != nil || ts < time.Now().Add(-2*time.Minute).Unix() || ts > time.Now().Add(2*time.Minute).Unix() || len(nonce) != 32 || len(signature) != 64 {
		http.Error(w, "invalid signature", 401)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 256<<10))
	if err != nil {
		http.Error(w, "body too large", 413)
		return
	}
	mac := hmac.New(sha256.New, []byte(node.Key))
	mac.Write([]byte(nodeID + "\n" + stamp + "\n" + nonce + "\n"))
	mac.Write(body)
	sig, err := hex.DecodeString(signature)
	if err != nil || !hmac.Equal(sig, mac.Sum(nil)) {
		http.Error(w, "invalid signature", 401)
		return
	}
	var report core.Report
	if json.Unmarshal(body, &report) != nil || report.NodeID != nodeID || report.NodeName != node.Name || report.TS < time.Now().Add(-2*time.Minute).Unix() || report.TS > time.Now().Add(2*time.Minute).Unix() || len(report.Metrics) > 100 {
		http.Error(w, "invalid report", 400)
		return
	}
	if err = a.store.UseNonce(nodeID, nonce, ts); err != nil {
		http.Error(w, "replayed report", 409)
		return
	}
	if err = a.store.Insert(report); err != nil {
		if errors.Is(err, core.ErrDuplicate) {
			http.Error(w, "duplicate report", 409)
		} else {
			log.Printf("ingest insert: %v", err)
			http.Error(w, "storage error", 500)
		}
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
func (a *app) allow(ip string) bool {
	a.rateMu.Lock()
	defer a.rateMu.Unlock()
	now := time.Now()
	old := a.rate[ip]
	fresh := old[:0]
	for _, t := range old {
		if now.Sub(t) < 5*time.Minute {
			fresh = append(fresh, t)
		}
	}
	a.rate[ip] = fresh
	return len(fresh) < 10
}
func (a *app) fail(ip string) {
	a.rateMu.Lock()
	defer a.rateMu.Unlock()
	if len(a.rate) > 10000 {
		a.rate = map[string][]time.Time{}
	}
	a.rate[ip] = append(a.rate[ip], time.Now())
}
func (a *app) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip, _, _ := net.SplitHostPort(r.RemoteAddr)
		if !a.allow(ip) {
			http.Error(w, "rate limit", 429)
			return
		}
		user, pass, ok := r.BasicAuth()
		if !ok || user != a.cfg.WebUser || bcrypt.CompareHashAndPassword([]byte(a.cfg.WebPasswordHash), []byte(pass)) != nil {
			a.fail(ip)
			w.Header().Set("WWW-Authenticate", `Basic realm="Chainwatch"`)
			http.Error(w, "authentication required", 401)
			return
		}
		a.rateMu.Lock()
		delete(a.rate, ip)
		a.rateMu.Unlock()
		w.Header().Set("Strict-Transport-Security", "max-age=31536000")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; font-src 'self' data:; connect-src 'self'")
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}
func jsonResponse(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(v)
}
func (a *app) serveWeb() {
	sub, err := fs.Sub(site, "web/out")
	if err != nil {
		log.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/topology", func(w http.ResponseWriter, r *http.Request) {
		nodes := []core.Node{{ID: a.cfg.ID, Name: a.cfg.Name}}
		for _, n := range a.cfg.Nodes {
			nodes = append(nodes, core.Node{ID: n.ID, Name: n.Name})
		}
		jsonResponse(w, nodes)
	})
	mux.HandleFunc("GET /api/snapshot", func(w http.ResponseWriter, r *http.Request) { jsonResponse(w, a.store.Snapshot()) })
	mux.HandleFunc("GET /api/history", func(w http.ResponseWriter, r *http.Request) {
		h, _ := strconv.Atoi(r.URL.Query().Get("hours"))
		v, e := a.store.History(h)
		if e != nil {
			http.Error(w, "query error", 500)
			return
		}
		jsonResponse(w, v)
	})
	mux.HandleFunc("GET /api/hourly", func(w http.ResponseWriter, r *http.Request) {
		d, _ := strconv.Atoi(r.URL.Query().Get("days"))
		v, e := a.store.Hourly(d)
		if e != nil {
			http.Error(w, "query error", 500)
			return
		}
		jsonResponse(w, v)
	})
	mux.HandleFunc("GET /api/events", func(w http.ResponseWriter, r *http.Request) {
		v, e := a.store.Events(100)
		if e != nil {
			http.Error(w, "query error", 500)
			return
		}
		jsonResponse(w, v)
	})
	mux.HandleFunc("GET /api/mtr", func(w http.ResponseWriter, r *http.Request) {
		v, e := a.store.MTR(30)
		if e != nil {
			http.Error(w, "query error", 500)
			return
		}
		jsonResponse(w, v)
	})
	mux.Handle("/", http.FileServer(http.FS(sub)))
	srv := &http.Server{Addr: a.cfg.ListenWeb, Handler: a.auth(mux), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 8192}
	log.Printf("dashboard listening on %s", srv.Addr)
	log.Fatal(srv.ListenAndServeTLS(a.cfg.TLSCert, a.cfg.TLSKey))
}
