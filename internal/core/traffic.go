package core

import (
	"encoding/json"
	"errors"
	"math"
	"net"
	"time"
)

// These are budgeting estimates, not packet counters. Loopback probes do not
// traverse a metered interface. The transport allowance includes headers and
// a modest Tailscale overhead; dashboard downloads are deliberately excluded.
func EstimatedProbeBytes(r Report) int64 {
	var total int64
	for _, m := range r.Metrics {
		if ip := net.ParseIP(m.Address); ip != nil && ip.IsLoopback() {
			continue
		}
		if m.TCP != nil {
			total += 3 * 600
		}
		if m.Ping != nil {
			total += 5 * 2 * 100
		}
		if m.MTR != nil {
			var hops []json.RawMessage
			if json.Unmarshal(m.MTR.Hops, &hops) == nil {
				total += int64(len(hops)) * 3 * 2 * 100
			}
		}
	}
	return total
}

type TrafficPlan struct {
	QuotaGB     int64  `json:"quota_gb"`
	ResetDay    int    `json:"reset_day"`
	BillingMode string `json:"billing_mode"`
}

type TrafficStatus struct {
	ID string `json:"id"`
	TrafficPlan
	StartedAt             int64 `json:"started_at"`
	CycleStart            int64 `json:"cycle_start"`
	CycleEnd              int64 `json:"cycle_end"`
	ObservedRX            int64 `json:"observed_rx"`
	ObservedTX            int64 `json:"observed_tx"`
	ObservedBilled        int64 `json:"observed_billed"`
	UsedBytes             int64 `json:"used_bytes"`
	RemainingBytes        int64 `json:"remaining_bytes"`
	EstimatedMonitorBytes int64 `json:"estimated_monitor_bytes"`
	OtherBytes            int64 `json:"other_bytes"`
	Calibrated            bool  `json:"calibrated"`
	Partial               bool  `json:"partial"`
}

func billingBytes(mode string, rx, tx int64) int64 {
	switch mode {
	case "rx":
		return rx
	case "tx":
		return tx
	case "max":
		return max(rx, tx)
	default:
		return rx + tx
	}
}

func monthBoundary(year int, month time.Month, day int) time.Time {
	first := time.Date(year, month, 1, 0, 0, 0, 0, time.UTC)
	lastDay := first.AddDate(0, 1, -1).Day()
	return time.Date(year, month, min(day, lastDay), 0, 0, 0, 0, time.UTC)
}

func trafficCycle(now time.Time, day int) (time.Time, time.Time) {
	now = now.UTC()
	start := monthBoundary(now.Year(), now.Month(), day)
	if now.Before(start) {
		previous := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, -1, 0)
		start = monthBoundary(previous.Year(), previous.Month(), day)
	}
	nextMonth := time.Date(start.Year(), start.Month()+1, 1, 0, 0, 0, 0, time.UTC)
	return start, monthBoundary(nextMonth.Year(), nextMonth.Month(), day)
}

func validTrafficPlan(p TrafficPlan) bool {
	return p.QuotaGB >= 0 && p.QuotaGB <= 10_000_000 && p.ResetDay >= 1 && p.ResetDay <= 31 &&
		(p.BillingMode == "sum" || p.BillingMode == "rx" || p.BillingMode == "tx" || p.BillingMode == "max")
}

func (s *Store) SetTrafficPlan(id string, p TrafficPlan) error {
	if !validTrafficPlan(p) {
		return errors.New("invalid traffic plan")
	}
	_, err := s.DB.Exec(`UPDATE traffic_plan SET calibration_offset=CASE WHEN reset_day=? AND billing_mode=? THEN calibration_offset ELSE 0 END,calibration_cycle=CASE WHEN reset_day=? AND billing_mode=? THEN calibration_cycle ELSE 0 END,quota_gb=?,reset_day=?,billing_mode=? WHERE id=?`, p.ResetDay, p.BillingMode, p.ResetDay, p.BillingMode, p.QuotaGB, p.ResetDay, p.BillingMode, id)
	return err
}

func (s *Store) TrafficStatus(id string, now time.Time) (TrafficStatus, error) {
	out := TrafficStatus{ID: id}
	var offset, calibratedCycle int64
	err := s.DB.QueryRow(`SELECT t.started_at,p.quota_gb,p.reset_day,p.billing_mode,p.calibration_offset,p.calibration_cycle FROM traffic_tracking t JOIN traffic_plan p ON p.id=t.id WHERE t.id=?`, id).Scan(&out.StartedAt, &out.QuotaGB, &out.ResetDay, &out.BillingMode, &offset, &calibratedCycle)
	if err != nil {
		return out, err
	}
	start, end := trafficCycle(now, out.ResetDay)
	out.CycleStart, out.CycleEnd = start.Unix(), end.Unix()
	out.Partial = out.StartedAt > out.CycleStart
	err = s.DB.QueryRow(`SELECT COALESCE(SUM(rx),0),COALESCE(SUM(tx),0),COALESCE(SUM(estimated_monitor),0) FROM traffic_hourly WHERE node=? AND bucket>=? AND bucket<?`, id, out.CycleStart, out.CycleEnd).Scan(&out.ObservedRX, &out.ObservedTX, &out.EstimatedMonitorBytes)
	if err != nil {
		return out, err
	}
	if out.BillingMode != "sum" {
		out.EstimatedMonitorBytes /= 2
	}
	out.ObservedBilled = billingBytes(out.BillingMode, out.ObservedRX, out.ObservedTX)
	out.UsedBytes = out.ObservedBilled
	if calibratedCycle == out.CycleStart {
		out.Calibrated = true
		out.UsedBytes = max(0, out.UsedBytes+offset)
	}
	out.OtherBytes = max(0, out.UsedBytes-out.EstimatedMonitorBytes)
	if out.QuotaGB > 0 {
		out.RemainingBytes = max(0, out.QuotaGB*1_000_000_000-out.UsedBytes)
	}
	return out, nil
}

func (s *Store) CalibrateTraffic(id string, usedGB float64, now time.Time) error {
	if math.IsNaN(usedGB) || math.IsInf(usedGB, 0) || usedGB < 0 || usedGB > 10_000_000 {
		return errors.New("invalid calibration")
	}
	status, err := s.TrafficStatus(id, now)
	if err != nil {
		return err
	}
	offset := int64(math.Round(usedGB*1_000_000_000)) - status.ObservedBilled
	_, err = s.DB.Exec(`UPDATE traffic_plan SET calibration_offset=?,calibration_cycle=? WHERE id=?`, offset, status.CycleStart, id)
	return err
}
