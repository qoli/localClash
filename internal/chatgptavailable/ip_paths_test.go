package chatgptavailable

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestIPTargetClientPreservesCONNECTAddressHostSNIAndCertificateValidation(t *testing.T) {
	for _, test := range []struct {
		name, address, host string
		http2               bool
		wantError           bool
	}{
		{"ipv4", "192.0.2.1", "example.com", false, false},
		{"ipv6", "2001:db8::1", "example.com", false, false},
		{"ipv4-inherited-h2-ALPN", "192.0.2.1", "example.com", true, false},
		{"ipv6-inherited-h2-ALPN", "2001:db8::1", "example.com", true, false},
		{"wrong-certificate-host", "192.0.2.1", "wrong.example", false, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			observed := make(chan [2]string, 1)
			server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				observed <- [2]string{r.Host, r.TLS.ServerName}
				if r.URL.RequestURI() != "/probe?key=public" {
					t.Errorf("request URI changed: %s", r.URL.RequestURI())
				}
				if test.http2 && r.ProtoMajor != 2 {
					t.Errorf("protocol = %s; want HTTP/2", r.Proto)
				}
				w.WriteHeader(http.StatusNoContent)
			}))
			server.EnableHTTP2 = test.http2
			server.StartTLS()
			defer server.Close()
			endpoint, _ := url.Parse(server.URL + "/probe?key=public")
			backend := endpoint.Host
			port := endpoint.Port()
			endpoint.Host = net.JoinHostPort(test.host, port)
			connectTarget := net.JoinHostPort(test.address, port)
			proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodConnect || r.Host != connectTarget {
					t.Errorf("proxy request %s %s; want CONNECT %s", r.Method, r.Host, connectTarget)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				upstream, err := net.Dial("tcp", backend)
				if err != nil {
					t.Error(err)
					return
				}
				defer upstream.Close()
				connection, buffered, err := w.(http.Hijacker).Hijack()
				if err != nil {
					t.Error(err)
					return
				}
				defer connection.Close()
				_, _ = connection.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))
				go func() { _, _ = io.Copy(connection, upstream) }()
				_, _ = io.Copy(upstream, buffered)
			}))
			defer proxy.Close()
			proxyURL, _ := url.Parse(proxy.URL)
			base := server.Client()
			base.Transport.(*http.Transport).Proxy = http.ProxyURL(proxyURL)
			base.Transport.(*http.Transport).DisableKeepAlives = true
			if test.http2 {
				// A used domain transport acquires h2 ALPN defaults. Cloning it
				// and setting a custom SNI must retain HTTP/2 protocol handling.
				base.Transport.(*http.Transport).ForceAttemptHTTP2 = false
				base.Transport.(*http.Transport).TLSClientConfig.NextProtos = []string{"h2", "http/1.1"}
			}
			client, err := ipTargetClient(base, endpoint, netip.MustParseAddr(test.address))
			if err != nil {
				t.Fatal(err)
			}
			defer client.CloseIdleConnections()
			response, err := client.Get(endpoint.String())
			if test.wantError {
				if err == nil || !strings.Contains(err.Error(), "certificate") {
					t.Fatalf("want original hostname certificate rejection, response=%v err=%v", response, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			response.Body.Close()
			if got := <-observed; got != [2]string{endpoint.Host, test.host} {
				t.Fatalf("Host/SNI = %v, want %s / %s", got, endpoint.Host, test.host)
			}
			if base.Transport.(*http.Transport).TLSClientConfig.ServerName != "" || endpoint.Host != net.JoinHostPort(test.host, port) {
				t.Fatal("IP client mutated the base transport or application endpoint")
			}
		})
	}
}

