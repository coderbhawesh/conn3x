// Copyright (c) The Conn3x Authors
// SPDX-License-Identifier: MPL-2.0

package ansible

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/zclconf/go-cty/cty"
)

// A playbook can come from someone else, so nothing in it may reach the
// circuit as anything but a string or a comment.

// stepAttrs parses converted HCL and returns the attributes of the one
// module block inside the step called name.
func stepAttrs(t *testing.T, src []byte, name string) hclsyntax.Attributes {
	t.Helper()
	f, diags := hclsyntax.ParseConfig(src, "out.tf", hcl.InitialPos)
	if diags.HasErrors() {
		t.Fatalf("output is not valid HCL: %s\n%s", diags, src)
	}
	for _, circuit := range f.Body.(*hclsyntax.Body).Blocks {
		for _, st := range circuit.Body.Blocks {
			if st.Type == "step" && st.Labels[0] == name {
				return st.Body.Blocks[0].Body.Attributes
			}
		}
	}
	t.Fatalf("no step %q in:\n%s", name, src)
	return nil
}

// literal evaluates an attribute with no variables or functions, so anything
// but a literal fails.
func literal(t *testing.T, attrs hclsyntax.Attributes, key string) cty.Value {
	t.Helper()
	a, ok := attrs[key]
	if !ok {
		t.Fatalf("no attribute %q", key)
	}
	v, diags := a.Expr.Value(nil)
	if diags.HasErrors() {
		t.Fatalf("%s is not a literal: %s", key, diags)
	}
	return v
}

var hostile = []string{
	`${upper("x")}`,
	`%{ if true }x%{ endif }`,
	`"} injected = "1`,
	"line one\ninjected = 1",
	"a\r\nb",
	`\${x}`,
	`$${x}`,
	"\u2028injected = 1",
	"tab\there",
	"\x1b[2Jcleared",
	`<<EOT`,
	`/* */ # //`,
}

