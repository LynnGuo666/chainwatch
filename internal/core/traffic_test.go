package core

import (
	"path/filepath"
	"testing"
	"time"
)

func TestTrafficStartsWithoutHistoricalBytesAndCalibrationContinues(t *testing.T) {
	s, err := OpenStore(filepath.Join(t.TempDir(), "traffic.sqlite3"), 32, 128)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	now := time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)
	if err := s.InitTraffic("node", now.Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	for i, counter := range []int64{1000, 1200, 1400} {
		r := Report{NodeID: "node", NodeName: "Node", TS: now.Add(time.Duration(i) * time.Minute).Unix(), System: System{NICRX: counter, NICTX: counter}}
		if err := s.Insert(r); err != nil {
			t.Fatal(err)
		}
		if i == 1 {
			if err := s.CalibrateTraffic("node", 10.5, now.Add(time.Minute)); err != nil {
				t.Fatal(err)
			}
		}
	}
	status, err := s.TrafficStatus("node", now.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if status.ObservedRX != 400 || status.ObservedTX != 400 || status.UsedBytes != 10_500_000_400 || !status.Calibrated || !status.Partial {
		t.Fatalf("unexpected calibrated status: %+v", status)
	}
	if err := s.SetTrafficPlan("node", TrafficPlan{QuotaGB: 2000, ResetDay: 1, BillingMode: "sum"}); err != nil {
		t.Fatal(err)
	}
	status, _ = s.TrafficStatus("node", now.Add(2*time.Minute))
	if !status.Calibrated || status.QuotaGB != 2000 || status.RemainingBytes != 2_000_000_000_000-status.UsedBytes {
		t.Fatalf("quota edit lost calibration: %+v", status)
	}
	if err := s.SetTrafficPlan("node", TrafficPlan{QuotaGB: 2000, ResetDay: 1, BillingMode: "tx"}); err != nil {
		t.Fatal(err)
	}
	status, _ = s.TrafficStatus("node", now.Add(2*time.Minute))
	if status.Calibrated || status.UsedBytes != 400 {
		t.Fatalf("billing mode change retained stale calibration: %+v", status)
	}
}

func TestTrafficCycleClampsMonthEnd(t *testing.T) {
	start, end := trafficCycle(time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC), 31)
	if start != time.Date(2026, 2, 28, 0, 0, 0, 0, time.UTC) || end != time.Date(2026, 3, 31, 0, 0, 0, 0, time.UTC) {
		t.Fatalf("wrong cycle: %s–%s", start, end)
	}
}

func TestEstimatedProbeBytesIgnoresLoopback(t *testing.T) {
	r := Report{Metrics: []Metric{{Address: "127.0.0.1", TCP: &TCP{}}, {Address: "100.64.0.2", TCP: &TCP{}, Ping: &Ping{}}}}
	if got := EstimatedProbeBytes(r); got != 2800 {
		t.Fatalf("estimate = %d", got)
	}
}
