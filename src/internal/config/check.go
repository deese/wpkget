package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"

	"gopkg.in/yaml.v3"
)

const redactedValue = "***"

// Validate parses the config file at path in strict mode: unknown keys (e.g. a
// typo such as "gitlab_host") are reported instead of being silently ignored.
// A missing file is returned as an error that satisfies os.IsNotExist.
func Validate(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)

	var c Config
	if err := dec.Decode(&c); err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("config: %s: %w", path, err)
	}
	return nil
}

// RedactedYAML renders the config as YAML with secrets masked.
func (c *Config) RedactedYAML() ([]byte, error) {
	masked := *c
	if masked.ZipdownToken != "" {
		masked.ZipdownToken = redactedValue
	}
	return yaml.Marshal(&masked)
}
