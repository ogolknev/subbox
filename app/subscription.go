package app

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var schemePattern = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9+.-]*://`)

type proxyEntry struct {
	raw  string
	name string
	uri  *url.URL

	tested   bool
	latency  time.Duration
	probeErr string

	httpTested  bool
	httpOK      bool
	httpStatus  int
	httpLatency time.Duration
	httpErr     string
}

func fetchSubscription(rawURL string) ([]proxyEntry, error) {
	client := &http.Client{Timeout: 20 * time.Second}
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("создание запроса: %w", err)
	}
	req.Header.Set("User-Agent", "subbox/1.0")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("загрузка подписки: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("подписка вернула HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxSubscriptionBody))
	if err != nil {
		return nil, fmt.Errorf("чтение подписки: %w", err)
	}

	links := extractLinks(body)
	if len(links) == 0 {
		return nil, errors.New("не удалось найти proxy-ссылки в подписке")
	}

	entries := make([]proxyEntry, 0, len(links))
	for _, raw := range links {
		uri, err := parseURL(raw)
		if err != nil || uri == nil {
			continue
		}
		if strings.ToLower(uri.Scheme) != "vless" {
			continue
		}
		if _, err := buildVLESSOutbound(uri); err != nil {
			continue
		}
		entries = append(entries, proxyEntry{
			raw:  raw,
			name: buildDisplayName(uri),
			uri:  uri,
		})
	}
	return entries, nil
}

func extractLinks(body []byte) []string {
	var links []string
	if json.Valid(body) {
		links = append(links, extractLinksFromJSON(body)...)
	}
	links = append(links, extractLinksFromPlainText(string(body))...)
	links = append(links, extractLinksFromBase64(string(body))...)
	return uniqueNonEmpty(links)
}

func extractLinksFromPlainText(content string) []string {
	var links []string
	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if !schemePattern.MatchString(line) {
			continue
		}
		links = append(links, line)
	}
	return links
}

func extractLinksFromJSON(body []byte) []string {
	var payload any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil
	}

	var links []string
	var walk func(any)
	walk = func(v any) {
		switch t := v.(type) {
		case string:
			value := strings.TrimSpace(t)
			if schemePattern.MatchString(value) {
				links = append(links, value)
			}
		case []any:
			for _, item := range t {
				walk(item)
			}
		case map[string]any:
			for _, item := range t {
				walk(item)
			}
		}
	}
	walk(payload)

	return links
}

func extractLinksFromBase64(content string) []string {
	normalized := collapseWhitespace(content)
	if normalized == "" || strings.Contains(normalized, "://") {
		return nil
	}

	var decoded []byte
	for _, decoder := range []func(string) ([]byte, error){
		base64.StdEncoding.DecodeString,
		base64.RawStdEncoding.DecodeString,
		base64.URLEncoding.DecodeString,
		base64.RawURLEncoding.DecodeString,
	} {
		out, err := decoder(padBase64(normalized))
		if err == nil {
			decoded = out
			break
		}
	}
	if len(decoded) == 0 {
		return nil
	}

	var links []string
	if json.Valid(decoded) {
		links = append(links, extractLinksFromJSON(decoded)...)
	}
	links = append(links, extractLinksFromPlainText(string(decoded))...)
	return links
}

