package main

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"localclash/internal/capability"
	"localclash/internal/poolcontract"

	"gopkg.in/yaml.v3"
)

func TestCandidatePoolsQualifyConcurrently(t *testing.T) {
	auto, _ := poolcontract.Lookup(poolcontract.AutoCapability)
	chatgpt, _ := poolcontract.Lookup(poolcontract.ChatGPTCapability)
	jobs := []poolPreparation{{pool: auto}, {pool: chatgpt}}
	started := make(chan string, len(jobs))
	release := make(chan struct{})
	done := make(chan struct{})
	chatgptError := errors.New("chatgpt probe failed")
	go func() {
		qualifyPreparations(jobs, func(pool poolcontract.Pool) (capability.Result, error) {
			started <- pool.Capability
			<-release
			if pool == chatgpt {
				return capability.Result{}, chatgptError
			}
			return capability.Result{Profile: pool.Capability, Probed: 1000}, nil
		})
		close(done)
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	observed := make(map[string]bool)
	for range jobs {
		select {
		case profile := <-started:
			observed[profile] = true
		case <-ctx.Done():
			close(release)
			<-done
			t.Fatal("both qualification runs must start before either finishes")
		}
	}
	close(release)
	<-done
	if !observed[auto.Capability] || !observed[chatgpt.Capability] {
		t.Fatalf("started profiles = %v", observed)
	}
	if jobs[0].result.Profile != auto.Capability || jobs[0].result.Probed != 1000 || jobs[0].err != nil || !errors.Is(jobs[1].err, chatgptError) {
		t.Fatalf("qualification results = %+v", jobs)
	}
}

func TestNextRun(t *testing.T) {
	location, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	// The next day begins daylight saving time, 23 hours after today's 08:00.
	now := time.Date(2026, time.March, 7, 8, 0, 0, 0, location)
	for period, elapsed := range map[string]time.Duration{
		"5m": 5 * time.Minute, "15m": 15 * time.Minute, "30m": 30 * time.Minute,
		"1h": time.Hour, "daily-08": 23 * time.Hour,
	} {
		got, err := nextRun(now, period)
		if err != nil || !got.Equal(now.Add(elapsed)) {
			t.Fatalf("nextRun(%q) = %v, %v; want %v", period, got, err, now.Add(elapsed))
		}
	}
	before := now.Add(-time.Second)
	if got, err := nextRun(before, "daily-08"); err != nil || !got.Equal(now) {
		t.Fatalf("next daily run before 08:00 = %v, %v; want %v", got, err, now)
	}
	if _, err := nextRun(now, "10m"); err == nil {
		t.Fatal("unknown period was accepted")
	}
}

func TestProviderDataKeepsSourceOrderAndRejectsEmptyPool(t *testing.T) {
	input, err := poolcontract.NewInput([]map[string]any{
		{"name": "first", "type": "ss", "port": 443, "password": "fixture"},
		{"name": "second", "type": "trojan", "port": 8443, "password": "fixture"},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct{ qualified, want []string }{
		{[]string{"second", "first"}, []string{"first", "second"}},
		{nil, []string{poolcontract.RejectName}},
	} {
		data, names, err := providerData(input, tt.qualified)
		if err != nil || !slices.Equal(names, tt.want) {
			t.Fatalf("provider names = %v, %v; want %v", names, err, tt.want)
		}
		var provider struct{ Proxies []struct{ Name, Type string } }
		if err := yaml.Unmarshal(data, &provider); err != nil {
			t.Fatal(err)
		}
		if len(provider.Proxies) != len(names) {
			t.Fatalf("published %d definitions for %d names", len(provider.Proxies), len(names))
		}
		for i, p := range provider.Proxies {
			if p.Name != names[i] || (p.Name == poolcontract.RejectName && p.Type != "reject") {
				t.Fatalf("published definition = %+v, want %s", p, names[i])
			}
		}
	}
	if _, _, err := providerData(input, []string{"unknown"}); err == nil {
		t.Fatal("unrecognized qualification was accepted")
	}
}

func TestMeasureDoHRequiresBothAddressFamiliesOnBothEndpoints(t *testing.T) {
	var requests atomic.Int32
	var missingIPv6 atomic.Bool
	resolver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		query, err := base64.RawURLEncoding.DecodeString(r.URL.Query().Get("dns"))
		if err != nil || len(query) < 17 || r.Header.Get("Accept") != "application/dns-message" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		kind := binary.BigEndian.Uint16(query[len(query)-4:])
		query[2], query[3], query[7] = 0x81, 0x80, 1
		addressLength := byte(4)
		if kind == 28 {
			addressLength = 16
			if missingIPv6.Load() && r.URL.Path == "/second" {
				query[7] = 0
			}
		}
		response := append(query, 0xc0, 0x0c, 0, byte(kind), 0, 1, 0, 0, 0, 60, 0, addressLength)
		w.Header().Set("Content-Type", "application/dns-message")
		w.Write(append(response, make([]byte, addressLength)...))
	}))
	defer resolver.Close()
	previous := overseasDoH
	t.Cleanup(func() { overseasDoH = previous })
	overseasDoH = []string{resolver.URL + "/first", resolver.URL + "/second"}
	if got := measureDoH(context.Background(), resolver.Client()); got <= 0 || requests.Load() != 4 {
		t.Fatalf("healthy endpoints measured %v with %d requests", got, requests.Load())
	}
	missingIPv6.Store(true)
	if got := measureDoH(context.Background(), resolver.Client()); got != 0 {
		t.Fatalf("endpoint without AAAA qualified with %v", got)
	}
}
