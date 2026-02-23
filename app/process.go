package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
)

func writeConfig(config map[string]any, explicitPath string, keep bool) (string, func(), error) {
	raw, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return "", nil, fmt.Errorf("marshal config: %w", err)
	}

	if explicitPath != "" {
		if err := os.WriteFile(explicitPath, raw, 0o600); err != nil {
			return "", nil, fmt.Errorf("запись %s: %w", explicitPath, err)
		}
		return explicitPath, func() {}, nil
	}

	file, err := os.CreateTemp("", "subbox-*.json")
	if err != nil {
		return "", nil, fmt.Errorf("создание temp config: %w", err)
	}
	path := file.Name()
	if _, err := file.Write(raw); err != nil {
		_ = file.Close()
		return "", nil, fmt.Errorf("запись temp config: %w", err)
	}
	if err := file.Close(); err != nil {
		return "", nil, fmt.Errorf("закрытие temp config: %w", err)
	}

	cleanup := func() {
		if keep {
			return
		}
		_ = os.Remove(path)
	}
	return path, cleanup, nil
}

func checkSingBoxConfig(binary, configPath string) error {
	cmd := exec.Command(binary, "check", "-c", configPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("sing-box check не прошел: %w\n%s", err, bytes.TrimSpace(output))
	}
	return nil
}

func runSingBox(binary, configPath string) error {
	cmd := exec.Command(binary, "run", "-c", configPath)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("не удалось запустить sing-box: %w", err)
	}

	sigCh := make(chan os.Signal, 2)
	doneCh := make(chan error, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	go func() {
		doneCh <- cmd.Wait()
	}()

	for {
		select {
		case sig := <-sigCh:
			if cmd.Process != nil {
				_ = cmd.Process.Signal(sig)
			}
		case err := <-doneCh:
			return err
		}
	}
}
