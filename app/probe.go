package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

type probeOutcome struct {
	index   int
	latency time.Duration
	err     error
}

type httpProbeOutcome struct {
	index   int
	status  int
	latency time.Duration
	err     error
}

func probeEntriesRTT(entries []proxyEntry, timeout time.Duration, workers int) {
	if len(entries) == 0 {
		return
	}
	if workers > len(entries) {
		workers = len(entries)
	}
	if workers < 1 {
		workers = 1
	}

	jobs := make(chan int)
	results := make(chan probeOutcome, len(entries))

	for i := 0; i < workers; i++ {
		go func() {
			for idx := range jobs {
				latency, err := probeEntryRTT(entries[idx], timeout)
				results <- probeOutcome{index: idx, latency: latency, err: err}
			}
		}()
	}

	for i := range entries {
		jobs <- i
	}
	close(jobs)

	for i := 0; i < len(entries); i++ {
		res := <-results
		entries[res.index].tested = true
		entries[res.index].latency = res.latency
		if res.err != nil {
			entries[res.index].probeErr = normalizeProbeError(res.err)
		}
	}
}

func probeEntryRTT(entry proxyEntry, timeout time.Duration) (time.Duration, error) {
	if entry.uri == nil {
		return 0, errors.New("bad uri")
	}
	host := entry.uri.Hostname()
	if host == "" {
		return 0, errors.New("no host")
	}
	port := entry.uri.Port()
	if port == "" {
		port = "443"
	}

	address := net.JoinHostPort(host, port)
	dialer := net.Dialer{Timeout: timeout}

	start := time.Now()
	conn, err := dialer.Dial("tcp", address)
	if err != nil {
		return 0, err
	}
	_ = conn.Close()
	return time.Since(start), nil
}

func probeEntriesHTTP(entries []proxyEntry, opts options) {
	if len(entries) == 0 {
		return
	}

	if _, err := exec.LookPath(opts.singBoxBinary); err != nil {
		markHTTPProbeSkipped(entries)
		return
	}
	curlPath, err := exec.LookPath("curl")
	if err != nil {
		markHTTPProbeSkipped(entries)
		return
	}

	workers := opts.healthWorkers
	if workers > len(entries) {
		workers = len(entries)
	}
	if workers < 1 {
		workers = 1
	}

	jobs := make(chan int)
	results := make(chan httpProbeOutcome, len(entries))

	for i := 0; i < workers; i++ {
		go func() {
			for idx := range jobs {
				if entries[idx].probeErr != "" {
					results <- httpProbeOutcome{index: idx, err: errors.New("skip")}
					continue
				}
				status, latency, err := probeEntryHTTP(entries[idx], opts, curlPath)
				results <- httpProbeOutcome{index: idx, status: status, latency: latency, err: err}
			}
		}()
	}

	for i := range entries {
		jobs <- i
	}
	close(jobs)

	for i := 0; i < len(entries); i++ {
		res := <-results
		entries[res.index].httpTested = true
		entries[res.index].httpLatency = res.latency
		entries[res.index].httpStatus = res.status
		if res.err != nil {
			entries[res.index].httpErr = normalizeHTTPProbeError(res.err)
			continue
		}
		entries[res.index].httpOK = true
	}
}

func markHTTPProbeSkipped(entries []proxyEntry) {
	for i := range entries {
		entries[i].httpTested = true
		entries[i].httpErr = "skip"
	}
}

