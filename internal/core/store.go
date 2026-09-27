package core

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	_ "modernc.org/sqlite"
)

var ErrDuplicate = errors.New("duplicate report")

type Store struct {
	DB        *sql.DB
	Path      string
	MaxMB     int64
	MinFreeMB int64
}

func OpenStore(path string, maxMB, minFreeMB int64) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	statements := []string{
		"PRAGMA journal_mode=WAL", "PRAGMA busy_timeout=5000", "PRAGMA journal_size_limit=4194304",
		fmt.Sprintf("PRAGMA max_page_count=%d", maxMB*1024*1024/4096),
		`CREATE TABLE IF NOT EXISTS reports(node TEXT NOT NULL, ts INTEGER NOT NULL, name TEXT NOT NULL, system BLOB NOT NULL, PRIMARY KEY(node,ts))`,
		`CREATE TABLE IF NOT EXISTS metrics(node TEXT NOT NULL, link TEXT NOT NULL, ts INTEGER NOT NULL, bad INTEGER NOT NULL, data BLOB NOT NULL, PRIMARY KEY(node,link,ts))`,
		`CREATE TABLE IF NOT EXISTS hourly(bucket INTEGER NOT NULL,node TEXT NOT NULL,link TEXT NOT NULL,n INTEGER NOT NULL,rtt_sum REAL NOT NULL,rtt_max REAL NOT NULL,loss_max REAL NOT NULL,bad_count INTEGER NOT NULL,rx INTEGER NOT NULL,tx INTEGER NOT NULL,PRIMARY KEY(bucket,node,link))`,
		`CREATE TABLE IF NOT EXISTS host_hourly(bucket INTEGER NOT NULL,node TEXT NOT NULL,n INTEGER NOT NULL,rx INTEGER NOT NULL,tx INTEGER NOT NULL,PRIMARY KEY(bucket,node))`,
		`CREATE TABLE IF NOT EXISTS mtr(node TEXT NOT NULL,link TEXT NOT NULL,ts INTEGER NOT NULL,data BLOB NOT NULL,PRIMARY KEY(node,link,ts))`,
		`CREATE TABLE IF NOT EXISTS nonces(node TEXT NOT NULL,nonce TEXT NOT NULL,ts INTEGER NOT NULL,PRIMARY KEY(node,nonce))`,
		`CREATE TABLE IF NOT EXISTS node_metadata(id TEXT PRIMARY KEY,name TEXT NOT NULL,note TEXT NOT NULL)`,
		`CREATE INDEX IF NOT EXISTS metrics_time ON metrics(ts)`,
	}
	for _, q := range statements {
		if _, err = db.Exec(q); err != nil {
			db.Close()
			return nil, err
		}
	}
	return &Store{DB: db, Path: path, MaxMB: maxMB, MinFreeMB: minFreeMB}, nil
}
func (s *Store) Close() error { return s.DB.Close() }

type NodeMetadata struct {
	Name string `json:"name"`
	Note string `json:"note"`
}

func (s *Store) NodeMetadata(id string) (NodeMetadata, error) {
	var m NodeMetadata
	err := s.DB.QueryRow(`SELECT name,note FROM node_metadata WHERE id=?`, id).Scan(&m.Name, &m.Note)
	if errors.Is(err, sql.ErrNoRows) {
		return NodeMetadata{}, nil
	}
	return m, err
}

