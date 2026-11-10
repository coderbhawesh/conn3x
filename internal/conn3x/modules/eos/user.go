// Copyright (c) The Conn3x Authors
// SPDX-License-Identifier: MPL-2.0

package eos

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// User is a local account on a switch, as eos_user owns it. Settings left
// unset are left alone. EOS keeps a salted hash of a secret, and Conn3x sends
// the hash, not the password, so a read can tell whether the account holds
// the step's secret.
type User struct {
	Name       string
	Privilege  int    // 0 to 15; -1 to leave
	Role       string // "" to leave
	WantSecret bool   // the step sets a password
	Secret     string // its hash, as SecretHash writes it, at apply only
	// SecretIs reports whether hash, as the switch prints it, is the step's
	// password. Nil when a read can't tell, having no HMAC key: then any
	// secret counts.
	SecretIs func(hash string) bool
	// NoPassword true lets the account log in with no password at all;
	// false never does; nil leaves an account that is there as it is. A new
	// account without a password gets "secret *", which no password matches.
	NoPassword *bool
	SSHKey     *string
}

var (
	userName = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.@-]{0,63}$`)
	roleName = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
	sshKey   = regexp.MustCompile(`^(ssh-(ed25519|rsa|dss)|ecdsa-sha2-nistp(256|384|521)|sk-ssh-ed25519@openssh\.com) [A-Za-z0-9+/=]+( [^\r\n]*)?$`)
)

// Validate checks a User before anything reaches the switch.
func (u User) Validate() error {
	if !userName.MatchString(u.Name) {
		return fmt.Errorf("name %q is not a user name EOS takes: letters, digits, _ . @ and -, up to 64", u.Name)
	}
	if u.Privilege < -1 || u.Privilege > 15 {
		return fmt.Errorf("privilege must be 0 to 15, not %d", u.Privilege)
	}
	if u.Role != "" && !roleName.MatchString(u.Role) {
		return fmt.Errorf("role %q is not a role name, such as network-admin", u.Role)
	}
	if u.WantSecret && u.NoPassword != nil && *u.NoPassword {
		return errors.New("nopassword = true lets the account in with no password, so it can't have password_wo as well")
	}
	if u.SSHKey != nil && *u.SSHKey != "" && (!sshKey.MatchString(*u.SSHKey) || hasControl(*u.SSHKey)) {
		return errors.New("ssh_key must be one public key line, such as \"ssh-ed25519 AAAA... alice@laptop\"")
	}
	return nil
}

// lines are the account's main line ("username alice role ... nopassword")
// and its ssh-key line, from the top level of config.
func (u User) lines(config string) (main, key string) {
	prefix := "username " + u.Name + " "
	for _, k := range parse(config).kids {
		rest, ok := strings.CutPrefix(k.text, prefix)
		switch {
		case !ok:
		case strings.HasPrefix(rest, "ssh-key "):
			key = k.text
		default:
			main = k.text
		}
	}
	return main, key
}

// fields reads a main line's privilege (1 when EOS leaves it out), role,
// secret ("" for none; its hash, or "*" for one nothing matches) and
// whether it says nopassword.
func fields(main string) (privilege int, role, secret string, nopassword bool) {
	privilege = 1
	f := strings.Fields(main)
	for i := 2; i < len(f); i++ {
		switch f[i] {
		case "privilege":
			if i+1 < len(f) {
				privilege, _ = strconv.Atoi(f[i+1])
			}
		case "role":
			if i+1 < len(f) {
				role = f[i+1]
			}
		case "secret":
			secret = f[len(f)-1] // after its type, as in "secret sha512 $6$..."
		case "nopassword":
			nopassword = true
		}
	}
	return privilege, role, secret, nopassword
}

// secretOK reports whether a main line's secret is the step's.
func (u User) secretOK(secret string) bool {
	if u.SecretIs == nil {
		return secret != ""
	}
	return u.SecretIs(secret)
}

// InSync reports whether the account matches, its secret included.
func (u User) InSync(config string) bool {
	main, key := u.lines(config)
	if main == "" {
		return false
	}
	priv, role, secret, nopassword := fields(main)
	if (u.Privilege >= 0 && priv != u.Privilege) || (u.Role != "" && role != u.Role) || (u.WantSecret && !u.secretOK(secret)) {
		return false
	}
	if u.NoPassword != nil && !u.WantSecret && *u.NoPassword != nopassword {
		return false
	}
	return u.SSHKey == nil || key == u.keyLine()
}

// OpenLogin reports whether the account is there with nopassword while the
// step says nothing about it: it then logs in with an empty password.
func (u User) OpenLogin(config string) bool {
	main, _ := u.lines(config)
	_, _, _, nopassword := fields(main)
	return nopassword && !u.WantSecret && u.NoPassword == nil
}

func (u User) keyLine() string {
	if u.SSHKey == nil || *u.SSHKey == "" {
		return ""
	}
	return "username " + u.Name + " ssh-key " + *u.SSHKey
}

// Commands are what makes the switch's account match, or nil. EOS merges a
// username line into what's there, so changing the role keeps the secret.
func (u User) Commands(config string) []string {
	main, key := u.lines(config)
	base := "username " + u.Name
	var settings string
	if u.Privilege >= 0 {
		settings += " privilege " + strconv.Itoa(u.Privilege)
	}
	if u.Role != "" {
		settings += " role " + u.Role
	}
	var cmds []string
	open := u.NoPassword != nil && *u.NoPassword
	if main == "" {
		// A new account needs a secret or nopassword in the same line.
		switch {
		case u.WantSecret:
			cmds = append(cmds, base+settings+" secret sha512 "+u.Secret)
		case open:
			cmds = append(cmds, base+settings+" nopassword")
		default:
			cmds = append(cmds, base+settings+" secret *")
		}
	} else {
		priv, role, secret, nopassword := fields(main)
		if (u.Privilege >= 0 && priv != u.Privilege) || (u.Role != "" && role != u.Role) {
			cmds = append(cmds, base+settings)
		}
		switch {
		case u.WantSecret:
			if !u.secretOK(secret) {
				cmds = append(cmds, base+" secret sha512 "+u.Secret)
			}
		case open && !nopassword:
			cmds = append(cmds, base+" nopassword")
		case u.NoPassword != nil && !open && nopassword:
			cmds = append(cmds, base+" secret *")
		}
	}
	if u.SSHKey != nil && key != u.keyLine() {
		if want := u.keyLine(); want != "" {
			cmds = append(cmds, want)
		} else {
			cmds = append(cmds, "no "+base+" ssh-key")
		}
	}
	return cmds
}

// RemoveCommands delete the account, when it's there.
func (u User) RemoveCommands(config string) []string {
	if main, _ := u.lines(config); main == "" {
		return nil
	}
	return []string{"no username " + u.Name}
}

// Guard refuses what would lock out login, the account Conn3x uses: a
// privilege under 15, which leaves its login unprivileged, or another role.
func (u User) Guard(login string) error {
	if u.Name != login {
		return nil
	}
	if u.Privilege >= 0 && u.Privilege < 15 {
		return fmt.Errorf("Conn3x logs in as %s and needs privilege 15; lowering it would leave every later apply unprivileged", login)
	}
	if u.Role != "" && u.Role != "network-admin" {
		return fmt.Errorf("Conn3x logs in as %s, and a role other than network-admin could stop it changing the switch", login)
	}
	return nil
}