func yamlQuote(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`, "\r", `\r`, "\t", `\t`, "\x1b", `\e`, "\u2028", `\L`)
	return `"` + r.Replace(s) + `"`
}

func TestHostileStringsComeBackAsTheSameStrings(t *testing.T) {
	for i, h := range hostile {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			q := yamlQuote(h)
			res := convertString(t, "- hosts: all\n  tasks:\n"+
				"    - name: motd\n      copy: {dest: /etc/motd, content: "+q+", owner: "+q+"}\n"+
				"    - name: looped\n      copy: {dest: \"/etc/{{ item }}\", content: "+q+"}\n      loop: [a, b]\n"+
				"    - name: line\n      lineinfile: {path: /etc/x, line: "+q+", regexp: "+q+"}\n"+
				"    - name: run\n      shell: {cmd: "+q+", creates: /etc/done}\n")
			if len(res.Review) != 0 {
				t.Fatalf("review: %v", res.Review)
			}
			for step, keys := range map[string][]string{"motd": {"content", "owner"}, "looped": {"content"}, "line": {"line", "regexp"}, "run": {"command"}} {
				attrs := stepAttrs(t, res.HCL, step)
				for _, k := range keys {
					if got := literal(t, attrs, k); got.Type() != cty.String || got.AsString() != h {
						t.Errorf("%s.%s = %#v, want %q\n%s", step, k, got, h, res.HCL)
					}
				}
			}
		})
	}
}

// uncommented returns the lines of converted HCL that start with marker, so
// aren't comments.
func uncommented(src []byte, marker string) []string {
	var out []string
	for _, line := range strings.Split(string(src), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), marker) {
			out = append(out, line)
		}
	}
	return out
}

func TestNULInAPlaybookIsRefused(t *testing.T) {
	path := filepath.Join(t.TempDir(), "play.yml")
	playbook := "- hosts: all\n  tasks:\n    - name: motd\n      copy: {dest: /etc/motd, content: \"\\0upper(\\\"marker\\\")\\0\"}\n"
	if err := os.WriteFile(path, []byte(playbook), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := Convert(path)
	if err == nil {
		t.Fatalf("converted a playbook with NUL bytes in a value:\n%s", res.HCL)
	}
	if !strings.Contains(err.Error(), "NUL") {
		t.Fatalf("error doesn't name the NUL byte: %v", err)
	}
}

func TestNULInAnINIInventoryIsRefused(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hosts.ini")
	if err := os.WriteFile(path, []byte("web1 ansible_user=\x00upper(\"marker\")\x00\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadInventory(path); err == nil || !strings.Contains(err.Error(), "NUL") {
		t.Fatalf("read an INI inventory with NUL bytes: %v", err)
	}
}

func TestReasonsWithNewlinesStayComments(t *testing.T) {
	const marker = "injected_attribute"
	inject := `"x\n` + marker + ` = \"not a comment\" #"`
	for name, playbook := range map[string]string{
		"missing src":    "- hosts: all\n  tasks:\n    - name: x\n      copy: {dest: /etc/a, src: " + inject + "}\n",
		"cron name":      "- hosts: all\n  tasks:\n    - name: x\n      cron: {name: " + inject + ", job: /bin/true, minute: \"1\"}\n",
		"module name":    "- hosts: all\n  tasks:\n    - name: x\n      " + inject + ": {a: b}\n",
		"keyword":        "- hosts: all\n  tasks:\n    - name: x\n      \"with_" + inject[1:] + ": [a]\n      ping:\n",
		"carriage":       "- hosts: all\n  tasks:\n    - name: x\n      copy: {dest: /etc/a, src: \"x\\r" + marker + " = 1\"}\n",
		"play inventory": "- hosts: " + inject + "\n  tasks: []\n",
	} {
		t.Run(name, func(t *testing.T) {
			res := convertString(t, playbook)
			if bad := uncommented(res.HCL, marker); len(bad) > 0 {
				t.Fatalf("playbook text outside a comment: %q\n%s", bad, res.HCL)
			}
			if _, diags := hclsyntax.ParseConfig(res.HCL, "out.tf", hcl.InitialPos); diags.HasErrors() {
				t.Fatalf("output is not valid HCL: %s\n%s", diags, res.HCL)
			}
		})
	}
}

func TestNumbersMustBeNumbers(t *testing.T) {
	for name, task := range map[string]string{
		"uid":      `user: {name: deploy, uid: "max(1, 2)"}`,
		"gid":      `group: {name: deploy, gid: "max(1, 2)"}`,
		"port":     `wait_for: {port: "max(1, 2)"}`,
		"win port": `win_wait_for: {port: "max(1, 2)"}`,
		"uid var":  `user: {name: deploy, uid: "{{ n }}"}`,
	} {
		t.Run(name, func(t *testing.T) {
			res := convertString(t, "- hosts: all\n  vars: {n: \"file(\\\"/etc/passwd\\\")\"}\n  tasks:\n    - name: x\n      "+task+"\n")
			if len(res.Review) != 1 || !strings.Contains(res.Review[0].Reason, "whole number") {
				t.Fatalf("review %v\n%s", res.Review, res.HCL)
			}
			if bad := append(uncommented(res.HCL, "max("), uncommented(res.HCL, "file(")...); len(bad) > 0 {
				t.Fatalf("expression in the output: %q", bad)
			}
		})
	}
	res := convertString(t, "- hosts: all\n  tasks:\n    - name: x\n      user: {name: deploy, uid: 1001}\n    - name: y\n      wait_for: {port: \"8080\"}\n")
	if len(res.Review) != 0 || !strings.Contains(string(res.HCL), "uid  = 1001") || !strings.Contains(string(res.HCL), "port = 8080") {
		t.Fatalf("review %v\n%s", res.Review, res.HCL)
	}
}

func TestFileNamesAreEscaped(t *testing.T) {
	dir := t.TempDir()
	name := `a${upper("x")}.conf`
	if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	for mod, key := range map[string]string{"copy": "content", "win_copy": "content"} {
		playbook := "- hosts: all\n  tasks:\n    - name: x\n      " + mod + ": {dest: 'C:/a', src: '" + name + "'}\n"
		if err := os.WriteFile(filepath.Join(dir, "play.yml"), []byte(playbook), 0o644); err != nil {
			t.Fatal(err)
		}
		res, err := Convert(filepath.Join(dir, "play.yml"))
		if err != nil || len(res.Review) != 0 {
			t.Fatalf("%s: %v, review %v", mod, err, res.Review)
		}
		want := `file("${path.module}/a$${upper(\"x\")}.conf")`
		if !strings.Contains(string(res.HCL), key+" = "+want) {
			t.Errorf("%s: file name not escaped:\n%s", mod, res.HCL)
		}
	}
}

func TestVaultValuesStayForReview(t *testing.T) {
	const cipher = "$ANSIBLE_VAULT;1.1;AES256"
	res := convertString(t, "- hosts: all\n  vars:\n    secret: !vault |\n      "+cipher+"\n      6162\n  tasks:\n"+
		"    - name: a\n      copy: {dest: /etc/a, content: !vault \""+cipher+" 6162\"}\n"+
		"    - name: b\n      copy: {dest: /etc/b, content: \"{{ secret }}\"}\n")
	if res.Converted != 0 || len(res.Review) != 2 {
		t.Fatalf("converted %d, review %v\n%s", res.Converted, res.Review, res.HCL)
	}
	for _, r := range res.Review {
		if !strings.Contains(r.Reason, "Ansible Vault") {
			t.Errorf("reason doesn't name the vault: %s", r.Reason)
		}
	}
	if strings.Contains(string(res.HCL), cipher) {
		t.Errorf("ciphertext in the output:\n%s", res.HCL)
	}
}

func TestInventoryGroupCycleIsAnError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hosts.ini")
	if err := os.WriteFile(path, []byte("[a]\nh1\n[a:children]\nb\n[b:children]\na\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadInventory(path); err == nil || !strings.Contains(err.Error(), "child of itself") {
		t.Fatalf("err = %v", err)
	}
}

// Each file imports the next twice, so reading them all the way down would
// take 2^40 reads.
func TestImportFanOutStops(t *testing.T) {
	dir := t.TempDir()
	const n = 40
	for i := 0; i < n; i++ {
		body := fmt.Sprintf("- import_tasks: t%d.yml\n- import_tasks: t%d.yml\n", i+1, i+1)
		if i == n-1 {
			body = "- name: leaf\n  ping:\n"
		}
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("t%d.yml", i)), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "play.yml"), []byte("- hosts: all\n  tasks:\n    - import_tasks: t0.yml\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := Convert(filepath.Join(dir, "play.yml"))
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, r := range res.Review {
		found = found || strings.Contains(r.Reason, "imports that multiply")
	}
	if !found {
		t.Fatalf("no review item about the import limit: %d items", len(res.Review))
	}
}

// Ansible's command module runs a program with plain arguments; a command
// step runs its text through /bin/sh.
func TestCommandWithShellSyntaxStaysForReview(t *testing.T) {
	for _, cmd := range []string{"echo one ; echo two", "echo $HOME", "ls *.conf", "echo a > /etc/b", "echo a | tee b", "echo `id`", "a && b", "echo ~root", "echo a\\ b"} {
		for _, form := range []string{"command: " + yamlQuote(cmd) + "\n      args: {creates: /etc/done}", "command: {cmd: " + yamlQuote(cmd) + ", creates: /etc/done}", "ansible.builtin.command: {cmd: " + yamlQuote(cmd) + ", creates: /etc/done}"} {
			res := convertString(t, "- hosts: all\n  tasks:\n    - name: x\n      "+form+"\n")
			if res.Converted != 0 || len(res.Review) != 1 || !strings.Contains(res.Review[0].Reason, "/bin/sh") {
				t.Errorf("%s: converted %d, review %v", form, res.Converted, res.Review)
			}
		}
	}
	res := convertString(t, "- hosts: all\n  tasks:\n"+
		"    - name: plain\n      command: /usr/bin/make -C \"/opt/my app\" install\n      args: {creates: /etc/done, chdir: /opt}\n"+
		"    - name: shell\n      shell: echo one ; echo $HOME > /etc/two\n      args: {creates: /etc/two}\n")
	if res.Converted != 2 || len(res.Review) != 0 {
		t.Fatalf("converted %d, review %v", res.Converted, res.Review)
	}
}

// A task tagged never runs in Ansible only when asked for by tag, so it
// can't become a step that runs on every apply.
func TestNeverTagStaysForReview(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "more.yml"), "- name: imported\n  service: {name: nginx, state: stopped}\n")
	for name, play := range map[string]string{
		"task list":   "- hosts: all\n  tasks:\n    - name: stop\n      service: {name: nginx, state: stopped}\n      tags: [never, maintenance]\n",
		"task string": "- hosts: all\n  tasks:\n    - name: stop\n      service: {name: nginx, state: stopped}\n      tags: maintenance, never\n",
		"play":        "- hosts: all\n  tags: never\n  tasks:\n    - name: stop\n      service: {name: nginx, state: stopped}\n",
		"import":      "- hosts: all\n  tasks:\n    - import_tasks: more.yml\n      tags: [never]\n",
	} {
		t.Run(name, func(t *testing.T) {
			writeFile(t, filepath.Join(dir, "play.yml"), play)
			res, err := Convert(filepath.Join(dir, "play.yml"))
			if err != nil {
				t.Fatal(err)
			}
			if res.Converted != 0 || len(res.Review) == 0 || !strings.Contains(res.Review[0].Reason, "never") {
				t.Fatalf("converted %d, review %v\n%s", res.Converted, res.Review, res.HCL)
			}
			if strings.Contains(string(res.HCL), "\n  step ") {
				t.Fatalf("a live step:\n%s", res.HCL)
			}
		})
	}
	res := convertString(t, "- hosts: all\n  tags: [web]\n  tasks:\n    - name: stop\n      service: {name: nginx, state: stopped}\n      tags: [always]\n")
	if res.Converted != 1 || len(res.Review) != 0 {
		t.Fatalf("converted %d, review %v", res.Converted, res.Review)
	}
}

func TestPlayKeywordsTheConverterDropsAreReported(t *testing.T) {
	for _, key := range []string{"check_mode: true", "module_defaults: {file: {mode: '0600'}}", "connection: local", "environment: {PATH: /opt/bin}", "run_once: true", "vars_prompt: [{name: x}]", "vars_files: [x.yml]", "ignore_errors: true", "user: deploy", "become_method: su"} {
		res := convertString(t, "- hosts: all\n  "+key+"\n  tasks:\n    - name: p\n      ping:\n")
		k, _, _ := strings.Cut(key, ":")
		if len(res.Review) != 1 || !strings.Contains(res.Review[0].Reason, "sets "+k) || !strings.Contains(string(res.HCL), "# TODO(convert): Play") {
			t.Errorf("%s: review %v\n%s", key, res.Review, res.HCL)
		}
	}
	res := convertString(t, "- name: web\n  hosts: all\n  become: true\n  become_method: sudo\n  gather_facts: false\n  serial: 1\n  any_errors_fatal: true\n  tags: [web]\n  vars: {a: b}\n  tasks:\n    - name: p\n      ping:\n")
	if len(res.Review) != 0 {
		t.Fatalf("review %v", res.Review)
	}
}

// Who a play's tasks run as can be set in files beside it, not only in the
// play, and a step would then run as someone else.
func TestFilesBesideThePlayThatChangeTheUser(t *testing.T) {
	const play = "- hosts: all\n  become: true\n  tasks:\n    - name: conf\n      copy: {dest: /srv/app/conf, content: x, mode: '0600'}\n    - name: pkg\n      package: {name: nginx}\n"
	for name, tc := range map[string]struct{ file, content, play, want string }{
		"group_vars file":  {"group_vars/all.yml", "ansible_become_user: app\n", play, "group_vars/all.yml"},
		"group_vars dir":   {"group_vars/web/main.yml", "x: 1\nansible_become_user: app\n", play, "group_vars/web/main.yml"},
		"host_vars":        {"host_vars/web1", "ansible_become_user: app\n", play, "host_vars/web1"},
		"ansible.cfg":      {"ansible.cfg", "[privilege_escalation]\nbecome_user = app\n", play, "ansible.cfg"},
		"cfg become":       {"ansible.cfg", "[privilege_escalation]\nbecome=True\n", strings.Replace(play, "  become: true\n", "", 1), "ansible.cfg"},
		"vars_files":       {"vars/main.yml", "ansible_become_user: app\n", strings.Replace(play, "  tasks:", "  vars_files: [vars/main.yml]\n  tasks:", 1), "vars/main.yml"},
		"vault":            {"group_vars/all.yml", "$ANSIBLE_VAULT;1.1;AES256\n6162\n", play, "encrypted"},
		"login user":       {"group_vars/all.yml", "ansible_user: deploy\n", strings.Replace(play, "  become: true\n", "", 1), "group_vars/all.yml"},
		"cfg become both":  {"ansible.cfg", "[privilege_escalation]\nbecome = True\n", play, ""},
		"harmless":         {"group_vars/all.yml", "http_port: 80\n# ansible_become_user: app\n", play, ""},
		"login and become": {"group_vars/all.yml", "ansible_user: deploy\n", play, ""},
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, tc.file)), 0o755); err != nil {
				t.Fatal(err)
			}
			writeFile(t, filepath.Join(dir, tc.file), tc.content)
			writeFile(t, filepath.Join(dir, "play.yml"), tc.play)
			res, err := Convert(filepath.Join(dir, "play.yml"))
			if err != nil {
				t.Fatal(err)
			}
			var reasons []string
			for _, r := range res.Review {
				if r.Task == "conf" {
					reasons = append(reasons, r.Reason)
				}
			}
			if tc.want == "" {
				if len(reasons) != 0 {
					t.Fatalf("review %v", reasons)
				}
				return
			}
			// The package installs the same whoever runs it, so it converts.
			if len(reasons) != 1 || !strings.Contains(reasons[0], tc.want) || res.Converted != 1 {
				t.Fatalf("converted %d, review %v\n%s", res.Converted, res.Review, res.HCL)
			}
		})
	}
}

// Without mode, Ansible leaves an existing file's mode alone and a file step
// sets 0644, so the step says so.
func TestCopyWithoutModeGetsANote(t *testing.T) {
	res := convertString(t, "- hosts: all\n  tasks:\n    - name: key\n      copy: {dest: /etc/app.key, content: x}\n    - name: conf\n      copy: {dest: /etc/app.conf, content: x, mode: '0600'}\n")
	if res.Converted != 2 || len(res.Review) != 0 || len(res.Notes) != 1 || res.Notes[0].Task != "key" || !strings.Contains(res.Notes[0].Reason, "0644") {
		t.Fatalf("converted %d, review %v, notes %v", res.Converted, res.Review, res.Notes)
	}
}

// A role from someone else can ship a link in files/ that points anywhere on
// the controller; converting it would copy that file into the circuit.
func TestALinkOutOfTheRoleStaysForReview(t *testing.T) {
	outside := filepath.Join(t.TempDir(), "id_rsa")
	writeFile(t, outside, "PRIVATE")
	dir := writeRole(t, map[string]string{
		"tasks/main.yml":      "- name: key\n  copy: {src: key, dest: /etc/key, mode: '0600'}\n- name: conf\n  template: {src: conf.j2, dest: /etc/conf, mode: '0600'}\n- name: ok\n  copy: {src: inside, dest: /etc/inside, mode: '0600'}\n",
		"files/real":          "x",
		"templates/.keep":     "",
		"files/.keep-outside": "",
	})
	for link, target := range map[string]string{"files/key": outside, "templates/conf.j2": outside, "files/inside": "real"} {
		if err := os.Symlink(target, filepath.Join(dir, link)); err != nil {
			t.Skip(err)
		}
	}
	res, err := ConvertRole(dir)
	if err != nil {
		t.Fatal(err)
	}
	if res.Converted != 1 || len(res.Review) != 2 {
		t.Fatalf("converted %d, review %v", res.Converted, res.Review)
	}
	for _, r := range res.Review {
		if !strings.Contains(r.Reason, "is a link to a file that is") {
			t.Errorf("reason: %s", r.Reason)
		}
	}
	for p, b := range res.Files {
		if strings.Contains(string(b), "PRIVATE") {
			t.Errorf("%s holds the file from outside the role", p)
		}
	}
}

// A task left for review is echoed in a comment, without its secrets.
func TestEchoedTaskLeavesOutSecretsByAnyCommonName(t *testing.T) {
	for _, key := range []string{"secret", "token", "passphrase", "api_key", "apikey", "private_key", "access_key", "secret_key", "client_secret", "auth_token", "passwd", "db_pass", "ssh_passphrase", "password", "login_password"} {
		for _, form := range []string{"some.unknown.module: {" + key + ": hunter2, name: x}", "some.unknown.module: name=x " + key + "=hunter2"} {
			res := convertString(t, "- hosts: all\n  tasks:\n    - name: t\n      "+form+"\n")
			if len(res.Review) != 1 || strings.Contains(string(res.HCL), "hunter2") {
				t.Errorf("%s: the secret is in the output", form)
			}
		}
	}
	res := convertString(t, "- hosts: all\n  tasks:\n    - name: t\n      some.unknown.module: {key: ssh-ed25519 AAAA, keyboard: us, bypass: x}\n")
	for _, kept := range []string{"ssh-ed25519 AAAA", "keyboard: us", "bypass: x"} {
		if !strings.Contains(string(res.HCL), kept) {
			t.Errorf("%q was left out:\n%s", kept, res.HCL)
		}
	}
}

// The same for a role: its defaults, vars and templates come from its author.
func TestHostileRoleValuesComeBackAsTheSameStrings(t *testing.T) {
	for i, h := range hostile {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			q := yamlQuote(h)
			dir := writeRole(t, map[string]string{
				"defaults/main.yml":     "greeting: " + q + "\n",
				"vars/main.yml":         "fixed: " + q + "\n",
				"tasks/main.yml":        "- name: motd\n  copy: {dest: /etc/motd, content: \"{{ greeting }}\", owner: \"{{ fixed }}\", mode: '0644'}\n- name: conf\n  template: {src: conf.j2, dest: /etc/conf, mode: '0644'}\n",
				"templates/conf.j2":     "a={{ fixed }}\n" + strings.ReplaceAll(h, "\r", "") + "\n",
				"meta/main.yml":         "dependencies: [" + q + "]\n",
				"files/.keep":           "",
				"handlers/main.yml":     "[]\n",
				"meta/argument_specs.x": "",
			})
			res, err := ConvertRole(dir)
			if err != nil {
				t.Fatal(err)
			}
			if res.Converted != 2 {
				t.Fatalf("converted %d, review %v", res.Converted, res.Review)
			}
			for name, src := range res.Files {
				if !strings.HasSuffix(name, ".hcl") && !strings.HasSuffix(name, ".tf") {
					continue
				}
				f, diags := hclsyntax.ParseConfig(src, name, hcl.InitialPos)
				if diags.HasErrors() {
					t.Fatalf("%s is not valid HCL: %s\n%s", name, diags, src)
				}
				for _, b := range f.Body.(*hclsyntax.Body).Blocks {
					switch {
					case b.Type == "variable" && b.Labels[0] == "greeting":
						if got := literal(t, b.Body.Attributes, "default"); got.AsString() != h {
							t.Errorf("default = %#v, want %q", got, h)
						}
					case b.Type == "circuit":
						for _, st := range b.Body.Blocks {
							if st.Type != "step" {
								continue
							}
							attrs := st.Body.Blocks[0].Body.Attributes
							if st.Labels[0] == "motd" {
								if got := literal(t, attrs, "owner"); got.AsString() != h {
									t.Errorf("owner = %#v, want %q", got, h)
								}
							}
							if v, ok := attrs["vars"]; ok {
								val, diags := v.Expr.Value(nil)
								if diags.HasErrors() || val.GetAttr("fixed").AsString() != h {
									t.Errorf("vars = %#v %s, want fixed = %q", val, diags, h)
								}
							}
						}
					case b.Type != "variable" && b.Type != "locals" && b.Type != "output" && b.Type != "terraform":
						t.Errorf("unexpected block %s %v in %s:\n%s", b.Type, b.Labels, name, src)
					}
				}
			}
		})
	}
}
