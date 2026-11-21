package poolprepare

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"localclash/internal/capability"
	"localclash/internal/poolcontract"
)

func TestYouTubeWarmRanges(t *testing.T) {
	for _, mode := range []string{"complete", "short", "wrong-range", "html", "forbidden", "untrusted-redirect"} {
		t.Run(mode, func(t *testing.T) {
			part := 0
			client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				res := &http.Response{Request: req, Header: make(http.Header), StatusCode: 200}
				var body io.Reader
				switch req.URL.Path {
				case "/watch":
					body = strings.NewReader(`{"visitorData":"anonymous"}`)
				case "/youtubei/v1/player":
					if req.Header.Get("X-Goog-Visitor-Id") != "anonymous" {
						t.Fatal("missing visitor context")
					}
					body = strings.NewReader(`{"playabilityStatus":{"status":"OK"},"streamingData":{"adaptiveFormats":[{"itag":315,"url":"https://first.googlevideo.com/videoplayback"}]}}`)
				case "/videoplayback":
					if req.URL.Host == "first.googlevideo.com" {
						res.StatusCode = 302
						redirect := "https://edge.googlevideo.com/videoplayback"
						if mode == "untrusted-redirect" {
							redirect = "https://example.org/videoplayback"
						}
						res.Header.Set("Location", redirect)
						body = strings.NewReader("")
						break
					}
					if req.URL.Host != "edge.googlevideo.com" {
						t.Fatal("unexpected redirect was followed")
					}
					offset := part * bulkBlockBytes
					if req.Header.Get("Range") != fmt.Sprintf("bytes=%d-%d", offset, offset+bulkBlockBytes-1) {
						t.Fatal("range did not advance")
					}
					part++
					res.StatusCode = 206
					res.Header.Set("Content-Type", "video/webm")
					res.Header.Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", offset, offset+bulkBlockBytes-1, 3*bulkBlockBytes))
					size := bulkBlockBytes
					if part == 2 {
						switch mode {
						case "wrong-range":
							res.Header.Set("Content-Range", "bytes 0-1/2")
						case "short":
							size--
						case "html":
							res.Header.Set("Content-Type", "text/html")
						case "forbidden":
							res.StatusCode = 403
						}
					}
					body = bytes.NewReader(make([]byte, size))
				default:
					t.Fatalf("unexpected request %s", req.URL.Path)
				}
				res.Body = io.NopCloser(body)
				return res, nil
			})}
			rate, stage := measureBulkVideo(t.Context(), client)
			if mode == "complete" {
				if rate <= 0 || stage != "" || part != 3 {
					t.Fatalf("rate=%v stage=%s parts=%d", rate, stage, part)
				}
			} else if rate != 0 || stage != "range" {
				t.Fatalf("invalid sample received score %v, stage %s", rate, stage)
			}
		})
	}
}

func TestBulkRotationAndStability(t *testing.T) {
	early := time.Unix(1, 0)
	nodes := []autoCandidate{{name: "steady"}, {name: "unseen"}, {name: "old failure"}}
	history := map[string]bulkObservation{"steady": {Checked: early, Mbps: 25, PreviousMbps: 22}, "old failure": {Checked: early}}
	next := bulkCandidates(nodes, history)
	if len(next) != 2 || next[0].name != "steady" || next[1].name != "unseen" {
		t.Fatalf("short subscription rotation: %+v", next)
	}
	if got := bulkCandidates(nodes[:1], history); len(got) != 1 || got[0].name != "steady" {
		t.Fatal("small completed subscription stopped rechecking")
	}
	for i := 0; i < poolcontract.BulkMinimum; i++ {
		name := fmt.Sprint(i)
		nodes = append(nodes, autoCandidate{name: name})
		history[name] = bulkObservation{Checked: early.Add(time.Hour), Mbps: 10}
	}
	if cold := bulkCandidates(nodes, nil); len(cold) != poolcontract.BulkMinimum {
		t.Fatal("cold start size", len(cold))
	}
	next = bulkCandidates(nodes, history)
	if len(next) != 2 || next[0].name != "steady" || next[1].name != "unseen" {
		t.Fatalf("full pool rotation: %+v", next)
	}
	tiers := []bulkObservation{{Mbps: 21, PreviousMbps: 24}, {Mbps: 8, PreviousMbps: 40}, {Mbps: 45}, {Mbps: 8}, {Mbps: 2}}
	if !slices.IsSortedFunc(tiers, func(a, b bulkObservation) int {
		return int(10 * (poolcontract.BulkWeight(b.tier()) - poolcontract.BulkWeight(a.tier())))
	}) {
		t.Fatal("unstable tier outranks consistently sustained tier")
	}
}

func TestSlowTransfersOnlyFillVacantPositions(t *testing.T) {
	var nodes []autoCandidate
	var names []string
	history := map[string]bulkObservation{}
	for i := 0; i < 40; i++ {
		name := fmt.Sprint(i)
		names = append(names, name)
		nodes = append(nodes, autoCandidate{name: name})
		history[name] = bulkObservation{Mbps: 2 + float64(i)/100}
	}
	history["17"] = bulkObservation{Mbps: 30, PreviousMbps: 28}
	result := capability.Result{BulkQuality: &capability.BulkQuality{}}
	selectBulkMembers(&result, nodes, names, history)
	if result.QualifiedCount != 30 || result.BulkQuality.Measured != 1 || result.BulkQuality.Reserves != 29 || result.Qualified[0] != "17" || result.Qualified[1] != "39" || result.BulkTiers["39"] != "reserve" {
		t.Fatalf("unexpected picked members: %+v", result)
	}
}
