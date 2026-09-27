package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"
)

type ipInfo struct {
	IP     string `json:"ip"`
	Prefix string `json:"prefix,omitempty"`
	ASN    string `json:"asn,omitempty"`
	Holder string `json:"holder,omitempty"`
	Source string `json:"source,omitempty"`
	Error  string `json:"error,omitempty"`
}

var ipInfoCache = struct {
	sync.Mutex
	data map[string]struct {
		value ipInfo
		until time.Time
	}
}{data: make(map[string]struct {
	value ipInfo
	until time.Time
})}

func publicLookupIP(s string) bool {
	ip := net.ParseIP(s)
	return ip != nil && ip.IsGlobalUnicast() && !ip.IsPrivate() && !ip.IsLoopback() && !inTailnetIP(ip)
}

func inTailnetIP(ip net.IP) bool {
	_, network, _ := net.ParseCIDR("100.64.0.0/10")
	return network.Contains(ip)
}

func ripeData(client *http.Client, endpoint, resource string, out any) error {
	u := "https://stat.ripe.net/data/" + endpoint + "/data.json?resource=" + url.QueryEscape(resource)
	resp, err := client.Get(u)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("RIPEstat HTTP %d", resp.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(out)
}

func lookupIP(ip string) ipInfo {
	result := ipInfo{IP: ip, Source: "RIPEstat"}
	if !publicLookupIP(ip) {
		result.Error = "仅查询公网 IP"
		return result
	}
	ipInfoCache.Lock()
	if cached, ok := ipInfoCache.data[ip]; ok && time.Now().Before(cached.until) {
		ipInfoCache.Unlock()
		return cached.value
	}
	ipInfoCache.Unlock()
	client := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{Proxy: nil}}
	var network struct {
		Data struct {
			Prefix string   `json:"prefix"`
			ASNs   []string `json:"asns"`
		} `json:"data"`
	}
	if err := ripeData(client, "network-info", ip, &network); err != nil {
		result.Error = "归属信息暂不可用"
	} else {
		result.Prefix = network.Data.Prefix
		if len(network.Data.ASNs) > 0 {
			result.ASN = "AS" + network.Data.ASNs[0]
			var as struct {
				Data struct {
					Holder string `json:"holder"`
				} `json:"data"`
			}
			if err := ripeData(client, "as-overview", result.ASN, &as); err == nil {
				result.Holder = as.Data.Holder
			}
		}
	}
	if result.Prefix == "" && result.Error == "" {
		result.Error = "未找到路由前缀"
	}
	ipInfoCache.Lock()
	ipInfoCache.data[ip] = struct {
		value ipInfo
		until time.Time
	}{result, time.Now().Add(time.Hour)}
	ipInfoCache.Unlock()
	return result
}

func ipInfoHandler(w http.ResponseWriter, r *http.Request) {
	ip := r.URL.Query().Get("ip")
	if !publicLookupIP(ip) {
		http.Error(w, errors.New("invalid public IP").Error(), http.StatusBadRequest)
		return
	}
	jsonResponse(w, lookupIP(ip))
}
