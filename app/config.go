package app

import (
	"fmt"
	"net"
	"runtime"
	"strconv"
	"strings"
)

type tunPolicy struct {
	strictRoute     bool
	autoRedirect    bool
	addMixedInbound bool
	forceTCP        bool
	blockQUIC       bool
	dnsStrategy     string
}

func defaultTunPolicy() tunPolicy {
	isLinux := runtime.GOOS == "linux"
	return tunPolicy{
		strictRoute:     isLinux,
		autoRedirect:    isLinux,
		addMixedInbound: true,
		forceTCP:        true,
		blockQUIC:       true,
		dnsStrategy:     defaultTunDNSStrategy,
	}
}

func buildSingBoxConfig(proxyOutbound map[string]any, opts options) map[string]any {
	config := map[string]any{
		"log": map[string]any{
			"level": opts.logLevel,
		},
	}

	route := map[string]any{
		"auto_detect_interface": true,
		"final":                 "proxy",
	}

	if opts.useTun {
		policy := defaultTunPolicy()
		if policy.forceTCP {
			proxyOutbound["network"] = "tcp"
		}

		inbounds := []any{map[string]any{
			"type":                  "tun",
			"tag":                   "tun-in",
			"interface_name":        opts.tunName,
			"address":               splitCSV(opts.tunAddress),
			"mtu":                   opts.tunMTU,
			"auto_route":            true,
			"strict_route":          policy.strictRoute,
			"stack":                 opts.tunStack,
			"route_exclude_address": []string{"127.0.0.0/8", "::1/128"},
		}}
		if policy.autoRedirect {
			tunInbound := inbounds[0].(map[string]any)
			tunInbound["auto_redirect"] = true
		}
		if policy.addMixedInbound {
			inbounds = append(inbounds, mixedInbound(opts))
		}
		config["inbounds"] = inbounds

		dns, rules, resolver := buildTunDNSAndRules(proxyOutbound, opts, policy)
		config["dns"] = dns
		route["rules"] = rules
		route["default_domain_resolver"] = resolver
	} else {
		config["inbounds"] = []any{mixedInbound(opts)}
	}

	config["outbounds"] = []any{
		proxyOutbound,
		map[string]any{"type": "direct", "tag": "direct"},
		map[string]any{"type": "block", "tag": "block"},
	}
	config["route"] = route

	return config
}

func mixedInbound(opts options) map[string]any {
	return map[string]any{
		"type":        "mixed",
		"tag":         "mixed-in",
		"listen":      opts.mixedListen,
		"listen_port": opts.mixedPort,
	}
}

func buildTunDNSAndRules(proxyOutbound map[string]any, opts options, policy tunPolicy) (map[string]any, []any, map[string]any) {
	serverHost := strings.TrimSpace(fmt.Sprintf("%v", proxyOutbound["server"]))

	routeRules := []any{
		map[string]any{"action": "sniff"},
		map[string]any{"protocol": "dns", "action": "hijack-dns"},
	}
	if policy.blockQUIC {
		routeRules = append(routeRules, map[string]any{
			"action":  "reject",
			"network": "udp",
			"port":    443,
			"method":  "default",
		})
	}

	dnsRules := []any{}
	if serverHost != "" && net.ParseIP(serverHost) == nil {
		dnsRules = append(dnsRules, map[string]any{
			"domain": []string{serverHost},
			"server": "bootstrap",
		})
		routeRules = append(routeRules, map[string]any{
			"domain":   []string{serverHost},
			"outbound": "direct",
		})
	}

	bootstrap := buildBootstrapDNSServer(opts.tunBootstrapDNS)
	remote := buildRemoteDNSServer(opts.tunRemoteDNS, "bootstrap")
	dns := map[string]any{
		"strategy": policy.dnsStrategy,
		"servers":  []any{bootstrap, remote},
		"rules":    dnsRules,
		"final":    "remote",
	}
	defaultResolver := map[string]any{
		"server":   "bootstrap",
		"strategy": policy.dnsStrategy,
	}

	return dns, routeRules, defaultResolver
}

func buildBootstrapDNSServer(raw string) map[string]any {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		raw = defaultTunBootstrapDNS
	}

	host := raw
	port := 53
	if u, err := parseURL("//" + raw); err == nil && u != nil {
		if h := strings.TrimSpace(u.Hostname()); h != "" {
			host = h
		}
		if p := strings.TrimSpace(u.Port()); p != "" {
			if parsed, err := strconv.Atoi(p); err == nil && parsed >= 1 && parsed <= 65535 {
				port = parsed
			}
		}
	}
	if net.ParseIP(host) == nil {
		host = defaultTunBootstrapDNS
	}

	return map[string]any{
		"type":        "udp",
		"tag":         "bootstrap",
		"server":      host,
		"server_port": port,
	}
}

func buildRemoteDNSServer(raw, resolverTag string) map[string]any {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		raw = defaultTunRemoteDNS
	}

	remote := map[string]any{
		"tag":    "remote",
		"detour": "proxy",
	}

	uri, err := parseURL(raw)
	if err == nil && uri != nil && uri.Scheme != "" && uri.Host != "" {
		scheme := strings.ToLower(uri.Scheme)
		switch scheme {
		case "https":
			remote["type"] = "https"
		case "tls":
			remote["type"] = "tls"
		case "tcp":
			remote["type"] = "tcp"
		case "udp":
			remote["type"] = "udp"
		case "quic":
			remote["type"] = "quic"
		case "h3", "http3":
			remote["type"] = "h3"
		default:
			remote["type"] = "https"
		}

		server := strings.TrimSpace(uri.Hostname())
		if server == "" {
			server = "1.1.1.1"
		}
		remote["server"] = server

		if portRaw := strings.TrimSpace(uri.Port()); portRaw != "" {
			if port, err := strconv.Atoi(portRaw); err == nil && port >= 1 && port <= 65535 {
				remote["server_port"] = port
			}
		}
		if remote["type"] == "https" || remote["type"] == "h3" {
			if path := strings.TrimSpace(uri.EscapedPath()); path != "" && path != "/" {
				remote["path"] = path
			}
		}
		if net.ParseIP(server) == nil {
			remote["domain_resolver"] = resolverTag
		}
		return remote
	}

	if net.ParseIP(raw) != nil {
		remote["type"] = "udp"
		remote["server"] = raw
		return remote
	}

	remote["type"] = "https"
	remote["server"] = raw
	remote["domain_resolver"] = resolverTag
	return remote
}
