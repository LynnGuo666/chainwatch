package core

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDiagnoseDirectionalPublicFailure(t *testing.T) {
	bad := Metric{LinkID: "a-public", LinkName: "A → B 公网", Target: "b", Address: "203.0.113.2", TCP: &TCP{SuccessPct: 33}}
	tail := Metric{LinkID: "a-tail", LinkName: "A → B Tailscale", Target: "b", Address: "100.64.0.2", TCP: &TCP{SuccessPct: 100}, Tail: &Tail{Path: "direct", Online: true}}
	reverse := Metric{LinkID: "b-public", LinkName: "B → A 公网", Target: "a", TCP: &TCP{SuccessPct: 100}}
	events := []HistoryRow{{Node: "a", TS: 1000, Bad: true, Metric: bad}, {Node: "a", TS: 1060, Bad: true, Metric: bad}}
	history := append(append([]HistoryRow{}, events...), HistoryRow{Node: "a", TS: 1060, Metric: tail}, HistoryRow{Node: "b", TS: 1061, Metric: reverse})
	mtr := []MTRRow{{Node: "a", Link: "a-public", TS: 1060, MTR: MTR{Hops: json.RawMessage(`[{"host":"203.0.113.2","Loss%":33,"Avg":1700}]`)}}}
	got := Diagnose(history, events, mtr)
	if len(got) != 1 || got[0].Samples != 2 || got[0].Cause != "单向公网路径或目标公网入口异常" || got[0].Confidence != "中" {
		t.Fatalf("diagnosis: %#v", got)
	}
	if !strings.Contains(strings.Join(got[0].Evidence, " "), "1700 ms") {
		t.Fatalf("missing MTR target-hop evidence: %#v", got[0].Evidence)
	}
}

func TestDiagnoseDoesNotInventCauseWithoutComparator(t *testing.T) {
	bad := Metric{LinkID: "public", LinkName: "公网", Target: "b", TCP: &TCP{SuccessPct: 0}}
	got := Diagnose(nil, []HistoryRow{{Node: "a", TS: 1000, Bad: true, Metric: bad}}, nil)
	if len(got) != 1 || got[0].Confidence != "低" || got[0].Cause != "证据不足，尚不能定位" {
		t.Fatalf("diagnosis: %#v", got)
	}
}