func collapseWhitespace(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch r {
		case '\n', '\r', '\t', ' ':
			continue
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func padBase64(s string) string {
	if mod := len(s) % 4; mod != 0 {
		return s + strings.Repeat("=", 4-mod)
	}
	return s
}

func buildDisplayName(uri *url.URL) string {
	if uri == nil {
		return "unknown"
	}
	if fragment := strings.TrimSpace(uri.Fragment); fragment != "" {
		if decoded, err := url.QueryUnescape(fragment); err == nil && decoded != "" {
			return decoded
		}
		return fragment
	}

	host := uri.Hostname()
	port := uri.Port()
	if port == "" {
		port = "443"
	}
	return fmt.Sprintf("%s:%s", host, port)
}

func buildVLESSOutbound(uri *url.URL) (map[string]any, error) {
	if uri == nil {
		return nil, errors.New("пустой URL")
	}
	if strings.ToLower(uri.Scheme) != "vless" {
		return nil, fmt.Errorf("unsupported scheme: %s", uri.Scheme)
	}

	uuid := strings.TrimSpace(uri.User.Username())
	if uuid == "" {
		return nil, errors.New("не найден UUID пользователя")
	}
	server := strings.TrimSpace(uri.Hostname())
	if server == "" {
		return nil, errors.New("не найден адрес сервера")
	}

	serverPort := 443
	if rawPort := strings.TrimSpace(uri.Port()); rawPort != "" {
		port, err := strconv.Atoi(rawPort)
		if err != nil || port < 1 || port > 65535 {
			return nil, fmt.Errorf("неверный port: %q", rawPort)
		}
		serverPort = port
	}

	query := uri.Query()
	outbound := map[string]any{
		"type":        "vless",
		"tag":         "proxy",
		"server":      server,
		"server_port": serverPort,
		"uuid":        uuid,
	}

	if flow := strings.TrimSpace(query.Get("flow")); flow != "" {
		outbound["flow"] = flow
	}

	security := strings.ToLower(strings.TrimSpace(query.Get("security")))
	publicKey := strings.TrimSpace(query.Get("pbk"))
	if security == "tls" || security == "reality" || publicKey != "" {
		tlsConfig := map[string]any{"enabled": true}
		if serverName := firstNonEmpty(query.Get("sni"), query.Get("serverName"), query.Get("host")); serverName != "" {
			tlsConfig["server_name"] = serverName
		}
		if parseBool(query.Get("allowInsecure")) {
			tlsConfig["insecure"] = true
		}
		if alpn := splitCSV(query.Get("alpn")); len(alpn) > 0 {
			tlsConfig["alpn"] = alpn
		}
		if fp := firstNonEmpty(query.Get("fp"), query.Get("fingerprint")); fp != "" {
			tlsConfig["utls"] = map[string]any{
				"enabled":     true,
				"fingerprint": fp,
			}
		}

		if security == "reality" || publicKey != "" {
			if publicKey == "" {
				return nil, errors.New("для reality отсутствует pbk")
			}
			reality := map[string]any{
				"enabled":    true,
				"public_key": publicKey,
			}
			if shortID := strings.TrimSpace(query.Get("sid")); shortID != "" {
				reality["short_id"] = shortID
			}
			tlsConfig["reality"] = reality
		}

		outbound["tls"] = tlsConfig
	}

	transportType := strings.ToLower(strings.TrimSpace(firstNonEmpty(query.Get("type"), query.Get("network"))))
	switch transportType {
	case "", "tcp":
	case "grpc":
		transport := map[string]any{"type": "grpc"}
		if serviceName := strings.TrimPrefix(firstNonEmpty(query.Get("serviceName"), query.Get("service_name")), "/"); serviceName != "" {
			transport["service_name"] = serviceName
		}
		if authority := strings.TrimSpace(query.Get("authority")); authority != "" {
			transport["authority"] = authority
		}
		outbound["transport"] = transport
	case "ws", "websocket":
		transport := map[string]any{"type": "ws"}
		if path := strings.TrimSpace(query.Get("path")); path != "" {
			transport["path"] = path
		}
		if host := strings.TrimSpace(query.Get("host")); host != "" {
			transport["headers"] = map[string]any{"Host": host}
		}
		outbound["transport"] = transport
	case "httpupgrade":
		transport := map[string]any{"type": "httpupgrade"}
		if host := strings.TrimSpace(query.Get("host")); host != "" {
			transport["host"] = host
		}
		if path := strings.TrimSpace(query.Get("path")); path != "" {
			transport["path"] = path
		}
		outbound["transport"] = transport
	case "http", "h2":
		transport := map[string]any{"type": "http"}
		if path := strings.TrimSpace(query.Get("path")); path != "" {
			transport["path"] = path
		}
		if hosts := splitCSV(query.Get("host")); len(hosts) > 0 {
			transport["host"] = hosts
		}
		outbound["transport"] = transport
	default:
		return nil, fmt.Errorf("неподдерживаемый transport type: %q", transportType)
	}

	return outbound, nil
}
