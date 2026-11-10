// Copyright (c) The Conn3x Authors
// SPDX-License-Identifier: MPL-2.0

package fetch

import (
	"context"
	"testing"

	"github.com/opentofu/opentofu/internal/conn3x/modules/moduletest"
)

// Root doesn't unpack into a directory someone else owns: unzip, and tar
// before 1.35, go through the links its owner has put there. A creates path
// inside dest already led there as its owner; one outside it didn't.
func TestArchiveUnpacksAsTheOwnerOfDest(t *testing.T) {
	dir := moduletest.RootDir(t)
	moduletest.Sh(t, dir, `mkdir -m 0700 guard; mkdir -p src/sub; echo x > src/sub/f
tar -cf a.tar -C src sub/f
if command -v zip >/dev/null; then (cd src && zip -q ../a.zip sub/f); fi`)
	for _, name := range []string{"a.tar", "a.zip"} {
		t.Run(name, func(t *testing.T) {
			if name == "a.zip" && moduletest.Sh(t, dir, "command -v unzip >/dev/null && ls a.zip 2>/dev/null || true") == "" {
				t.Skip("no zip or unzip here")
			}
			moduletest.Sh(t, dir, "rm -rf app; mkdir app; ln -s ../guard app/sub; chown -hR nobody app")
			_ = ApplyArchive(context.Background(), moduletest.Local{}, Archive{Src: dir + "/" + name, Dest: dir + "/app", Creates: dir + "/unpacked"})
			if got := moduletest.Sh(t, dir, "ls -A guard"); got != "" {
				t.Fatalf("root unpacked %s through the owner's link, into the guarded directory", got)
			}
		})
	}
}
