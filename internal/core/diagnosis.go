package core

import (
	"encoding/json"
	"fmt"
	"net"
	"sort"
)

// Incident is an evidence-backed hypothesis, not an assertion about an ISP hop.
type Incident struct {
	Node       string   `json:"node"`
	Link       string   `json:"link"`
	LinkName   string   `json:"link_name"`
	StartTS    int64    `json:"start_ts"`
	EndTS      int64    `json:"end_ts"`
	Samples    int      `json:"samples"`
	Cause      string   `json:"cause"`
	Confidence string   `json:"confidence"`
	Evidence   []string `json:"evidence"`
}

func healthy(m Metric) bool { return !IsAnomaly(m) }

func nearest(rows []HistoryRow, ts int64, predicate func(HistoryRow) bool) (HistoryRow, bool) {
	var found HistoryRow
	best := int64(91)
	for _, row := range rows {
		d := abs64(row.TS - ts)
		if d < best && predicate(row) {
			found, best = row, d
		}
	}
	return found, best <= 90
}

func abs64(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}

func describeMetric(m Metric) string {
	if m.TCP != nil && m.TCP.SuccessPct < 100 {
		return fmt.Sprintf("TCP 三次连接成功率 %d%%", m.TCP.SuccessPct)
	}
	if m.Ping != nil && m.Ping.LossPct > 0 {
		return fmt.Sprintf("ICMP 五次探测丢包 %.0f%%", m.Ping.LossPct)
	}
	if m.Tail != nil && m.Tail.Path != "direct" {
		return "Tailscale 路径为 " + m.Tail.Path
	}
	return "采样异常"
}

func Diagnose(history, events []HistoryRow, mtrs []MTRRow) []Incident {
	// Normal samples are retained for 24 hours. Older incidents remain visible,
	// but must not be assigned a cause based on missing comparison data.
	ordered := append([]HistoryRow(nil), events...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].TS < ordered[j].TS })
	out := make([]Incident, 0)
	lastByLink := map[string]int{}
	for _, event := range ordered {
		key := event.Node + "\x00" + event.Metric.LinkID
		if index, exists := lastByLink[key]; exists {
			last := &out[index]
			if event.TS-last.EndTS <= 300 {
				last.EndTS = event.TS
				last.Samples++
				continue
			}
		}
		incident := Incident{Node: event.Node, Link: event.Metric.LinkID, LinkName: event.Metric.LinkName,
			StartTS: event.TS, EndTS: event.TS, Samples: 1, Cause: "证据不足，尚不能定位", Confidence: "低",
			Evidence: []string{describeMetric(event.Metric)}}
		out = append(out, incident)
		lastByLink[key] = len(out) - 1
	}
	for i := range out {
		incident := &out[i]
		var event HistoryRow
		for j := len(ordered) - 1; j >= 0; j-- {
			if ordered[j].Node == incident.Node && ordered[j].Metric.LinkID == incident.Link && ordered[j].TS == incident.EndTS {
				event = ordered[j]
				break
			}
		}
		m := event.Metric
		if ip := net.ParseIP(m.Address); ip != nil && ip.IsLoopback() {
			incident.Cause, incident.Confidence = "本机服务入口异常", "中"
			incident.Evidence = append(incident.Evidence, "故障目标是本机回环地址；需结合服务日志区分进程、监听和负载")
		} else if m.Tail != nil && m.Tail.Path == "DERP" {
			incident.Cause, incident.Confidence = "Tailscale 切换到中继", "高"
			incident.Evidence = append(incident.Evidence, "故障时该链路的 Tailscale 路径为 DERP")
		} else if m.Tail != nil && (!m.Tail.Online || m.Tail.Path == "unavailable") {
			incident.Cause, incident.Confidence = "Tailscale 对端或本地状态异常", "中"
			incident.Evidence = append(incident.Evidence, "Tailscale 对端离线或状态不可读取")
		}
		if m.Target != "" {
			alt, ok := nearest(history, event.TS, func(row HistoryRow) bool {
				return row.Node == event.Node && row.Metric.Target == m.Target && row.Metric.LinkID != m.LinkID &&
					(row.Metric.Tail != nil) != (m.Tail != nil)
			})
			if ok {
				incident.Evidence = append(incident.Evidence, fmt.Sprintf("同目标对照链路「%s」：%s", alt.Metric.LinkName, map[bool]string{true: "正常", false: describeMetric(alt.Metric)}[healthy(alt.Metric)]))
				if m.Tail == nil && alt.Metric.Tail != nil && healthy(alt.Metric) {
					incident.Cause, incident.Confidence = "公网方向或公网入口异常", "中"
					incident.Evidence = append(incident.Evidence, "同一来源到同一目标的 Tailscale 路径正常；公网故障不等于目标整机离线")
				} else if m.Tail != nil && alt.Metric.Tail == nil && healthy(alt.Metric) && incident.Cause != "Tailscale 切换到中继" {
					incident.Cause, incident.Confidence = "Tailscale 路径异常", "中"
				} else if !healthy(alt.Metric) {
					incident.Cause, incident.Confidence = "多条路径同时异常，位置待定", "低"
				}
			}
			if reverse, ok := nearest(history, event.TS, func(row HistoryRow) bool {
				return row.Node == m.Target && row.Metric.Target == event.Node && (row.Metric.Tail != nil) == (m.Tail != nil)
			}); ok {
				incident.Evidence = append(incident.Evidence, fmt.Sprintf("反向链路「%s」：%s", reverse.Metric.LinkName, map[bool]string{true: "正常", false: describeMetric(reverse.Metric)}[healthy(reverse.Metric)]))
				if healthy(reverse.Metric) && incident.Cause == "公网方向或公网入口异常" {
					incident.Cause = "单向公网路径或目标公网入口异常"
				}
			}
		}
		var closest *MTRRow
		best := int64(601)
		for j := range mtrs {
			row := &mtrs[j]
			if row.Node == event.Node && row.Link == m.LinkID && abs64(row.TS-event.TS) < best {
				closest, best = row, abs64(row.TS-event.TS)
			}
		}
		if closest != nil && best <= 600 {
			var hops []struct {
				Host string  `json:"host"`
				Loss float64 `json:"Loss%"`
				Avg  float64 `json:"Avg"`
			}
			if json.Unmarshal(closest.MTR.Hops, &hops) == nil && len(hops) > 0 {
				last := hops[len(hops)-1]
				if last.Host == m.Address {
					incident.Evidence = append(incident.Evidence, fmt.Sprintf("邻近 TCP MTR 目标跳：丢包 %.0f%%，平均 %.0f ms（仅 3 次探测）", last.Loss, last.Avg))
				}
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].EndTS > out[j].EndTS })
	if len(out) > 30 {
		out = out[:30]
	}
	return out
}
