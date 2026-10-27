// Copyright (c) The Conn3x Authors
// SPDX-License-Identifier: MPL-2.0

//go:build e2e

package e2e

import (
	"strings"
	"testing"
)

type sysctlVars struct {
	Port, KeyFile, KnownHosts, Package string
	WithSysctl                         bool
}

// TestSysctlCreatesDropInDir sets a kernel parameter on a host that has no
// /etc/sysctl.d, as on Rocky Linux's ubi-init image.
func TestSysctlCreatesDropInDir(t *testing.T) {
	for _, image := range linuxImages {
		t.Run(image, func(t *testing.T) {
			// Podman mounts /proc/sys read-only; a real host's root can write
			// it, and --privileged lets the container's root do the same.
			// Rootless, that stays inside the container's user and network
			// namespaces, and the port range is per network namespace, so the
			// container's value doesn't touch the machine running the tests.
			h := startHostWith(t, image, "--privileged")
			pkg := "procps"
			if rpmImage(image) {
				pkg = "procps-ng"
			}
			w := newWorkdir(t, "sysctl")
			w.writeAs("main.circuit.hcl", sysctlVars{h.port, h.keyFile, h.knownHosts, pkg, false})
			w.mustConn3x("init", "-no-color")
			w.mustConn3x("apply", "-auto-approve", "-no-color")

			// Installing procps may create the directory, so remove it after.
			h.exec("", "rm", "-rf", "/etc/sysctl.d")
			w.writeAs("main.circuit.hcl", sysctlVars{h.port, h.keyFile, h.knownHosts, pkg, true})
			w.mustConn3x("apply", "-auto-approve", "-no-color")

			if got := strings.TrimSpace(h.exec("", "stat", "-c", "%U %a", "/etc/sysctl.d")); got != "root 755" {
				t.Fatalf("/etc/sysctl.d is %q, want root 755", got)
			}
			if got := h.exec("", "cat", "/etc/sysctl.d/90-conn3x-net.ipv4.ip_local_port_range.conf"); !strings.Contains(got, "net.ipv4.ip_local_port_range = 20000 60999\n") {
				t.Fatalf("drop-in file = %q", got)
			}
			if got := strings.Join(strings.Fields(h.exec("", "sysctl", "-n", "net.ipv4.ip_local_port_range")), " "); got != "20000 60999" {
				t.Fatalf("running value = %q", got)
			}
			if out := w.mustConn3x("plan", "-no-color"); !strings.Contains(out, "No changes.") {
				t.Fatalf("plan after apply:\n%s", out)
			}
		})
	}
}
