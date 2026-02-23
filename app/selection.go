package app

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/manifoldco/promptui"
	"golang.org/x/term"
)

func chooseEntry(entries []proxyEntry, opts options) (proxyEntry, error) {
	if opts.selectedIndex > 0 {
		if opts.selectedIndex > len(entries) {
			return proxyEntry{}, fmt.Errorf("индекс %d вне диапазона 1..%d", opts.selectedIndex, len(entries))
		}
		return entries[opts.selectedIndex-1], nil
	}

	fmt.Printf("RTT тест %d конфигов...\n", len(entries))
	probeEntriesRTT(entries, opts.probeTimeout, opts.probeWorkers)

	if opts.healthCheck {
		fmt.Printf("HTTP тест %d конфигов...\n", len(entries))
		probeEntriesHTTP(entries, opts)
	}

	sortEntries(entries, opts.healthCheck)
	if opts.healthCheck {
		fmt.Println("Сортировка: сначала HTTP OK, затем по RTT")
	}

	fd := int(os.Stdin.Fd())
	if term.IsTerminal(fd) {
		return chooseEntryWithArrows(entries)
	}
	return chooseEntryByNumber(entries)
}

func chooseEntryByNumber(entries []proxyEntry) (proxyEntry, error) {
	fmt.Println("Доступные конфиги:")
	for i, entry := range entries {
		fmt.Printf("%2d) [RTT:%-7s HTTP:%-8s] %s\n", i+1, formatProbeStatus(entry), formatHTTPStatus(entry), entry.name)
	}

	reader := bufio.NewReader(os.Stdin)
	for {
		fmt.Printf("Выберите номер [1-%d]: ", len(entries))
		line, err := reader.ReadString('\n')
		if err != nil {
			return proxyEntry{}, fmt.Errorf("чтение ввода: %w", err)
		}
		line = strings.TrimSpace(line)
		index, err := strconv.Atoi(line)
		if err != nil || index < 1 || index > len(entries) {
			fmt.Println("Неверный номер, попробуйте снова.")
			continue
		}
		return entries[index-1], nil
	}
}

func chooseEntryWithArrows(entries []proxyEntry) (proxyEntry, error) {
	type menuItem struct {
		Index int
		RTT   string
		HTTP  string
		Name  string
	}

	termWidth := 100
	if width, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil && width > 0 {
		termWidth = width
	}
	maxName := termWidth - 36
	if maxName < 16 {
		maxName = 16
	}

	items := make([]menuItem, 0, len(entries))
	for i, entry := range entries {
		items = append(items, menuItem{
			Index: i + 1,
			RTT:   fmt.Sprintf("%-7s", formatProbeStatus(entry)),
			HTTP:  fmt.Sprintf("%-8s", formatHTTPStatus(entry)),
			Name:  clipRunes(entry.name, maxName),
		})
	}

	size := minInt(20, len(items))
	if size < 5 {
		size = len(items)
	}

	selector := promptui.Select{
		Label: "Выберите конфиг (стрелки, Enter; Ctrl+C - выход)",
		Items: items,
		Size:  size,
		Templates: &promptui.SelectTemplates{
			Active:   "▸ {{ printf \"%2d\" .Index }} | RTT {{ .RTT }} | HTTP {{ .HTTP }} | {{ .Name }}",
			Inactive: "  {{ printf \"%2d\" .Index }} | RTT {{ .RTT }} | HTTP {{ .HTTP }} | {{ .Name }}",
			Selected: "Выбран: {{ printf \"%2d\" .Index }} | RTT {{ .RTT }} | HTTP {{ .HTTP }} | {{ .Name }}",
			Label:    "{{ . }}",
		},
	}

	index, _, err := selector.Run()
	if err != nil {
		return proxyEntry{}, errors.New("выбор отменен")
	}
	return entries[index], nil
}

func sortEntries(entries []proxyEntry, withHTTP bool) {
	sort.SliceStable(entries, func(i, j int) bool {
		a := entries[i]
		b := entries[j]

		if withHTTP {
			ab := healthBucket(a)
			bb := healthBucket(b)
			if ab != bb {
				return ab < bb
			}
			if ab == 0 {
				if a.httpLatency != b.httpLatency {
					return a.httpLatency < b.httpLatency
				}
			}
		}

		al := probeLatencyOrMax(a)
		bl := probeLatencyOrMax(b)
		if al != bl {
			return al < bl
		}
		return strings.ToLower(a.name) < strings.ToLower(b.name)
	})
}

func healthBucket(entry proxyEntry) int {
	if entry.httpTested && entry.httpOK {
		return 0
	}
	if entry.tested && entry.probeErr == "" {
		return 1
	}
	return 2
}

func probeLatencyOrMax(entry proxyEntry) time.Duration {
	if entry.tested && entry.probeErr == "" {
		return entry.latency
	}
	return 10 * time.Second
}

func formatProbeStatus(entry proxyEntry) string {
	if !entry.tested {
		return "--"
	}
	if entry.probeErr != "" {
		return entry.probeErr
	}
	ms := entry.latency.Milliseconds()
	if ms < 1 {
		ms = 1
	}
	return fmt.Sprintf("%dms", ms)
}

func formatHTTPStatus(entry proxyEntry) string {
	if !entry.httpTested {
		return "--"
	}
	if entry.httpOK {
		if entry.httpStatus > 0 {
			return fmt.Sprintf("ok:%d", entry.httpStatus)
		}
		return "ok"
	}
	if entry.httpErr == "" {
		return "fail"
	}
	return clipRunes(entry.httpErr, 8)
}
