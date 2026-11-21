package poolprepare

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	youtubeOrigin  = "https://www.youtube.com"
	bulkVideoID    = "aqz-KE-bpKQ" // Blender Foundation's Big Buck Bunny, 2160p60
	bulkBlockBytes = 2 << 20
	playerVersion  = "1.65.10"
	playerAgent    = "com.google.android.apps.youtube.vr.oculus/" + playerVersion + " (Linux; U; Android 12L; eureka-user Build/SQ3A.220605.009.A1) gzip"
)

var visitorPattern = regexp.MustCompile(`"(?:visitorData|VISITOR_DATA)"\s*:\s*"([^"]+)"`)
var mediaRange = regexp.MustCompile(`^bytes ([0-9]+)-([0-9]+)/([0-9]+)$`)

func googleVideoURL(u *url.URL) bool {
	return u != nil && u.Scheme == "https" && strings.HasSuffix(u.Hostname(), ".googlevideo.com") && u.Path == "/videoplayback"
}

// Resolve anonymously for each candidate. Media URLs are short-lived and may
// be bound to the exit IP. Never persist URLs, visitor data or response bodies.
func resolveBulkVideo(ctx context.Context, client *http.Client) (*url.URL, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, youtubeOrigin+"/watch?v="+bulkVideoID, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, errors.New("watch page unavailable")
	}
	page, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	visitor := visitorPattern.FindSubmatch(page)
	if visitor == nil {
		return nil, errors.New("anonymous player session unavailable")
	}
	device := map[string]any{
		"clientName": "ANDROID_VR", "clientVersion": playerVersion, "userAgent": playerAgent,
		"deviceMake": "Oculus", "deviceModel": "Quest 3", "androidSdkVersion": 32,
		"osName": "Android", "osVersion": "12L", "hl": "en",
	}
	payload, err := json.Marshal(map[string]any{
		"context":         map[string]any{"client": device},
		"videoId":         bulkVideoID,
		"playbackContext": map[string]any{"contentPlaybackContext": map[string]string{"html5Preference": "HTML5_PREF_WANTS"}},
	})
	if err != nil {
		return nil, err
	}
	req, err = http.NewRequestWithContext(ctx, http.MethodPost, youtubeOrigin+"/youtubei/v1/player?prettyPrint=false", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", youtubeOrigin)
	req.Header.Set("User-Agent", playerAgent)
	req.Header.Set("X-Youtube-Client-Name", "28")
	req.Header.Set("X-Youtube-Client-Version", playerVersion)
	req.Header.Set("X-Goog-Visitor-Id", string(visitor[1]))
	player, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer player.Body.Close()
	var data struct {
		PlayabilityStatus struct{ Status string }
		StreamingData     struct {
			AdaptiveFormats []struct {
				Itag int
				URL  string
			}
		}
	}
	if player.StatusCode != http.StatusOK || json.NewDecoder(io.LimitReader(player.Body, 4<<20)).Decode(&data) != nil || data.PlayabilityStatus.Status != "OK" {
		return nil, errors.New("public video unavailable")
	}
	for _, format := range data.StreamingData.AdaptiveFormats {
		u, err := url.Parse(format.URL)
		if format.Itag == 315 && err == nil && googleVideoURL(u) {
			return u, nil
		}
	}
	return nil, errors.New("video format unavailable")
}

func bulkRange(ctx context.Context, client *http.Client, target *url.URL, offset int) (*url.URL, float64, error) {
	end := offset + bulkBlockBytes - 1
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Range", "bytes="+strconv.Itoa(offset)+"-"+strconv.Itoa(end))
	req.Header.Set("Accept-Encoding", "identity")
	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	media, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	match := mediaRange.FindStringSubmatch(resp.Header.Get("Content-Range"))
	if resp.StatusCode != http.StatusPartialContent || !strings.HasPrefix(media, "video/") || match == nil || match[1] != strconv.Itoa(offset) || match[2] != strconv.Itoa(end) {
		return nil, 0, errors.New("invalid video range")
	}
	total, err := strconv.ParseInt(match[3], 10, 64)
	if err != nil || total < 3*bulkBlockBytes {
		return nil, 0, errors.New("incomplete media object")
	}
	n, err := io.Copy(io.Discard, io.LimitReader(resp.Body, bulkBlockBytes+1))
	if err != nil || n != bulkBlockBytes {
		return nil, 0, errors.New("incomplete video range")
	}
	return resp.Request.URL, float64(n) * 8 / time.Since(start).Seconds() / 1e6, nil
}

func measureBulkVideo(ctx context.Context, client *http.Client) (float64, string) {
	target, err := resolveBulkVideo(ctx, client)
	if err != nil {
		return 0, "resolve"
	}
	media := *client
	media.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if !googleVideoURL(req.URL) || len(via) >= 10 {
			return http.ErrUseLastResponse
		}
		return nil
	}
	var steady float64
	for part := 0; part < 3; part++ {
		var rate float64
		target, rate, err = bulkRange(ctx, &media, target, part*bulkBlockBytes)
		if err != nil {
			return 0, "range"
		}
		if part == 1 || (part == 2 && rate < steady) {
			steady = rate
		}
	}
	return steady, ""
}