func probeEntryHTTP(entry proxyEntry, opts options, curlPath string) (int, time.Duration, error) {
	proxyOutbound, err := buildVLESSOutbound(entry.uri)
	if err != nil {
		return 0, 0, err
	}
	if opts.useTun {
		proxyOutbound["network"] = "tcp"
	}

	port, err := reserveLocalPort()
	if err != nil {
		return 0, 0, err
	}

	config := map[string]any{
		"log": map[string]any{"level": "error"},
		"inbounds": []any{
			map[string]any{
				"type":        "mixed",
				"tag":         "mixed-in",
				"listen":      "127.0.0.1",
				"listen_port": port,
			},
		},
		"outbounds": []any{
			proxyOutbound,
			map[string]any{"type": "direct", "tag": "direct"},
			map[string]any{"type": "block", "tag": "block"},
		},
		"route": map[string]any{
			"auto_detect_interface": true,
			"final":                 "proxy",
		},
	}

	configPath, cleanupConfig, err := writeConfig(config, "", false)
	if err != nil {
		return 0, 0, err
	}
	defer cleanupConfig()

	totalTimeout := opts.healthTimeout + 4*time.Second
	ctx, cancel := context.WithTimeout(context.Background(), totalTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, opts.singBoxBinary, "run", "-c", configPath)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		return 0, 0, err
	}

	waitCh := make(chan error, 1)
	go func() {
		waitCh <- cmd.Wait()
	}()
	defer stopProbeProcess(cmd, waitCh)

	proxyAddr := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	startTimeout := minDuration(3*time.Second, opts.healthTimeout)
	if startTimeout < 500*time.Millisecond {
		startTimeout = 500 * time.Millisecond
	}
	if err := waitForProxyReady(proxyAddr, waitCh, startTimeout); err != nil {
		return 0, 0, err
	}

	requestURL := strings.TrimSpace(opts.healthURL)
	if requestURL == "" {
		requestURL = defaultHealthURL
	}

	var lastErr error
	var lastStatus int
	var lastLatency time.Duration
	for attempt := 0; attempt < 2; attempt++ {
		status, latency, err := doProxyHTTPCheckWithCurl(curlPath, proxyAddr, requestURL, opts.healthTimeout)
		lastErr = err
		lastStatus = status
		lastLatency = latency
		if err == nil {
			return status, latency, nil
		}
		if attempt == 0 {
			time.Sleep(200 * time.Millisecond)
		}
	}

	if lastErr != nil {
		return 0, lastLatency, lastErr
	}
	return lastStatus, lastLatency, nil
}

func doProxyHTTPCheckWithCurl(curlPath, proxyAddr, requestURL string, timeout time.Duration) (int, time.Duration, error) {
	maxTimeSec := fmt.Sprintf("%.1f", timeout.Seconds())
	connectTimeoutSec := fmt.Sprintf("%.1f", minDuration(3*time.Second, timeout).Seconds())
	args := []string{
		"--silent",
		"--show-error",
		"--output", "/dev/null",
		"--write-out", "%{http_code}",
		"--ipv4",
		"--connect-timeout", connectTimeoutSec,
		"--max-time", maxTimeSec,
		"--proxy", "http://" + proxyAddr,
		"--user-agent", "subbox-health/1.0",
		requestURL,
	}

	start := time.Now()
	out, err := exec.Command(curlPath, args...).CombinedOutput()
	latency := time.Since(start)
	if err != nil {
		return 0, latency, err
	}

	statusRaw := strings.TrimSpace(string(out))
	if statusRaw == "" {
		return 0, latency, errors.New("curl вернул пустой статус")
	}
	status, convErr := strconv.Atoi(statusRaw)
	if convErr != nil {
		return 0, latency, fmt.Errorf("curl вернул нечисловой статус: %q", statusRaw)
	}
	return status, latency, nil
}

func reserveLocalPort() (int, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer ln.Close()

	addr, ok := ln.Addr().(*net.TCPAddr)
	if !ok || addr.Port <= 0 {
		return 0, errors.New("не удалось получить локальный порт")
	}
	return addr.Port, nil
}

func waitForProxyReady(address string, waitCh <-chan error, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		select {
		case err := <-waitCh:
			if err == nil {
				return errors.New("sing-box завершился до начала проверки")
			}
			return fmt.Errorf("sing-box завершился до начала проверки: %w", err)
		default:
		}

		conn, err := net.DialTimeout("tcp", address, 200*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return errors.New("таймаут запуска локального proxy для проверки")
}

func stopProbeProcess(cmd *exec.Cmd, waitCh <-chan error) {
	if cmd == nil || cmd.Process == nil {
		return
	}

	_ = cmd.Process.Signal(os.Interrupt)
	select {
	case <-waitCh:
		return
	case <-time.After(300 * time.Millisecond):
	}

	_ = cmd.Process.Kill()
	select {
	case <-waitCh:
	case <-time.After(300 * time.Millisecond):
	}
}

func minDuration(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}

func normalizeProbeError(err error) string {
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return "timeout"
	}
	return "fail"
}

func normalizeHTTPProbeError(err error) string {
	if err == nil {
		return ""
	}
	text := strings.ToLower(strings.TrimSpace(err.Error()))
	if text == "skip" {
		return "skip"
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return "timeout"
	}
	if errors.Is(err, context.DeadlineExceeded) || strings.Contains(text, "deadline exceeded") {
		return "timeout"
	}
	return "fail"
}
