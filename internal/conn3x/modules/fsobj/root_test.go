// Copyright (c) The Conn3x Authors
// SPDX-License-Identifier: MPL-2.0

package fsobj

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/opentofu/opentofu/internal/conn3x/modules/moduletest"
)

// In a directory anyone can create in, a link step doesn't go through a name
// someone could have guessed and put there first.
func TestApplyLinkInAStickyDirectory(t *testing.T) {
	dir := moduletest.RootDir(t)
	moduletest.Sh(t, dir, "mkdir -m 1777 s")
	// The names a temporary link called after its shell's pid would get next.
	last, _ := strconv.Atoi(moduletest.Sh(t, dir, "cat /proc/sys/kernel/ns_last_pid"))
	for pid := last; pid < last+3000; pid++ {
		name := fmt.Sprintf("%s/s/.conn3x-link.%d", dir, pid)
		// ln refuses a file or a link at the name, but goes into a directory.
		if err := os.Mkdir(name, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Lchown(name, 65534, 65534); err != nil {
			t.Fatal(err)
		}
	}
	if err := ApplyLink(context.Background(), moduletest.Local{}, Link{Path: dir + "/s/l", Target: "/etc/hostname"}); err != nil {
		t.Fatal(err)
	}
	if got := moduletest.Sh(t, dir, "stat -c '%U %N' s/l"); !strings.HasPrefix(got, "root ") || !strings.HasSuffix(got, "-> '/etc/hostname'") {
		t.Fatalf("the link is %s", got)
	}
}

// A directory step doesn't take over, or go through, what someone else put
// at its path in a directory anyone can create in.
func TestApplyDirectoryInAStickyDirectory(t *testing.T) {
	dir := moduletest.RootDir(t)
	moduletest.Sh(t, dir, "mkdir -m 1777 s; mkdir -m 0700 guard")
	d := Directory{Path: dir + "/s/d", Mode: "0777", Owner: "nobody"}
	if err := ApplyDirectory(context.Background(), moduletest.Local{}, d); err != nil {
		t.Fatal(err)
	}
	// Now nobody's, so nobody can swap it for a link: a second apply changes
	// the link, if anything, and never what it points to.
	moduletest.Sh(t, dir, "rmdir s/d; ln -s ../guard s/d; chown -h nobody s/d")
	if err := ApplyDirectory(context.Background(), moduletest.Local{}, d); err == nil {
		t.Error("applied over someone's link")
	}
	if got := moduletest.Sh(t, dir, "stat -c '%U %a' guard"); got != "root 700" {
		t.Fatalf("the directory behind the link is now %s", got)
	}
}
