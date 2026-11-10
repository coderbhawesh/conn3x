// Copyright (c) The Conn3x Authors
// SPDX-License-Identifier: MPL-2.0

package moduletest

import (
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
)

// RootDir is a new directory only root can change, for a test of what root
// does on a path. It skips the test unless it runs as root on Linux: build
// it with go test -c and run it in a throwaway container.
func RootDir(t *testing.T) string {
	t.Helper()
	if runtime.GOOS != "linux" || os.Geteuid() != 0 {
		t.Skip("needs root on Linux")
	}
	// Not under t.TempDir, whose parents may belong to anyone.
	const kept = "/var/conn3x-route-test"
	if err := os.MkdirAll(kept, 0o755); err != nil {
		t.Fatal(err)
	}
	dir, err := os.MkdirTemp(kept, "t")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

// Sh runs a script in dir and returns what it printed.
func Sh(t *testing.T, dir, script string) string {
	t.Helper()
	c := exec.Command("sh", "-ec", script)
	c.Dir = dir
	out, err := c.CombinedOutput()
	if err != nil {
		t.Fatalf("%s: %v\n%s", script, err, out)
	}
	return strings.TrimSpace(string(out))
}
