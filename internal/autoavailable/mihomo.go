package autoavailable

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"localclash/internal/proxyprobe"
)

const Generate204URL = "https://cp.cloudflare.com/generate_204"

type MihomoOptions struct {
	CorePath       string
	RuntimeParent  string
	Concurrency    int
	RequestTimeout time.Duration
	Endpoint       string
}

type MihomoProber struct {
	options MihomoOptions
	probe   func(context.Context, *http.Client, string, time.Duration) g204Result
}

func RebuildWithMihomo(ctx context.Context, proxies []map[string]any, corePath, runtimeParent, snapshotPath string) (Result, error) {
	return RebuildCandidateWithMihomo(ctx, proxies, corePath, runtimeParent, snapshotPath, snapshotPath)
}

func RebuildCandidateWithMihomo(ctx context.Context, proxies []map[string]any, corePath, runtimeParent, snapshotPath, previousSnapshotPath string) (Result, error) {
	prober, err := NewMihomoProber(MihomoOptions{CorePath: corePath, RuntimeParent: runtimeParent})
	if err != nil {
		return Result{}, err
	}
	return Rebuild(ctx, proxies, prober, Options{SnapshotPath: snapshotPath, PreviousSnapshotPath: previousSnapshotPath})
}

func NewMihomoProber(options MihomoOptions) (*MihomoProber, error) {
	if strings.TrimSpace(options.CorePath) == "" {
		return nil, errors.New("automatic connectivity probe core path is required")
	}
	if strings.TrimSpace(options.RuntimeParent) == "" {
		return nil, errors.New("automatic connectivity probe runtime parent is required")
	}
	if options.Concurrency <= 0 {
		options.Concurrency = 16
	}
	if options.RequestTimeout <= 0 {
		options.RequestTimeout = 5 * time.Second
	}
	if strings.TrimSpace(options.Endpoint) == "" {
		options.Endpoint = Generate204URL
	}
	return &MihomoProber{options: options, probe: probeGenerate204}, nil
}

func (p *MihomoProber) Probe(ctx context.Context, proxies []map[string]any, candidates []Candidate) ([]Observation, error) {
	names := make([]string, len(candidates))
	for index, candidate := range candidates {
		names[index] = candidate.Name
	}
	session, err := proxyprobe.Start(ctx, proxies, names, proxyprobe.Options{
		CorePath: p.options.CorePath, RuntimeParent: p.options.RuntimeParent,
		RuntimePrefix: "auto-connectivity-probe-", ListenerPrefix: "auto-connectivity-probe",
	})
	if err != nil {
		return nil, err
	}
	defer session.Close()

	observations := make([]Observation, len(candidates))
	jobs := make(chan int)
	var wg sync.WaitGroup
	workers := p.options.Concurrency
	if workers > len(candidates) {
		workers = len(candidates)
	}
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for index := range jobs {
				client, clientErr := session.HTTPClient(index, p.options.RequestTimeout)
				if clientErr != nil {
					observations[index] = Observation{EndpointFingerprint: candidates[index].EndpointFingerprint, Error: clientErr.Error()}
					continue
				}
				observations[index] = p.probeCandidate(ctx, candidates[index], client)
			}
		}()
	}
	for index := range candidates {
		select {
		case jobs <- index:
		case <-ctx.Done():
			close(jobs)
			wg.Wait()
			return nil, ctx.Err()
		}
	}
	close(jobs)
	wg.Wait()
	return observations, nil
}

func (p *MihomoProber) probeCandidate(ctx context.Context, candidate Candidate, client *http.Client) Observation {
	started := time.Now()
	observation := Observation{EndpointFingerprint: candidate.EndpointFingerprint, Attempts: 1}
	result := p.probe(ctx, client, p.options.Endpoint, p.options.RequestTimeout)
	observation.HTTPStatus = result.httpStatus
	if result.err == nil {
		observation.Available = true
	} else {
		observation.Error = result.err.Error()
	}
	observation.Duration = time.Since(started)
	return observation
}

type g204Result struct {
	httpStatus int
	err        error
}

func probeGenerate204(parent context.Context, client *http.Client, endpoint string, timeout time.Duration) g204Result {
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return g204Result{err: fmt.Errorf("build generate_204 request: %w", err)}
	}
	req.Header.Set("User-Agent", "localClash-auto-connectivity/1")
	resp, err := client.Do(req)
	if err != nil {
		return g204Result{err: fmt.Errorf("request generate_204: %w", err)}
	}
	defer resp.Body.Close()
	result := g204Result{httpStatus: resp.StatusCode}
	if resp.StatusCode != http.StatusNoContent {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		result.err = fmt.Errorf("generate_204 returned HTTP %d", resp.StatusCode)
		return result
	}
	var one [1]byte
	if count, readErr := resp.Body.Read(one[:]); readErr != nil && !errors.Is(readErr, io.EOF) {
		result.err = fmt.Errorf("read generate_204 response body: %w", readErr)
		return result
	} else if count != 0 {
		result.err = errors.New("generate_204 returned a non-empty response body")
		return result
	}
	return result
}
