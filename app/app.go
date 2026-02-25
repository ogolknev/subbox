package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

func RunCLI() error {
	opts := parseFlags()
	return run(opts)
}

func run(opts options) error {
	if err := validateOptions(&opts); err != nil {
		return err
	}

	entries, err := fetchSubscription(opts.subscriptionURL)
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		return errors.New("в подписке нет поддерживаемых VLESS конфигов")
	}

	chosen, err := chooseEntry(entries, opts)
	if err != nil {
		return err
	}

	proxyOutbound, err := buildVLESSOutbound(chosen.uri)
	if err != nil {
		return fmt.Errorf("конвертация %q: %w", chosen.name, err)
	}

	config := buildSingBoxConfig(proxyOutbound, opts)
	configPath, cleanup, err := writeConfig(config, opts.configPath, opts.keepConfig)
	if err != nil {
		return err
	}
	defer cleanup()

	fmt.Printf("Выбран конфиг: %s\n", chosen.name)
	fmt.Printf("Сгенерирован файл: %s\n", configPath)

	if opts.printConfig {
		raw, err := json.MarshalIndent(config, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(raw))
	}

	if opts.dryRun {
		fmt.Println("dry-run: запуск sing-box пропущен")
		return nil
	}

	if _, err := exec.LookPath(opts.singBoxBinary); err != nil {
		return fmt.Errorf("не найден бинарник %q в PATH", opts.singBoxBinary)
	}

	if !opts.skipCheck {
		if err := checkSingBoxConfig(opts.singBoxBinary, configPath); err != nil {
			return err
		}
	}

	if opts.useTun {
		fmt.Println("Запуск в TUN режиме. Для Linux обычно требуется sudo/root или CAP_NET_ADMIN.")
	}
	fmt.Printf("Запуск: %s run -c %s\n", opts.singBoxBinary, configPath)
	return runSingBox(opts.singBoxBinary, configPath)
}

func validateOptions(opts *options) error {
	if strings.TrimSpace(opts.subscriptionURL) == "" {
		return errors.New("URL подписки пустой")
	}
	if opts.tunMTU < 576 {
		return fmt.Errorf("слишком маленький tun-mtu: %d", opts.tunMTU)
	}
	if opts.mixedPort < 1 || opts.mixedPort > 65535 {
		return fmt.Errorf("неверный порт mixed inbound: %d", opts.mixedPort)
	}
	if !opts.skipRTT {
		if opts.probeTimeout <= 0 {
			return fmt.Errorf("неверный probe-timeout: %s", opts.probeTimeout)
		}
		if opts.probeWorkers < 1 {
			return fmt.Errorf("неверный probe-workers: %d", opts.probeWorkers)
		}
	}
	if opts.healthCheck && !opts.skipHTTP {
		if opts.healthTimeout <= 0 {
			return fmt.Errorf("неверный health-timeout: %s", opts.healthTimeout)
		}
		if opts.healthWorkers < 1 {
			return fmt.Errorf("неверный health-workers: %d", opts.healthWorkers)
		}
		healthURL := strings.TrimSpace(opts.healthURL)
		if healthURL == "" {
			return errors.New("health-url не может быть пустым, когда health-check включен")
		}
		parsed, err := parseURL(healthURL)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" {
			return fmt.Errorf("неверный health-url: %q", opts.healthURL)
		}
	}

	if !opts.useTun {
		return nil
	}

	if strings.TrimSpace(opts.tunRemoteDNS) == "" {
		return errors.New("tun-remote-dns не может быть пустым")
	}
	if strings.TrimSpace(opts.tunBootstrapDNS) == "" {
		return errors.New("tun-bootstrap-dns не может быть пустым")
	}

	stack := strings.ToLower(strings.TrimSpace(opts.tunStack))
	switch stack {
	case "system", "mixed", "gvisor":
		opts.tunStack = stack
	default:
		return fmt.Errorf("неподдерживаемый tun-stack: %q", opts.tunStack)
	}

	return nil
}
