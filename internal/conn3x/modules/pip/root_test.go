// Copyright (c) The Conn3x Authors
// SPDX-License-Identifier: MPL-2.0

package pip

import (
	"context"
	"strings"
	"testing"

	"github.com/opentofu/opentofu/internal/conn3x/modules/moduletest"
)

// A link someone else owns at the venv's path doesn't lead root into a venv
// of root's own.
func TestALinkToRootsVenvIsRefused(t *testing.T) {
	dir := moduletest.RootDir(t)
	moduletest.Sh(t, dir, "mkdir -m 1777 s; mkdir -p venv/bin; ln -s ../venv s/venv; chown -h nobody s/venv")
	_, err := Read(context.Background(), moduletest.Local{}, Desired{Name: "requests", Venv: dir + "/s/venv", State: Present})
	if err == nil || !strings.Contains(err.Error(), "refusing") || !strings.Contains(err.Error(), "set user") {
		t.Fatalf("err = %v", err)
	}
}

// In a directory anyone can create in, a venv that isn't there reads as
// missing and is never run.
func TestAMissingVenvInAStickyDirectory(t *testing.T) {
	dir := moduletest.RootDir(t)
	moduletest.Sh(t, dir, "mkdir -m 1777 s")
	a, err := Read(context.Background(), moduletest.Local{}, Desired{Name: "requests", Venv: dir + "/s/venv", State: Present})
	if err != nil || a.Installed {
		t.Fatalf("got %+v, %v", a, err)
	}
}