func TestAdmitIPPaths(t *testing.T) {
	v4OAuth := IPPathObservation{Endpoint: "oauth", Family: "ipv4", Status: oauthRegionSupported, HTTPStatus: 401}
	v4Statsig := IPPathObservation{Endpoint: "statsig", Family: "ipv4", Status: statsigReachable, HTTPStatus: 200}
	v6OAuth := IPPathObservation{Endpoint: "oauth", Family: "ipv6", Status: oauthRegionSupported, HTTPStatus: 401}
	v6Statsig := IPPathObservation{Endpoint: "statsig", Family: "ipv6", Status: statsigReachable, HTTPStatus: 200}
	for _, test := range []struct {
		name                string
		paths               []IPPathObservation
		available, rejected bool
	}{
		{"no-IP-positive", nil, false, false},
		{"ipv4-only", []IPPathObservation{v4OAuth, v4Statsig, {Endpoint: "oauth", Family: "ipv6", Status: oauthTransportFailure}}, true, false},
		{"ipv6-only", []IPPathObservation{v6OAuth, v6Statsig}, true, false},
		{"split-families", []IPPathObservation{v4OAuth, v6Statsig}, false, false},
		{"oauth-region-veto", []IPPathObservation{v4OAuth, v4Statsig, {Endpoint: "oauth", Family: "ipv6", Status: oauthRegionUnsupported, HTTPStatus: 403, ErrorCode: oauthRegionUnsupported}}, false, true},
		{"statsig-region-veto", []IPPathObservation{v6OAuth, v6Statsig, {Endpoint: "statsig", Family: "ipv4", Status: statsigRejected, HTTPStatus: 403, ErrorCode: oauthRegionUnsupported}}, false, true},
		{"other-403-is-not-region-veto", []IPPathObservation{v4OAuth, v4Statsig, {Endpoint: "statsig", Family: "ipv6", Status: statsigRejected, HTTPStatus: 403}}, true, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			available, rejected := admitIPPaths(test.paths)
			if available != test.available || rejected != test.rejected {
				t.Fatalf("admission=%v rejection=%v", available, rejected)
			}
		})
	}
}

func TestProbeCandidateIPRejectionVetoesDomainSuccess(t *testing.T) {
	for _, test := range []struct {
		name string
		ip   IPVerification
	}{
		{"explicit-region-rejection", IPVerification{Available: true, RegionRejected: true}},
		{"no-conclusive-IP-path", IPVerification{}},
	} {
		t.Run(test.name, func(t *testing.T) {
			prober := &MihomoProber{
				options: MihomoOptions{Attempts: 1},
				probeOAuth: func(context.Context, *http.Client, string, string, time.Duration) oauthProbeResult {
					return oauthProbeResult{decision: oauthRegionSupported}
				},
				probeStatsig: func(context.Context, *http.Client, string, string, time.Duration) statsigProbeResult {
					return statsigProbeResult{decision: statsigReachable}
				},
				probeIPPaths: func(context.Context, *http.Client) IPVerification { return test.ip },
			}
			observation := prober.probeCandidate(context.Background(), Candidate{Fingerprint: "hk"}, &http.Client{})
			if observation.Available || observation.ServiceRejected != test.ip.RegionRejected || observation.IPVerification == nil {
				t.Fatalf("domain success overrode IP result: %+v", observation)
			}
		})
	}
}

func TestResolveIPsUsesHTTPSDNSContractAndHandlesMissingOrInvalidAnswers(t *testing.T) {
	for _, test := range []struct {
		name, family, body string
		want               []string
		wantError          bool
	}{
		{"all-A-and-dedup", "ipv4", `{"Status":0,"Answer":[{"type":5,"data":"alias.example"},{"type":1,"data":"192.0.2.1"},{"type":1,"data":"192.0.2.2"},{"type":1,"data":"192.0.2.1"}]}`, []string{"192.0.2.1", "192.0.2.2"}, false},
		{"AAAA", "ipv6", `{"Status":0,"Answer":[{"type":28,"data":"2001:db8::1"}]}`, []string{"2001:db8::1"}, false},
		{"no-AAAA", "ipv6", `{"Status":0}`, []string{}, false},
		{"failed-DNS", "ipv4", `{"Status":2}`, nil, true},
		{"missing-status", "ipv4", `{}`, nil, true},
		{"wrong-family", "ipv4", `{"Status":0,"Answer":[{"type":1,"data":"2001:db8::1"}]}`, nil, true},
		{"invalid-address", "ipv6", `{"Status":0,"Answer":[{"type":28,"data":"bad"}]}`, nil, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				wantType := "A"
				if test.family == "ipv6" {
					wantType = "AAAA"
				}
				if r.URL.Query().Get("name") != "auth.openai.com" || r.URL.Query().Get("type") != wantType || r.Header.Get("Accept") != "application/dns-json" {
					t.Error("unexpected DNS query", r.URL)
				}
				_, _ = io.WriteString(w, test.body)
			}))
			defer server.Close()
			prober := &MihomoProber{options: MihomoOptions{DNSOverHTTPSEndpoint: server.URL, RequestTimeout: time.Second}}
			addresses, err := prober.resolveIPs(context.Background(), server.Client(), "auth.openai.com", test.family)
			if (err != nil) != test.wantError {
				t.Fatalf("unexpected resolution error: %v", err)
			}
			if test.wantError {
				return
			}
			got := []string{}
			for _, address := range addresses {
				got = append(got, address.String())
			}
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("addresses = %v, want %v", got, test.want)
			}
		})
	}
}

