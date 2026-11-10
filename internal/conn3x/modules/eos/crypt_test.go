// Copyright (c) The Conn3x Authors
// SPDX-License-Identifier: MPL-2.0

package eos

import "testing"

func TestSecretHash(t *testing.T) {
	for _, c := range []struct{ password, salt, want string }{
		// From the SHA-crypt specification.
		{"Hello world!", "saltstring", "$6$saltstring$svn8UoSVapNtMuq1ukKS4tPQd8iKwSMHWjl/O817G3uBnIFNjnQJuesI68u4OTLiBFdcbYEdFCoEOfaS35inz1"},
		// What cEOS-lab 4.37.0F printed for "username legacy secret Old1".
		{"Old1", "4F2qCSz0f6rNlnlZ", "$6$4F2qCSz0f6rNlnlZ$5h14iNBtu9.swuTWJ36Xdqc/HEWyIlcmRQArd0NK57GoIYLLXdxSsITa4jxXeVD7gM0IiPZwuAlEWBa9Ap0fB/"},
	} {
		if got := SecretHash(c.password, c.salt); got != c.want {
			t.Errorf("SecretHash(%q, %q) = %s, want %s", c.password, c.salt, got, c.want)
		}
	}
	if s := CryptSalt([]byte("0123456789ab")); len(s) != 16 {
		t.Errorf("CryptSalt gave %q, want 16 characters", s)
	}
}
