// Copyright (c) The Conn3x Authors
// SPDX-License-Identifier: MPL-2.0

package eos

import (
	"slices"
	"strings"
	"testing"
)

const usersRunning = `! Command: show running-config
username admin privilege 15 role network-admin nopassword
username admin ssh-key ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIGUr c3x-lab
username bob privilege 15 role network-admin secret sha512 $6$opl$Eam
username bobby role network-operator nopassword
!
`

func TestUserCommands(t *testing.T) {
	key := "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIPqY carol@laptop"
	empty, yes, no := "", true, false
	bobs := func(hash string) bool { return hash == "$6$opl$Eam" }
	other := func(hash string) bool { return hash == "$6$new$Hash" }
	for name, tc := range map[string]struct {
		u    User
		want []string // nil: in sync
	}{
		"in sync":              {User{Name: "bob", Privilege: 15, Role: "network-admin", WantSecret: true, SecretIs: bobs}, nil},
		"in sync, no key here": {User{Name: "bob", Privilege: 15, WantSecret: true}, nil},
		"not bobby":            {User{Name: "bob", Privilege: -1, Role: "network-admin"}, nil},
		"new, no password":     {User{Name: "carol", Privilege: -1, Role: "network-operator", SSHKey: &key}, []string{"username carol role network-operator secret *", "username carol ssh-key " + key}},
		"new, nopassword":      {User{Name: "carol", Privilege: -1, NoPassword: &yes}, []string{"username carol nopassword"}},
		"new, password":        {User{Name: "dave", Privilege: 5, WantSecret: true, Secret: "$6$new$Hash", SecretIs: other}, []string{"username dave privilege 5 secret sha512 $6$new$Hash"}},
		"demote keeps secret":  {User{Name: "bob", Privilege: 1, Role: "network-operator", WantSecret: true, SecretIs: bobs}, []string{"username bob privilege 1 role network-operator"}},
		// An account that was there with another secret, or whose new
		// secret never committed: the switch's hash decides, not state.
		"another secret":    {User{Name: "bob", Privilege: -1, WantSecret: true, Secret: "$6$new$Hash", SecretIs: other}, []string{"username bob secret sha512 $6$new$Hash"}},
		"first password":    {User{Name: "bobby", Privilege: -1, WantSecret: true, Secret: "$6$new$Hash", SecretIs: other}, []string{"username bobby secret sha512 $6$new$Hash"}},
		"left as it is":     {User{Name: "bobby", Privilege: -1}, nil},
		"nopassword kept":   {User{Name: "bobby", Privilege: -1, NoPassword: &yes}, nil},
		"nopassword closed": {User{Name: "bobby", Privilege: -1, NoPassword: &no}, []string{"username bobby secret *"}},
		"nopassword opened": {User{Name: "bob", Privilege: -1, NoPassword: &yes}, []string{"username bob nopassword"}},
		"secret stays":      {User{Name: "bob", Privilege: -1, NoPassword: &no}, nil},
		"drop the key":      {User{Name: "admin", Privilege: -1, SSHKey: &empty}, []string{"no username admin ssh-key"}},
	} {
		got := tc.u.Commands(usersRunning)
		if tc.want == nil {
			if !tc.u.InSync(usersRunning) || got != nil {
				t.Errorf("%s: not in sync, commands %q", name, got)
			}
			continue
		}
		if !slices.Equal(got, tc.want) || tc.u.InSync(usersRunning) {
			t.Errorf("%s: commands %q, want %q", name, got, tc.want)
		}
	}
	if !(User{Name: "bobby", Privilege: -1}).OpenLogin(usersRunning) || (User{Name: "bobby", NoPassword: &yes}).OpenLogin(usersRunning) || (User{Name: "bob"}).OpenLogin(usersRunning) {
		t.Error("OpenLogin is for an account with nopassword that the step says nothing about")
	}
	if got := (User{Name: "bob"}).RemoveCommands(usersRunning); !slices.Equal(got, []string{"no username bob"}) {
		t.Errorf("remove: %q", got)
	}
	if (User{Name: "zed"}).RemoveCommands(usersRunning) != nil {
		t.Error("removing an account that isn't there sends nothing")
	}
}

func TestUserValidateAndGuard(t *testing.T) {
	bad, open := "not a key", true
	for want, u := range map[string]User{
		"not a user name":     {Name: "two words", Privilege: -1},
		"privilege must be":   {Name: "a", Privilege: 16},
		"not a role name":     {Name: "a", Privilege: -1, Role: "net admin"},
		"can't have password": {Name: "a", Privilege: -1, WantSecret: true, NoPassword: &open},
		"one public key line": {Name: "a", Privilege: -1, SSHKey: &bad},
	} {
		if err := u.Validate(); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%+v: err = %v, want %q", u, err, want)
		}
	}
	if ctl := "ssh-ed25519 AAAA a\x1b[2Jb"; (User{Name: "a", Privilege: -1, SSHKey: &ctl}).Validate() == nil {
		t.Error("a control character in a key's comment wasn't refused")
	}
	if err := (User{Name: "admin", Privilege: 1}).Guard("admin"); err == nil {
		t.Error("demoting the login user wasn't refused")
	}
	if err := (User{Name: "admin", Privilege: -1, Role: "network-operator"}).Guard("admin"); err == nil {
		t.Error("changing the login user's role wasn't refused")
	}
	if err := (User{Name: "bob", Privilege: 1}).Guard("admin"); err != nil {
		t.Error(err)
	}
}
