package core

import (
	"context"
	"encoding/json"
	"math"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type Ping struct {
	LossPct  float64 `json:"loss_pct"`
	AvgMS    float64 `json:"avg_ms"`
	JitterMS float64 `json:"jitter_ms"`
}
type TCP struct {
	SuccessPct int     `json:"success_pct"`
	AvgMS      float64 `json:"avg_ms"`
}
type Tail struct {
	Path     string `json:"path"`
	Endpoint string `json:"endpoint,omitempty"`
	Online   bool   `json:"online"`
	RXBytes  int64  `json:"rx_bytes"`
	TXBytes  int64  `json:"tx_bytes"`
}
type System struct {
	Version      string  `json:"version,omitempty"`
	BuildVersion string  `json:"build_version,omitempty"`
	Load1        float64 `json:"load_1m"`
	MemoryPct    float64 `json:"memory_pct"`
	DiskPct      float64 `json:"disk_pct"`
	NICRX        int64   `json:"nic_rx_bytes"`
	NICTX        int64   `json:"nic_tx_bytes"`
	NICRXDelta   int64   `json:"nic_rx_delta"`
	NICTXDelta   int64   `json:"nic_tx_delta"`
}
type MTR struct {
	Target string          `json:"target"`
	Reason string          `json:"reason"`
	Hops   json.RawMessage `json:"hops"`
}
type Metric struct {
	LinkID   string `json:"link_id"`
	LinkName string `json:"link_name"`
	Target   string `json:"target"`
	Address  string `json:"address"`
	Protocol string `json:"protocol"`
	Ping     *Ping  `json:"ping,omitempty"`
	TCP      *TCP   `json:"tcp,omitempty"`
	Tail     *Tail  `json:"tailscale,omitempty"`
	RXDelta  int64  `json:"rx_delta,omitempty"`
	TXDelta  int64  `json:"tx_delta,omitempty"`
	MTR      *MTR   `json:"mtr,omitempty"`
}
type Report struct {
	NodeID   string   `json:"node_id"`
	NodeName string   `json:"node_name"`
	TS       int64    `json:"ts"`
	System   System   `json:"system"`
	Metrics  []Metric `json:"metrics"`
}

var lossPattern = regexp.MustCompile(`([0-9.]+)% packet loss`)
var rttPattern = regexp.MustCompile(`(?:rtt|round-trip) min/avg/max/(?:mdev|stddev) = [0-9.]+/([0-9.]+)/[0-9.]+/([0-9.]+)`)

func run(timeout time.Duration, name string, args ...string) string {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	out, _ := exec.CommandContext(ctx, name, args...).Output()
	return string(out)
}
func number(s string) float64 { v, _ := strconv.ParseFloat(s, 64); return v }
func PingHost(address string) *Ping {
	out := run(9*time.Second, "ping", "-n", "-c", "5", "-i", "0.2", "-W", "1", address)
	p := &Ping{LossPct: 100}
	if m := lossPattern.FindStringSubmatch(out); len(m) > 1 {
		p.LossPct = number(m[1])
	}
	if m := rttPattern.FindStringSubmatch(out); len(m) > 2 {
		p.AvgMS = number(m[1])
		p.JitterMS = number(m[2])
	}
	return p
}
func TCPHost(address string, port int) *TCP {
	p := &TCP{}
	success := 0
	sum := 0.0
	for i := 0; i < 3; i++ {
		start := time.Now()
		c, err := net.DialTimeout("tcp", net.JoinHostPort(address, strconv.Itoa(port)), 1500*time.Millisecond)
		if err == nil {
			_ = c.Close()
			success++
			sum += float64(time.Since(start).Microseconds()) / 1000
		}
	}
	p.SuccessPct = 100 * success / 3
	if success > 0 {
		p.AvgMS = math.Round(sum/float64(success)*10) / 10
	}
	return p
}
func TailPeer(ip string) *Tail {
	raw := run(8*time.Second, "tailscale", "status", "--json")
	var s struct {
		Peer map[string]struct {
			TailscaleIPs []string
			CurAddr      string
			Relay        string
			Online       bool
			RxBytes      int64
			TxBytes      int64
		}
	}
	if json.Unmarshal([]byte(raw), &s) != nil {
		return &Tail{Path: "unavailable"}
	}
	for _, peer := range s.Peer {
		for _, addr := range peer.TailscaleIPs {
			if addr == ip {
				path := "unknown"
				if peer.CurAddr != "" {
					path = "direct"
				} else if peer.Relay != "" {
					path = "DERP"
				}
				return &Tail{Path: path, Endpoint: peer.CurAddr, Online: peer.Online, RXBytes: peer.RxBytes, TXBytes: peer.TxBytes}
			}
		}
	}
	return &Tail{Path: "unknown"}
}
func HostSystem(interfaces []string) System {
	s := System{}
	if b, e := os.ReadFile("/proc/loadavg"); e == nil {
		parts := strings.Fields(string(b))
		if len(parts) > 0 {
			s.Load1 = number(parts[0])
		}
	}
	if b, e := os.ReadFile("/proc/meminfo"); e == nil {
		total, avail := 0.0, 0.0
		for _, line := range strings.Split(string(b), "\n") {
			if strings.HasPrefix(line, "MemTotal:") {
				total = number(strings.Fields(line)[1])
			}
			if strings.HasPrefix(line, "MemAvailable:") {
				avail = number(strings.Fields(line)[1])
			}
		}
		if total > 0 {
			s.MemoryPct = math.Round((total-avail)/total*1000) / 10
		}
	}
	var stat syscall.Statfs_t
	if syscall.Statfs("/", &stat) == nil && stat.Blocks > 0 {
		s.DiskPct = math.Round(float64(stat.Blocks-stat.Bavail)/float64(stat.Blocks)*1000) / 10
	}
	if len(interfaces) == 0 {
		interfaces = nicCandidates()
	}
	for _, iface := range interfaces {
		if filepath.Base(iface) != iface || iface == "." {
			continue
		}
		if b, e := os.ReadFile(filepath.Join("/sys/class/net", iface, "statistics/rx_bytes")); e == nil {
			v, _ := strconv.ParseInt(strings.TrimSpace(string(b)), 10, 64)
			s.NICRX += v
		}
		if b, e := os.ReadFile(filepath.Join("/sys/class/net", iface, "statistics/tx_bytes")); e == nil {
			v, _ := strconv.ParseInt(strings.TrimSpace(string(b)), 10, 64)
			s.NICTX += v
		}
	}
	return s
}
func nicCandidates() []string {
	entries, e := os.ReadDir("/sys/class/net")
	if e != nil {
		return nil
	}
	out := []string{}
	for _, entry := range entries {
		n := entry.Name()
		if n != "lo" && !strings.HasPrefix(n, "tailscale") && !strings.HasPrefix(n, "docker") && !strings.HasPrefix(n, "veth") && !strings.HasPrefix(n, "br-") && !strings.HasPrefix(n, "wg") {
			out = append(out, n)
		}
	}
	return out
}
func IsAnomaly(m Metric) bool {
	if m.Ping != nil && m.Ping.LossPct > 0 {
		return true
	}
	if m.TCP != nil && m.TCP.SuccessPct < 100 {
		return true
	}
	if m.Tail != nil && m.Tail.Path != "direct" {
		return true
	}
	return false
}
func RunMTR(address string, port int, reason string) *MTR {
	raw := run(25*time.Second, "mtr", "-n", "-T", "-P", strconv.Itoa(port), "-j", "-c", "3", address)
	var result struct {
		Report struct {
			Hubs json.RawMessage `json:"hubs"`
		} `json:"report"`
	}
	if json.Unmarshal([]byte(raw), &result) != nil || len(result.Report.Hubs) == 0 {
		return nil
	}
	return &MTR{Target: address, Reason: reason, Hops: result.Report.Hubs}
}
func ProbeLink(l Link) Metric {
	m := Metric{LinkID: l.ID, LinkName: l.Name, Target: l.Target, Address: l.Address, Protocol: l.Protocol}
	if l.Protocol == "icmp" || l.Protocol == "both" {
		m.Ping = PingHost(l.Address)
	}
	if l.Protocol == "tcp" || l.Protocol == "both" {
		m.TCP = TCPHost(l.Address, l.Port)
	}
	if l.TailscalePeer != "" {
		m.Tail = TailPeer(l.TailscalePeer)
	}
	return m
}
