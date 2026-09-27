package core

import (
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestStoreReportAndCounters(t *testing.T) {
	s, err := OpenStore(filepath.Join(t.TempDir(), "metrics.sqlite3"), 32, 128)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	now := time.Now().Unix()
	report := Report{NodeID: "edge", NodeName: "Edge", TS: now, System: System{Load1: .2, NICRX: 10000, NICTX: 20000}, Metrics: []Metric{{LinkID: "hub", LinkName: "Hub", Target: "hub", Address: "100.64.0.1", Protocol: "both", Ping: &Ping{AvgMS: 5}, TCP: &TCP{SuccessPct: 100}, Tail: &Tail{Path: "direct", RXBytes: 1000, TXBytes: 2000}}}}
	if err = s.Insert(report); err != nil {
		t.Fatal(err)
	}
	if err = s.Insert(report); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("duplicate: %v", err)
	}
	report.TS++
	report.System.NICRX = 10500
	report.System.NICTX = 20700
	report.Metrics[0].Tail.RXBytes = 1500
	report.Metrics[0].Tail.TXBytes = 2600
	report.Metrics[0].Ping.LossPct = 20
	if err = s.Insert(report); err != nil {
		t.Fatal(err)
	}
	snap := s.Snapshot()
	if len(snap.Nodes) != 1 || len(snap.Links) != 1 || !snap.Links[0].Bad {
		t.Fatalf("snapshot: %#v", snap)
	}
	if snap.Nodes[0].System.NICRXDelta != 500 || snap.Nodes[0].System.NICTXDelta != 700 {
		t.Fatalf("host traffic deltas: %#v", snap.Nodes[0].System)
	}
	history, err := s.History(1)
	if err != nil || len(history) != 2 {
		t.Fatalf("history %d: %v", len(history), err)
	}
	if history[0].Metric.RXDelta != 500 || history[0].Metric.TXDelta != 600 {
		t.Fatalf("counter deltas: %#v", history[0].Metric)
	}
	hourly, err := s.Hourly(1)
	if err != nil || len(hourly) != 1 || hourly[0].RX != 500 || hourly[0].BadCount != 1 {
		t.Fatalf("hourly: %#v %v", hourly, err)
	}
	hostHourly, err := s.HostHourly(1)
	if err != nil || len(hostHourly) != 1 || hostHourly[0].RX != 500 || hostHourly[0].TX != 700 {
		t.Fatalf("host hourly: %#v %v", hostHourly, err)
	}
	events, err := s.Events(10)
	if err != nil || len(events) != 1 {
		t.Fatalf("events: %#v %v", events, err)
	}
	if err = s.UseNonce("edge", "a", now); err != nil {
		t.Fatal(err)
	}
	if err = s.UseNonce("edge", "a", now); err == nil {
		t.Fatal("replay nonce accepted")
	}
	if err = s.Cleanup(); err != nil {
		t.Fatal(err)
	}
}
