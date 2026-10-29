package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/deese/wpkget/src/internal/config"
	"github.com/deese/wpkget/src/internal/gitlab"
	"github.com/deese/wpkget/src/internal/packages"
)

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestRunConfigExitCodes(t *testing.T) {
	good := writeConfig(t, "bin_dir: x\n")
	typo := writeConfig(t, "gitlab_host:\n  h: {}\n")
	badCA := writeConfig(t, "gitlab_hosts:\n  git.example.com:\n    ca_file: /nonexistent/ca.pem\n")
	missing := filepath.Join(t.TempDir(), "nope.yaml")

	cases := []struct {
		name string
		args []string
		want int
	}{
		{"no subcommand", []string{"--config", good}, exitGeneral},
		{"unknown subcommand", []string{"bogus", "--config", good}, exitGeneral},
		{"path", []string{"path", "--config", good}, exitOK},
		{"path packages", []string{"--config", good, "path", "packages"}, exitOK},
		{"path bad target", []string{"path", "foo", "--config", good}, exitGeneral},
		{"check ok", []string{"check", "--config", good}, exitOK},
		{"check unknown key", []string{"check", "--config", typo}, exitGeneral},
		{"check missing file", []string{"check", "--config", missing}, exitOK},
		{"check bad ca_file", []string{"check", "--config", badCA}, exitGeneral},
		{"check extra arg", []string{"check", "x", "--config", good}, exitGeneral},
		{"show", []string{"show", "--config", good}, exitOK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := runConfig(tc.args); got != tc.want {
				t.Fatalf("runConfig(%v) = %d, want %d", tc.args, got, tc.want)
			}
		})
	}
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Fatal("check must not create a missing config file")
	}
}

func TestFindTracked(t *testing.T) {
	pkgList := &packages.List{Packages: []packages.Entry{
		{Repo: "cli/cli"},
		{Repo: "grp/sub/proj", Provider: "gitlab", Host: "git.example.com"},
	}}

	cases := []struct {
		arg     string
		wantErr bool
	}{
		{"cli/cli", false},
		{"grp/sub/proj", false}, // plain repo matches regardless of host
		{"https://git.example.com/grp/sub/proj", false},
		{"https://github.com/cli/cli", false},
		{"https://github.com/other/tool", true},
		{"other/tool", true},
		{"https://unknown.example.org/grp/proj", true},
	}
	gitlab.Configure(&config.Config{GitLabHosts: map[string]config.GitLabHost{"git.example.com": {}}})
	for _, tc := range cases {
		_, err := findTracked(pkgList, tc.arg)
		if (err != nil) != tc.wantErr {
			t.Errorf("findTracked(%q) error = %v, wantErr %v", tc.arg, err, tc.wantErr)
		}
	}
}
