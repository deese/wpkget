package secret

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFromPass(t *testing.T) {
	if _, err := os.Stat("/bin/sh"); err != nil {
		t.Skip("no POSIX shell available")
	}
	script := filepath.Join(t.TempDir(), "fakepass")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf 'tok-from-pass\nsecond line\n'\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := FromPass(script, "github/wpkget")
	if err != nil || got != "tok-from-pass" {
		t.Fatalf("got %q, err %v", got, err)
	}
	if _, err := FromPass(script, "--evil"); err == nil {
		t.Fatal("entry starting with '-' must be rejected")
	}
}
