// Package secret reads credentials from pass-compatible password managers.
package secret

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// passTimeout bounds a lookup, which may wait for a passphrase or PIN prompt.
const passTimeout = 60 * time.Second

// FromPass runs "<passCmd> show <entry>" and returns the first line of its
// output, which by pass convention holds the secret.
func FromPass(passCmd, entry string) (string, error) {
	if strings.HasPrefix(entry, "-") {
		return "", fmt.Errorf("invalid pass entry %q", entry)
	}
	ctx, cancel := context.WithTimeout(context.Background(), passTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, passCmd, "show", entry)
	cmd.Stdin = os.Stdin // allow passphrase/PIN prompts
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("%s show %s: %w", passCmd, entry, err)
	}
	line, _, _ := strings.Cut(string(out), "\n")
	value := strings.TrimSpace(line)
	if value == "" {
		return "", fmt.Errorf("%s show %s: empty entry", passCmd, entry)
	}
	return value, nil
}
