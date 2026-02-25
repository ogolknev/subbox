# subbox

Терминальный клиент для VLESS-подписок:

1. Загружает подписку по URL.
2. Делает RTT тест (TCP connect) по каждому узлу.
3. Делает HTTP health-check каждого узла через временный `sing-box`.
4. Сортирует узлы по живости (`HTTP OK` выше, затем по RTT).
5. Дает интерактивный выбор (стрелки `↑/↓`, Enter).
6. Конвертирует выбранный узел в JSON-конфиг `sing-box` и запускает его.

Поддерживаемые транспорты VLESS: `tcp`, `grpc`, `ws`, `httpupgrade`, `http/h2`.

## Сборка

```bash
go build -o subbox .
```

## Linux installation

Install system-wide (default: `/usr/local/bin/subbox`):

```bash
./scripts/install-linux.sh
```

Install for current user (no sudo):

```bash
BINDIR="$HOME/.local/bin" ./scripts/install-linux.sh
```

Uninstall:

```bash
./scripts/uninstall-linux.sh
```

## Структура проекта

```text
.
├── main.go                  # entrypoint CLI
└── app/                     # основная логика приложения
    ├── app.go
    ├── options.go
    ├── subscription.go
    ├── selection.go
    ├── probe.go
    ├── config.go
    ├── process.go
    └── util.go
```

## Запуск

Перед запуском укажи URL подписки через `--url` или переменную `SUBBOX_URL`.

Интерактивный выбор:

```bash
./subbox
```

Неинтерактивный выбор (например, 3-й узел):

```bash
./subbox --select 3
```

Запуск в TUN режиме (весь трафик через VPN):

```bash
sudo ./subbox --tun
```

Сохранить итоговый конфиг в файл:

```bash
./subbox --select 3 --config ./singbox.json
```

## Что делает TUN режим

В `--tun` режиме приложение использует безопасные значения по умолчанию, которые оказались стабильными в реальной проверке:

- `strict_route` и `auto_redirect` включаются на Linux.
- Добавляется `mixed` inbound (`127.0.0.1:2080`) для совместимости.
- Для proxy outbound принудительно используется TCP.
- QUIC (`udp/443`) блокируется, чтобы избежать подвисаний на некоторых узлах.
- DNS в TUN идет через удаленный resolver (`--tun-remote-dns`) с bootstrap DNS (`--tun-bootstrap-dns`).
- DNS стратегия: `prefer_ipv4`.

## Полезные флаги

URL подписки (обязательно берите URL в кавычки, если в нем есть `#`):

```bash
./subbox --url 'https://example.com/subscription#name'
```

Пропуск тестов перед меню:

```bash
./subbox --skip-tests --url 'https://example.com/subscription#name'
# или отдельно
./subbox --skip-rtt --skip-http --url 'https://example.com/subscription#name'
```

Или через переменную окружения:

```bash
SUBBOX_URL='https://example.com/subscription' ./subbox
```

Проверить генерацию без запуска:

```bash
./subbox --dry-run --print-config
```

Настройка RTT теста:

```bash
./subbox --probe-timeout 2s --probe-workers 20
```

Настройка HTTP health-check:

```bash
./subbox --health-check --health-url 'https://www.gstatic.com/generate_204' --health-timeout 8s --health-workers 3
```

Настройка DNS/стека в TUN режиме:

```bash
sudo ./subbox --tun --tun-stack mixed --tun-bootstrap-dns 1.1.1.1 --tun-remote-dns 'https://1.1.1.1/dns-query'
```

## Важные заметки

- Для `--tun` обычно нужны права `root` или capability `CAP_NET_ADMIN`.
- Если на машине уже запущен другой `sing-box`, он может мешать проверкам и маршрутизации.