func (s *Store) SetNodeMetadata(id string, m NodeMetadata) error {
	_, err := s.DB.Exec(`INSERT INTO node_metadata(id,name,note) VALUES(?,?,?) ON CONFLICT(id) DO UPDATE SET name=excluded.name,note=excluded.note`, id, m.Name, m.Note)
	return err
}
func (s *Store) UseNonce(node, nonce string, ts int64) error {
	_, err := s.DB.Exec(`INSERT INTO nonces VALUES(?,?,?)`, node, nonce, ts)
	return err
}
func (s *Store) Insert(r Report) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var previousSystem []byte
	var previousTS int64
	_ = tx.QueryRow(`SELECT ts,system FROM reports WHERE node=? ORDER BY ts DESC LIMIT 1`, r.NodeID).Scan(&previousTS, &previousSystem)
	if r.TS-previousTS >= 0 && r.TS-previousTS <= 600 {
		var old System
		if json.Unmarshal(previousSystem, &old) == nil {
			r.System.NICRXDelta = max(0, r.System.NICRX-old.NICRX)
			r.System.NICTXDelta = max(0, r.System.NICTX-old.NICTX)
		}
	}
	sys, _ := json.Marshal(r.System)
	if _, err = tx.Exec(`INSERT INTO reports VALUES(?,?,?,?)`, r.NodeID, r.TS, r.NodeName, sys); err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			return ErrDuplicate
		}
		return err
	}
	bucket := r.TS / 3600 * 3600
	_, err = tx.Exec(`INSERT INTO host_hourly VALUES(?,?,?,?,?) ON CONFLICT(bucket,node) DO UPDATE SET n=n+1,rx=rx+excluded.rx,tx=tx+excluded.tx`, bucket, r.NodeID, 1, r.System.NICRXDelta, r.System.NICTXDelta)
	if err != nil {
		return err
	}
	for _, m := range r.Metrics {
		var prev []byte
		var prevTS int64
		_ = tx.QueryRow(`SELECT ts,data FROM metrics WHERE node=? AND link=? ORDER BY ts DESC LIMIT 1`, r.NodeID, m.LinkID).Scan(&prevTS, &prev)
		if r.TS-prevTS >= 0 && r.TS-prevTS <= 600 && m.Tail != nil {
			var old Metric
			if json.Unmarshal(prev, &old) == nil && old.Tail != nil {
				m.RXDelta = max(0, m.Tail.RXBytes-old.Tail.RXBytes)
				m.TXDelta = max(0, m.Tail.TXBytes-old.Tail.TXBytes)
			}
		}
		bad := 0
		if IsAnomaly(m) {
			bad = 1
		}
		body, _ := json.Marshal(m)
		if _, err = tx.Exec(`INSERT INTO metrics VALUES(?,?,?,?,?)`, r.NodeID, m.LinkID, r.TS, bad, body); err != nil {
			return err
		}
		rtt, loss := 0.0, 0.0
		if m.Ping != nil {
			rtt = m.Ping.AvgMS
			loss = m.Ping.LossPct
		} else if m.TCP != nil {
			rtt = m.TCP.AvgMS
		}
		_, err = tx.Exec(`INSERT INTO hourly VALUES(?,?,?,?,?,?,?,?,?,?) ON CONFLICT(bucket,node,link) DO UPDATE SET n=n+1,rtt_sum=rtt_sum+excluded.rtt_sum,rtt_max=max(rtt_max,excluded.rtt_max),loss_max=max(loss_max,excluded.loss_max),bad_count=bad_count+excluded.bad_count,rx=rx+excluded.rx,tx=tx+excluded.tx`, bucket, r.NodeID, m.LinkID, 1, rtt, rtt, loss, bad, m.RXDelta, m.TXDelta)
		if err != nil {
			return err
		}
		if m.MTR != nil {
			data, _ := json.Marshal(m.MTR)
			if _, err = tx.Exec(`INSERT INTO mtr VALUES(?,?,?,?)`, r.NodeID, m.LinkID, r.TS, data); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

type LiveNode struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	TS     int64  `json:"ts"`
	System System `json:"system"`
}
type LiveLink struct {
	Node   string `json:"node"`
	TS     int64  `json:"ts"`
	Metric Metric `json:"metric"`
	Bad    bool   `json:"bad"`
}
type Snapshot struct {
	Nodes        []LiveNode `json:"nodes"`
	Links        []LiveLink `json:"links"`
	StorageBytes int64      `json:"storage_bytes"`
	FreeBytes    int64      `json:"free_bytes"`
}

func (s *Store) Snapshot() Snapshot {
	out := Snapshot{Nodes: []LiveNode{}, Links: []LiveLink{}}
	rows, e := s.DB.Query(`SELECT r.node,r.name,r.ts,r.system FROM reports r WHERE r.ts=(SELECT max(ts) FROM reports WHERE node=r.node)`)
	if e == nil {
		for rows.Next() {
			var x LiveNode
			var b []byte
			if rows.Scan(&x.ID, &x.Name, &x.TS, &b) == nil {
				_ = json.Unmarshal(b, &x.System)
				out.Nodes = append(out.Nodes, x)
			}
		}
		rows.Close()
	}
	rows, e = s.DB.Query(`SELECT m.node,m.ts,m.bad,m.data FROM metrics m WHERE m.ts=(SELECT max(ts) FROM metrics WHERE node=m.node AND link=m.link)`)
	if e == nil {
		for rows.Next() {
			var x LiveLink
			var b []byte
			var bad int
			if rows.Scan(&x.Node, &x.TS, &bad, &b) == nil {
				_ = json.Unmarshal(b, &x.Metric)
				x.Bad = bad == 1
				out.Links = append(out.Links, x)
			}
		}
		rows.Close()
	}
	out.StorageBytes, out.FreeBytes = s.Disk()
	return out
}

type HistoryRow struct {
	Node   string `json:"node"`
	TS     int64  `json:"ts"`
	Bad    bool   `json:"bad"`
	Metric Metric `json:"metric"`
}

func (s *Store) History(hours int) ([]HistoryRow, error) {
	if hours < 1 || hours > 24 {
		hours = 24
	}
	rows, err := s.DB.Query(`SELECT node,ts,bad,data FROM metrics WHERE ts>=? ORDER BY ts DESC LIMIT 10000`, time.Now().Unix()-int64(hours)*3600)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []HistoryRow{}
	for rows.Next() {
		var x HistoryRow
		var b []byte
		var bad int
		if rows.Scan(&x.Node, &x.TS, &bad, &b) == nil {
			_ = json.Unmarshal(b, &x.Metric)
			x.Bad = bad == 1
			out = append(out, x)
		}
	}
	return out, rows.Err()
}

func (s *Store) Events(limit int) ([]HistoryRow, error) {
	if limit < 1 || limit > 500 {
		limit = 100
	}
	rows, err := s.DB.Query(`SELECT node,ts,bad,data FROM metrics WHERE bad=1 AND ts>=? ORDER BY ts DESC LIMIT ?`, time.Now().Unix()-30*86400, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []HistoryRow{}
	for rows.Next() {
		var x HistoryRow
		var b []byte
		var bad int
		if rows.Scan(&x.Node, &x.TS, &bad, &b) == nil && json.Unmarshal(b, &x.Metric) == nil {
			x.Bad = true
			out = append(out, x)
		}
	}
	return out, rows.Err()
}

type HourlyRow struct {
	Bucket   int64   `json:"bucket"`
	Node     string  `json:"node"`
	Link     string  `json:"link"`
	Count    int     `json:"count"`
	AvgMS    float64 `json:"avg_ms"`
	MaxMS    float64 `json:"max_ms"`
	MaxLoss  float64 `json:"max_loss"`
	BadCount int     `json:"bad_count"`
	RX       int64   `json:"rx_bytes"`
	TX       int64   `json:"tx_bytes"`
}

type HostHourlyRow struct {
	Bucket int64  `json:"bucket"`
	Node   string `json:"node"`
	Count  int    `json:"count"`
	RX     int64  `json:"rx_bytes"`
	TX     int64  `json:"tx_bytes"`
}

func (s *Store) HostHourly(days int) ([]HostHourlyRow, error) {
	if days < 1 || days > 30 {
		days = 30
	}
	rows, err := s.DB.Query(`SELECT bucket,node,n,rx,tx FROM host_hourly WHERE bucket>=? ORDER BY bucket DESC LIMIT 100000`, time.Now().Unix()-int64(days)*86400)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []HostHourlyRow{}
	for rows.Next() {
		var x HostHourlyRow
		if rows.Scan(&x.Bucket, &x.Node, &x.Count, &x.RX, &x.TX) == nil {
			out = append(out, x)
		}
	}
	return out, rows.Err()
}

func (s *Store) Hourly(days int) ([]HourlyRow, error) {
	if days < 1 || days > 30 {
		days = 30
	}
	rows, err := s.DB.Query(`SELECT bucket,node,link,n,rtt_sum,rtt_max,loss_max,bad_count,rx,tx FROM hourly WHERE bucket>=? ORDER BY bucket DESC LIMIT 100000`, time.Now().Unix()-int64(days)*86400)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []HourlyRow{}
	for rows.Next() {
		var x HourlyRow
		var sum float64
		if rows.Scan(&x.Bucket, &x.Node, &x.Link, &x.Count, &sum, &x.MaxMS, &x.MaxLoss, &x.BadCount, &x.RX, &x.TX) == nil {
			if x.Count > 0 {
				x.AvgMS = sum / float64(x.Count)
			}
			out = append(out, x)
		}
	}
	return out, rows.Err()
}

type MTRRow struct {
	Node string `json:"node"`
	Link string `json:"link"`
	TS   int64  `json:"ts"`
	MTR  MTR    `json:"mtr"`
}

func (s *Store) MTR(limit int) ([]MTRRow, error) {
	if limit < 1 || limit > 100 {
		limit = 30
	}
	rows, err := s.DB.Query(`SELECT node,link,ts,data FROM mtr ORDER BY ts DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []MTRRow{}
	for rows.Next() {
		var x MTRRow
		var b []byte
		if rows.Scan(&x.Node, &x.Link, &x.TS, &b) == nil && json.Unmarshal(b, &x.MTR) == nil {
			out = append(out, x)
		}
	}
	return out, rows.Err()
}
func (s *Store) Disk() (int64, int64) {
	var size, free int64
	for _, path := range []string{s.Path, s.Path + "-wal", s.Path + "-shm"} {
		if st, e := os.Stat(path); e == nil {
			size += st.Size()
		}
	}
	var st syscall.Statfs_t
	if syscall.Statfs(filepath.Dir(s.Path), &st) == nil {
		free = int64(st.Bavail) * int64(st.Bsize)
	}
	return size, free
}
func (s *Store) Cleanup() error {
	now := time.Now().Unix()
	for _, q := range []struct {
		SQL  string
		Args []any
	}{
		{`DELETE FROM reports WHERE ts<? AND ts<>(SELECT max(r2.ts) FROM reports r2 WHERE r2.node=reports.node)`, []any{now - 86400}},
		{`DELETE FROM metrics WHERE ts<? OR (ts<? AND bad=0)`, []any{now - 30*86400, now - 86400}},
		{`DELETE FROM hourly WHERE bucket<?`, []any{now - 30*86400}},
		{`DELETE FROM host_hourly WHERE bucket<?`, []any{now - 30*86400}},
		{`DELETE FROM mtr WHERE ts<?`, []any{now - 30*86400}},
		{`DELETE FROM nonces WHERE ts<?`, []any{now - 300}},
	} {
		if _, err := s.DB.Exec(q.SQL, q.Args...); err != nil {
			return err
		}
	}
	size, free := s.Disk()
	if size > (s.MaxMB*3/4)<<20 || free < s.MinFreeMB<<20 {
		for i := 0; i < 10; i++ {
			_, _ = s.DB.Exec(`DELETE FROM metrics WHERE (node,link,ts) IN (SELECT node,link,ts FROM metrics WHERE bad=1 ORDER BY ts LIMIT 500)`)
			_, _ = s.DB.Exec(`DELETE FROM mtr WHERE (node,link,ts) IN (SELECT node,link,ts FROM mtr ORDER BY ts LIMIT 100)`)
			size, free = s.Disk()
			if size < s.MaxMB/2<<20 && free >= s.MinFreeMB<<20 {
				break
			}
		}
		_, _ = s.DB.Exec("PRAGMA wal_checkpoint(TRUNCATE)")
		_, _ = s.DB.Exec("VACUUM")
	}
	return nil
}
