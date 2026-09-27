package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
	"time"
)

type Node struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	PublicIP string `json:"public_ip,omitempty"`
	TailIP   string `json:"tailscale_ip,omitempty"`
	SourceIP string `json:"source_ip,omitempty"`
	Key      string `json:"key,omitempty"`
}

type Link struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Target        string `json:"target"`
	Address       string `json:"address"`
	Port          int    `json:"port"`
	Protocol      string `json:"protocol"` // icmp, tcp, or both
	TailscalePeer string `json:"tailscale_peer,omitempty"`
	MTR           bool   `json:"mtr"`
}

type Config struct {
	Mode              string   `json:"mode"` // hub or agent
	ID                string   `json:"id"`
	Name              string   `json:"name"`
	PublicIP          string   `json:"public_ip,omitempty"`
	TailIP            string   `json:"tailscale_ip,omitempty"`
	HubURL            string   `json:"hub_url,omitempty"`
	ListenWeb         string   `json:"listen_web,omitempty"`
	ListenIngest      string   `json:"listen_ingest,omitempty"`
	TLSCert           string   `json:"tls_cert,omitempty"`
	TLSKey            string   `json:"tls_key,omitempty"`
	WebUser           string   `json:"web_user,omitempty"`
	WebPasswordHash   string   `json:"web_password_hash,omitempty"`
	AgentKey          string   `json:"agent_key,omitempty"`
	NetworkInterfaces []string `json:"network_interfaces,omitempty"`
	Nodes             []Node   `json:"nodes,omitempty"`
	Links             []Link   `json:"links"`
	IntervalSeconds   int      `json:"interval_seconds"`
	StoragePath       string   `json:"storage_path,omitempty"`
	StorageMaxMB      int64    `json:"storage_max_mb,omitempty"`
	MinFreeMB         int64    `json:"min_free_mb,omitempty"`
}

func Load(path string) (Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	var c Config
	if err = json.Unmarshal(raw, &c); err != nil {
		return c, err
	}
	c.TLSCert = os.ExpandEnv(c.TLSCert)
	c.TLSKey = os.ExpandEnv(c.TLSKey)
	c.StoragePath = os.ExpandEnv(c.StoragePath)
	if c.ID == "" || c.Name == "" {
		return c, errors.New("id and name are required")
	}
	if c.PublicIP != "" && net.ParseIP(c.PublicIP) == nil || c.TailIP != "" && !inTailnet(net.ParseIP(c.TailIP)) {
		return c, errors.New("invalid public_ip or tailscale_ip")
	}
	if c.IntervalSeconds == 0 {
		c.IntervalSeconds = 60
	}
	if c.IntervalSeconds < 30 || c.IntervalSeconds > 3600 {
		return c, errors.New("interval_seconds must be 30..3600")
	}
	if len(c.Links) > 100 {
		return c, errors.New("at most 100 links per agent")
	}
	seen := map[string]bool{}
	for _, l := range c.Links {
		if l.ID == "" || l.Name == "" || l.Target == "" || l.Address == "" || seen[l.ID] {
			return c, fmt.Errorf("invalid or duplicate link %q", l.ID)
		}
		if l.Port < 1 || l.Port > 65535 {
			return c, fmt.Errorf("invalid port on %s", l.ID)
		}
		if l.Protocol != "icmp" && l.Protocol != "tcp" && l.Protocol != "both" {
			return c, fmt.Errorf("invalid protocol on %s", l.ID)
		}
		seen[l.ID] = true
	}
	switch c.Mode {
	case "hub":
		if c.ListenWeb == "" || c.ListenIngest == "" || c.TLSCert == "" || c.TLSKey == "" || c.WebUser == "" || c.WebPasswordHash == "" || c.StoragePath == "" {
			return c, errors.New("missing hub web, TLS, auth, or storage configuration")
		}
		if c.StorageMaxMB == 0 {
			c.StorageMaxMB = 128
		}
		if c.MinFreeMB == 0 {
			c.MinFreeMB = 1024
		}
		if c.StorageMaxMB < 32 || c.MinFreeMB < 128 {
			return c, errors.New("storage limits are too small")
		}
		host, _, e := net.SplitHostPort(c.ListenIngest)
		ip := net.ParseIP(host)
		if e != nil || ip == nil || !(ip.IsPrivate() || ip.IsLoopback() || inTailnet(ip)) {
			return c, errors.New("listen_ingest must bind to a private IP")
		}
		ids := map[string]bool{c.ID: true}
		for _, n := range c.Nodes {
			if n.ID == "" || ids[n.ID] || n.Name == "" || len(n.Key) < 48 {
				return c, fmt.Errorf("invalid agent %q", n.ID)
			}
			if n.PublicIP != "" && net.ParseIP(n.PublicIP) == nil || n.TailIP != "" && !inTailnet(net.ParseIP(n.TailIP)) {
				return c, fmt.Errorf("invalid IP on agent %q", n.ID)
			}
			ids[n.ID] = true
		}
	case "agent":
		if len(c.AgentKey) < 48 || c.HubURL == "" {
			return c, errors.New("agent key and hub_url are required")
		}
		u, e := url.Parse(c.HubURL)
		if e != nil || u.Host == "" || u.Path != "/report" {
			return c, errors.New("invalid hub_url")
		}
		if u.Scheme != "https" {
			ip := net.ParseIP(strings.Split(u.Hostname(), "%")[0])
			if u.Scheme != "http" || ip == nil || (!inTailnet(ip) && !ip.IsLoopback()) {
				return c, errors.New("plaintext hub_url is allowed only on Tailscale/loopback IP")
			}
		}
	default:
		return c, errors.New("mode must be hub or agent")
	}
	return c, nil
}

func inTailnet(ip net.IP) bool {
	tailnet := net.IPNet{IP: net.IPv4(100, 64, 0, 0), Mask: net.CIDRMask(10, 32)}
	return tailnet.Contains(ip)
}

func (c Config) Interval() time.Duration { return time.Duration(c.IntervalSeconds) * time.Second }