func TestVerifyIPPathsTestsEveryResolvedAddressAndPersistsRejection(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		answers := []map[string]any{}
		if r.URL.Query().Get("type") == "A" {
			answers = append(answers, map[string]any{"type": 1, "data": "192.0.2.1"}, map[string]any{"type": 1, "data": "192.0.2.2"})
		} else {
			answers = append(answers, map[string]any{"type": 28, "data": "2001:db8::1"})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"Status": 0, "Answer": answers})
	}))
	defer server.Close()
	prober := &MihomoProber{
		options: MihomoOptions{DNSOverHTTPSEndpoint: server.URL, OAuthEndpoint: oauthTokenURL, StatsigEndpoint: statsigInitializeURL, Attempts: 1, RequestTimeout: time.Second},
		probeOAuth: func(_ context.Context, client *http.Client, _ string, _ string, _ time.Duration) oauthProbeResult {
			if client.Transport.(*ipTargetTransport).target == "192.0.2.2:443" {
				return oauthProbeResult{decision: oauthRegionUnsupported, httpStatus: 403, errorCode: oauthRegionUnsupported, explicitReject: true}
			}
			return oauthProbeResult{decision: oauthRegionSupported, httpStatus: 401}
		},
		probeStatsig: func(context.Context, *http.Client, string, string, time.Duration) statsigProbeResult {
			return statsigProbeResult{decision: statsigReachable, httpStatus: 200, country: "US"}
		},
	}
	verification := prober.verifyIPPaths(context.Background(), server.Client())
	if verification.Available || !verification.RegionRejected || len(verification.Resolutions) != 4 || len(verification.Paths) != 6 {
		t.Fatalf("verification = %+v", verification)
	}
	for _, resolution := range verification.Resolutions {
		if resolution.Error != "" || resolution.Attempts != 1 {
			t.Fatalf("resolution=%+v", resolution)
		}
	}
	for _, path := range verification.Paths {
		if path.Attempts != 1 || path.Status == "" {
			t.Fatalf("missing path evidence: %+v", path)
		}
	}
}

func TestStatsigRegionRejectionStopsRetryWithoutClassifyingOther403AsRegion(t *testing.T) {
	for _, test := range []struct{ name, body, code string }{
		{"country", `{"error":{"code":"unsupported_country_region_territory"}}`, oauthRegionUnsupported},
		{"other-policy", `{"error":{"code":"policy_denied"}}`, "policy_denied"},
		{"mobile-dc-response", `{"type":"dc","cf_details":{}}`, ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.WriteHeader(http.StatusForbidden)
				_, _ = io.WriteString(w, test.body)
			}))
			defer server.Close()
			prober := &MihomoProber{options: MihomoOptions{Attempts: 2, RetryDelay: time.Nanosecond, RequestTimeout: time.Second}, probeStatsig: requestStatsig}
			result := prober.probeStatsigEndpointAttempts(context.Background(), server.Client(), server.URL)
			wantCalls := 2
			if test.code == oauthRegionUnsupported {
				wantCalls = 1
			}
			if result.result.errorCode != test.code || result.attempts != wantCalls || calls != wantCalls {
				t.Fatalf("result=%+v calls=%d", result, calls)
			}
		})
	}
}
