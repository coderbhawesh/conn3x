// Copyright (c) The Conn3x Authors
// SPDX-License-Identifier: MPL-2.0

package file_test

import (
	"context"
	"strings"
	"testing"

	"github.com/opentofu/opentofu/internal/conn3x/modules/file"
	"github.com/opentofu/opentofu/internal/conn3x/modules/moduletest"
)

// A file someone put at the path first, in a directory anyone can create
// in, doesn't make them the owner of what is written there.
func TestApplyDoesNotKeepASquattersOwner(t *testing.T) {
	dir := moduletest.RootDir(t)
	moduletest.Sh(t, dir, "mkdir -m 1777 s; touch s/secret s/own; chown nobody s/secret s/own")
	ctx, r := context.Background(), moduletest.Local{}
	err := file.Apply(ctx, r, file.Desired{Path: dir + "/s/secret", Content: "hunter2\n", Mode: "0600"})
	if err == nil || !strings.Contains(err.Error(), "set owner and group") {
		t.Errorf("err = %v", err)
	}
	if got := moduletest.Sh(t, dir, "cat s/secret"); got != "" {
		t.Errorf("the squatter's file now holds %q", got)
	}
	// Named as the owner, they get the file; with both set, nothing is kept.
	if err := file.Apply(ctx, r, file.Desired{Path: dir + "/s/own", Content: "x\n", Mode: "0600", Owner: "nobody"}); err != nil {
		t.Error(err)
	}
	if err := file.Apply(ctx, r, file.Desired{Path: dir + "/s/secret", Content: "hunter2\n", Mode: "0600", Owner: "root", Group: "root"}); err != nil {
		t.Error(err)
	}
	if got := moduletest.Sh(t, dir, "stat -c '%U:%G %a' s/secret s/own | tr '\n' ' '"); !strings.HasPrefix(got, "root:root 600 nobody:") {
		t.Errorf("owners and modes: %s", got)
	}
	// A read doesn't go through a link someone put there.
	moduletest.Sh(t, dir, "echo root-only > keep; chmod 0600 keep; ln -s ../keep s/l; chown -h nobody s/l")
	if a, err := file.Read(ctx, r, dir+"/s/l"); err == nil && a.Content != "" {
		t.Errorf("read %q through someone's link", a.Content)
	}
}
