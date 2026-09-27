package main

import "testing"

func TestPublicLookupIP(t *testing.T) {
	for ip, want := range map[string]bool{
		"179.253.255.98": true,
		"99.88.84.227":   true,
		"100.115.131.40": false,
		"127.0.0.1":      false,
		"192.168.1.1":    false,
		"not-an-ip":      false,
	} {
		if got := publicLookupIP(ip); got != want {
			t.Errorf("publicLookupIP(%q) = %v, want %v", ip, got, want)
		}
	}
}
