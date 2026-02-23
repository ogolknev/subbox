package app

import (
	"flag"
	"os"
	"strings"
	"time"
)

const (
	defaultMixedListen     = "127.0.0.1"
	defaultMixedPort       = 2080
	defaultTunName         = "sb-tun"
	defaultTunAddress      = "172.19.0.1/30"
	defaultTunMTU          = 1400
	defaultTunStack        = "mixed"
	defaultTunBootstrapDNS = "1.1.1.1"
	defaultTunRemoteDNS    = "https://1.1.1.1/dns-query"
	defaultTunDNSStrategy  = "prefer_ipv4"
	defaultHealthURL       = "https://www.gstatic.com/generate_204"
	defaultLogLevel        = "info"
	maxSubscriptionBody    = 8 << 20
)

type options struct {
	subscriptionURL string
	singBoxBinary   string
	configPath      string
	logLevel        string

	useTun          bool
	tunName         string
	tunAddress      string
	tunMTU          int
	tunStack        string
	tunBootstrapDNS string
	tunRemoteDNS    string

	mixedListen string
	mixedPort   int

	probeTimeout  time.Duration
	probeWorkers  int
	healthCheck   bool
	healthURL     string
	healthTimeout time.Duration
	healthWorkers int

	selectedIndex int
	dryRun        bool
	printConfig   bool
	keepConfig    bool
	skipCheck     bool
}

func parseFlags() options {
	defaultURL := strings.TrimSpace(os.Getenv("SUBBOX_URL"))

	opts := options{}
	flag.StringVar(&opts.subscriptionURL, "url", defaultURL, "URL подписки (обязательно, если не задан SUBBOX_URL)")
	flag.StringVar(&opts.singBoxBinary, "bin", "sing-box", "путь к бинарнику sing-box")
	flag.StringVar(&opts.configPath, "config", "", "куда сохранить сгенерированный конфиг")
	flag.StringVar(&opts.logLevel, "log-level", defaultLogLevel, "уровень логов sing-box")

	flag.BoolVar(&opts.useTun, "tun", false, "включить TUN режим (весь трафик через VPN)")
	flag.StringVar(&opts.tunName, "tun-name", defaultTunName, "имя TUN интерфейса")
	flag.StringVar(&opts.tunAddress, "tun-address", defaultTunAddress, "адрес TUN интерфейса (CSV)")
	flag.IntVar(&opts.tunMTU, "tun-mtu", defaultTunMTU, "MTU для TUN интерфейса")
	flag.StringVar(&opts.tunStack, "tun-stack", defaultTunStack, "TCP/IP стек TUN: system|mixed|gvisor")
	flag.StringVar(&opts.tunBootstrapDNS, "tun-bootstrap-dns", defaultTunBootstrapDNS, "DNS для первичного резолва VPN endpoint")
	flag.StringVar(&opts.tunRemoteDNS, "tun-remote-dns", defaultTunRemoteDNS, "DNS сервер для TUN режима (например DoH)")

	flag.StringVar(&opts.mixedListen, "listen", defaultMixedListen, "адрес mixed inbound")
	flag.IntVar(&opts.mixedPort, "port", defaultMixedPort, "порт mixed inbound")

	flag.DurationVar(&opts.probeTimeout, "probe-timeout", 1500*time.Millisecond, "таймаут RTT теста")
	flag.IntVar(&opts.probeWorkers, "probe-workers", 12, "количество параллельных RTT тестов")
	flag.BoolVar(&opts.healthCheck, "health-check", true, "выполнять HTTP-проверку каждого конфига перед меню")
	flag.StringVar(&opts.healthURL, "health-url", defaultHealthURL, "URL для HTTP-проверки через каждый конфиг")
	flag.DurationVar(&opts.healthTimeout, "health-timeout", 8*time.Second, "таймаут HTTP-проверки одного конфига")
	flag.IntVar(&opts.healthWorkers, "health-workers", 3, "количество параллельных HTTP-проверок")

	flag.IntVar(&opts.selectedIndex, "select", 0, "номер конфига для неинтерактивного выбора")
	flag.BoolVar(&opts.dryRun, "dry-run", false, "не запускать sing-box, только собрать конфиг")
	flag.BoolVar(&opts.printConfig, "print-config", false, "печатать сгенерированный JSON")
	flag.BoolVar(&opts.keepConfig, "keep-config", false, "не удалять временный конфиг после завершения")
	flag.BoolVar(&opts.skipCheck, "skip-check", false, "не выполнять sing-box check перед запуском")
	flag.Parse()

	return opts
}
