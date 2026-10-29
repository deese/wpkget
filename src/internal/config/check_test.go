package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func writeTemp(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestValidate(t *testing.T) {
	if err := Validate(writeTemp(t, "")); err != nil {
		t.Fatalf("empty file must be valid: %v", err)
	}
	if err := Validate(writeTemp(t, "bin_dir: x\ngitlab_hosts:\n  h:\n    prefix: /gitlab\n")); err != nil {
		t.Fatalf("valid file rejected: %v", err)
	}
	err := Validate(writeTemp(t, "gitlab_host:\n  h: {}\n"))
	if err == nil || !strings.Contains(err.Error(), "gitlab_host") {
		t.Fatalf("typo in key must be reported, got %v", err)
	}
	err = Validate(writeTemp(t, "gitlab_hosts:\n  h:\n    token_evn: X\n"))
	if err == nil || !strings.Contains(err.Error(), "token_evn") {
		t.Fatalf("typo in nested key must be reported, got %v", err)
	}
	if err := Validate(filepath.Join(t.TempDir(), "missing.yaml")); !os.IsNotExist(err) {
		t.Fatalf("missing file must satisfy os.IsNotExist, got %v", err)
	}
}

func TestRedactedYAML(t *testing.T) {
	c := &Config{ZipdownToken: "supersecret", GitLabHosts: map[string]GitLabHost{"h": {TokenPass: "gitlab/x"}}}
	out, err := c.RedactedYAML()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "supersecret") || !strings.Contains(string(out), redactedValue) {
		t.Fatalf("token not redacted:\n%s", out)
	}
	if c.ZipdownToken != "supersecret" {
		t.Fatal("RedactedYAML must not mutate the receiver")
	}
}

func TestSplitCommand(t *testing.T) {
	cases := map[string][]string{
		"":                                {},
		"   ":                             {},
		"vim":                             {"vim"},
		"code -w":                         {"code", "-w"},
		`"C:\Program Files\ed\ed.exe" -w`: {`C:\Program Files\ed\ed.exe`, "-w"},
		`ed "" x`:                         {"ed", "", "x"},
	}
	for in, want := range cases {
		got := splitCommand(in)
		if len(got) == 0 && len(want) == 0 {
			continue
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("splitCommand(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestLoadProgress(t *testing.T) {
	cfg, err := Load(writeTemp(t, ""))
	if err != nil || cfg.Progress != "auto" {
		t.Fatalf("default progress = %q, err %v", cfg.Progress, err)
	}
	cfg, err = Load(writeTemp(t, "progress: never\n"))
	if err != nil || cfg.Progress != "never" {
		t.Fatalf("progress = %q, err %v", cfg.Progress, err)
	}
	if _, err := Load(writeTemp(t, "progress: sometimes\n")); err == nil {
		t.Fatal("invalid progress value must be rejected")
	}
}
