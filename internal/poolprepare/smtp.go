package poolprepare

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/smtp"
	"slices"
	"sync"
	"time"

	"localclash/internal/capability"
	"localclash/internal/poolcontract"
	"localclash/internal/proxyprobe"
)

const smtpProbeAddress = "smtp.gmail.com:587"
const smtpProbeTimeout = 10 * time.Second

// Qualify SMTP independently of HTTP reachability and automatic-pool ranking.
func qualifySMTP(ctx context.Context, input poolcontract.Input, names []string, opts Options) (capability.Result, error) {
	start := time.Now()
	result := capability.Result{Profile: poolcontract.SMTPCapability, Candidates: len(names)}
	candidates, _, _, err := autoCandidates(input.Proxies, names, nil, nil)
	if err != nil {
		return result, err
	}
	result.DeduplicatedCount = len(names) - len(candidates)
	for batch := range slices.Chunk(candidates, proxyprobe.BatchSize) {
		selected := make([]string, len(batch))
		for i, candidate := range batch {
			selected[i] = candidate.name
		}
		qualified, err := smtpBatch(ctx, input, selected, opts)
		if err != nil {
			return result, err
		}
		result.Probed += len(batch)
		result.Qualified = append(result.Qualified, qualified...)
	}
	result.QualifiedCount = len(result.Qualified)
	result.ObservedQualifiedCount = result.QualifiedCount
	result.UnavailableCount = result.Probed - result.QualifiedCount
	result.DurationMS = time.Since(start).Milliseconds()
	return result, ctx.Err()
}

func smtpBatch(ctx context.Context, input poolcontract.Input, names []string, opts Options) ([]string, error) {
	session, err := proxyprobe.Start(ctx, proxyprobe.RequiredDefinitions(input.Proxies, names), names, proxyprobe.Options{
		CorePath: opts.CorePath, RuntimeParent: opts.ProbeDir, RuntimePrefix: "smtp-pool-",
	})
	if err != nil {
		return nil, err
	}
	defer session.Close()

	jobs := make(chan int)
	passed := make([]bool, len(names))
	var workers sync.WaitGroup
	for range min(len(names), opts.Concurrency) {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for i := range jobs {
				probeCtx, cancel := context.WithTimeout(ctx, smtpProbeTimeout)
				passed[i] = probeSMTP(probeCtx, func(ctx context.Context, address string) (net.Conn, error) {
					return session.DialContext(ctx, i, address)
				}, smtpProbeAddress, nil) == nil
				cancel()
			}
		}()
	}
	for i := range names {
		if ctx.Err() != nil {
			break
		}
		jobs <- i
	}
	close(jobs)
	workers.Wait()
	if err := errors.Join(ctx.Err(), session.Err()); err != nil {
		return nil, err
	}
	var qualified []string
	for i, ok := range passed {
		if ok {
			qualified = append(qualified, names[i])
		}
	}
	return qualified, nil
}

func probeSMTP(ctx context.Context, dial func(context.Context, string) (net.Conn, error), address string, tlsConfig *tls.Config) error {
	conn, err := dial(ctx, address)
	if err != nil {
		return err
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	if deadline, ok := ctx.Deadline(); ok {
		if err := conn.SetDeadline(deadline); err != nil {
			return err
		}
	}
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	client, err := smtp.NewClient(conn, host)
	if err != nil {
		return fmt.Errorf("SMTP greeting: %w", err)
	}
	defer client.Close()
	if err := client.Hello("localhost"); err != nil {
		return fmt.Errorf("SMTP EHLO: %w", err)
	}
	if available, _ := client.Extension("STARTTLS"); !available {
		return errors.New("SMTP STARTTLS is unavailable")
	}
	if tlsConfig == nil {
		tlsConfig = &tls.Config{ServerName: host}
	}
	// StartTLS verifies the certificate and repeats EHLO over the encrypted tunnel.
	if err := client.StartTLS(tlsConfig); err != nil {
		return fmt.Errorf("SMTP STARTTLS: %w", err)
	}
	// The verified TLS EHLO completes the support check. QUIT is session cleanup.
	_ = client.Quit()
	return nil
}
