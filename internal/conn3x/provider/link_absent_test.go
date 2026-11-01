// Copyright (c) The Conn3x Authors
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zclconf/go-cty/cty"

	"github.com/opentofu/opentofu/internal/providers"
)

// linkTest drives conn3x_link against the test SSH server, as fileTest
// does conn3x_file.
type linkTest struct{ *fileTest }

func newLinkTest(t *testing.T) *linkTest {
	f := newFileTest(t)
	f.path = filepath.Join(f.server.Dir, "current")
	return &linkTest{f}
}

func (l *linkTest) config(attrs map[string]cty.Value) cty.Value {
	vals := objFor(linkSchema(), attrs).AsValueMap()
	vals["path"], vals["ssh"] = cty.StringVal(l.path), l.conn
	return cty.ObjectVal(vals)
}

func (l *linkTest) plan(prior, config cty.Value) providers.PlanResourceChangeResponse {
	l.t.Helper()
	proposed := config
	if !prior.IsNull() {
		vals := config.AsValueMap()
		for _, name := range []string{"id", "revision", "on_remove", "in_sync"} {
			if vals[name].IsNull() {
				vals[name] = prior.GetAttr(name)
			}
		}
		proposed = cty.ObjectVal(vals)
	}
	resp := l.p.PlanResourceChange(ctx, providers.PlanResourceChangeRequest{
		TypeName: "conn3x_link", PriorState: prior, ProposedNewState: proposed, Config: config,
	})
	if resp.Diagnostics.HasErrors() {
		l.t.Fatalf("plan: %s", resp.Diagnostics.Err())
	}
	return resp
}

func (l *linkTest) tryApply(prior, planned, config cty.Value) providers.ApplyResourceChangeResponse {
	return l.p.ApplyResourceChange(ctx, providers.ApplyResourceChangeRequest{
		TypeName: "conn3x_link", PriorState: prior, PlannedState: planned, Config: config,
	})
}

func (l *linkTest) apply(prior, planned, config cty.Value) cty.Value {
	l.t.Helper()
	resp := l.tryApply(prior, planned, config)
	if resp.Diagnostics.HasErrors() {
		l.t.Fatalf("apply: %s", resp.Diagnostics.Err())
	}
	return resp.NewState
}

func (l *linkTest) read(prior cty.Value) cty.Value {
	l.t.Helper()
	resp := l.p.ReadResource(ctx, providers.ReadResourceRequest{TypeName: "conn3x_link", PriorState: prior})
	if resp.Diagnostics.HasErrors() {
		l.t.Fatalf("read: %s", resp.Diagnostics.Err())
	}
	return resp.NewState
}

// applyAbsent plans and applies state = "absent" over prior.
func (l *linkTest) applyAbsent(prior cty.Value) cty.Value {
	l.t.Helper()
	cfg := l.config(absent)
	if prior == cty.NilVal {
		prior = cty.NullVal(cfg.Type())
	}
	return l.apply(prior, l.plan(prior, cfg).PlannedState, cfg)
}

// tryAbsent applies state = "absent" to a path that wasn't managed before.
func (l *linkTest) tryAbsent() providers.ApplyResourceChangeResponse {
	cfg := l.config(absent)
	null := cty.NullVal(cfg.Type())
	return l.tryApply(null, l.plan(null, cfg).PlannedState, cfg)
}

func TestLinkAbsentRemovesLink(t *testing.T) {
	l := newLinkTest(t)
	target := filepath.Join(l.server.Dir, "release")
	if err := os.WriteFile(target, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, l.path); err != nil {
		t.Fatal(err)
	}
	state := l.applyAbsent(cty.NilVal)
	if _, err := os.Lstat(l.path); !os.IsNotExist(err) {
		t.Fatal("link still exists with state = absent")
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("the link's target was removed: %v", err)
	}
	if got := state.GetAttr("in_sync"); !got.RawEquals(cty.True) {
		t.Fatalf("in_sync = %#v, want true", got)
	}

	// Once gone, the next plan changes nothing.
	read := l.read(state)
	if planned := l.plan(read, l.config(absent)).PlannedState; !planned.RawEquals(read) {
		t.Fatalf("second plan changes something:\nplanned %#v\nprior   %#v", planned, read)
	}
}

func TestLinkAbsentRemovesDanglingLink(t *testing.T) {
	l := newLinkTest(t)
	if err := os.Symlink(filepath.Join(l.server.Dir, "nowhere"), l.path); err != nil {
		t.Fatal(err)
	}
	l.applyAbsent(cty.NilVal)
	if _, err := os.Lstat(l.path); !os.IsNotExist(err) {
		t.Fatal("dangling link still exists with state = absent")
	}
}

func TestLinkAbsentMissingPathPlansClean(t *testing.T) {
	l := newLinkTest(t)
	state := l.applyAbsent(cty.NilVal)
	if planned := l.plan(l.read(state), l.config(absent)).PlannedState; !planned.GetAttr("in_sync").RawEquals(cty.True) {
		t.Fatalf("in_sync = %#v, want true", planned.GetAttr("in_sync"))
	}
}

func TestLinkAbsentKeepsItGone(t *testing.T) {
	l := newLinkTest(t)
	state := l.applyAbsent(cty.NilVal)
	if err := os.Symlink("/etc", l.path); err != nil {
		t.Fatal(err)
	}
	read := l.read(state)
	if got := read.GetAttr("in_sync"); !got.RawEquals(cty.False) {
		t.Fatalf("in_sync after the link came back = %#v, want false", got)
	}
	planned := l.plan(read, l.config(absent)).PlannedState
	if planned.GetAttr("in_sync").IsKnown() {
		t.Fatal("plan shows no change for a link that came back")
	}
	l.apply(read, planned, l.config(absent))
	if _, err := os.Lstat(l.path); !os.IsNotExist(err) {
		t.Fatal("link not removed again")
	}
}

// A file or a directory is the wrong kind of thing and is left in place.
func TestLinkAbsentRefusesOtherKinds(t *testing.T) {
	for name, create := range map[string]func(string) error{
		"file":      func(p string) error { return os.WriteFile(p, []byte("x"), 0o644) },
		"directory": func(p string) error { return os.Mkdir(p, 0o755) },
	} {
		t.Run(name, func(t *testing.T) {
			l := newLinkTest(t)
			if err := create(l.path); err != nil {
				t.Fatal(err)
			}
			resp := l.tryAbsent()
			if !resp.Diagnostics.HasErrors() || !strings.Contains(resp.Diagnostics.Err().Error(), "not a symlink") {
				t.Fatalf("apply over a %s: %v", name, resp.Diagnostics.Err())
			}
			if _, err := os.Lstat(l.path); err != nil {
				t.Fatalf("%s removed: %v", name, err)
			}
		})
	}
}

func TestLinkPresentToAbsent(t *testing.T) {
	l := newLinkTest(t)
	cfg := l.config(map[string]cty.Value{"target": cty.StringVal("/etc")})
	null := cty.NullVal(cfg.Type())
	state := l.apply(null, l.plan(null, cfg).PlannedState, cfg)

	cfg = l.config(absent)
	resp := l.plan(state, cfg)
	if len(resp.RequiresReplace) != 0 {
		t.Fatalf("RequiresReplace = %#v, want an update", resp.RequiresReplace)
	}
	state = l.apply(state, resp.PlannedState, cfg)
	if _, err := os.Lstat(l.path); !os.IsNotExist(err) {
		t.Fatal("link still exists")
	}

	// And back: present creates it again, with in_sync no longer tracked.
	cfg = l.config(map[string]cty.Value{"target": cty.StringVal("/tmp")})
	state = l.apply(state, l.plan(state, cfg).PlannedState, cfg)
	if got, err := os.Readlink(l.path); err != nil || got != "/tmp" {
		t.Fatalf("link = %q, %v; want /tmp", got, err)
	}
	if !state.GetAttr("in_sync").IsNull() {
		t.Fatalf("in_sync = %#v, want null", state.GetAttr("in_sync"))
	}
}

func TestLinkAbsentValidate(t *testing.T) {
	l := newLinkTest(t)
	validate := func(attrs map[string]cty.Value) error {
		return l.p.ValidateResourceConfig(ctx, providers.ValidateResourceConfigRequest{TypeName: "conn3x_link", Config: l.config(attrs)}).Diagnostics.Err()
	}
	if err := validate(absent); err != nil {
		t.Fatalf("state = absent alone: %v", err)
	}
	if err := validate(map[string]cty.Value{"state": cty.StringVal("absent"), "target": cty.StringVal("/etc")}); err == nil || !strings.Contains(err.Error(), "target") {
		t.Errorf("target with state = absent: err = %v", err)
	}
	if err := validate(map[string]cty.Value{"state": cty.StringVal("gone"), "target": cty.StringVal("/etc")}); err == nil {
		t.Error("state = \"gone\" accepted")
	}
	for name, attrs := range map[string]map[string]cty.Value{
		"no state":        nil,
		"state = present": {"state": cty.StringVal("present")},
	} {
		if err := validate(attrs); err == nil || !strings.Contains(err.Error(), "target is required") {
			t.Errorf("%s without target: err = %v", name, err)
		}
	}
	if err := validate(map[string]cty.Value{"state": cty.StringVal("present"), "target": cty.StringVal("/etc")}); err != nil {
		t.Errorf("state = present with target: %v", err)
	}
	l.path = "relative/link"
	if err := validate(absent); err == nil || !strings.Contains(err.Error(), "absolute") {
		t.Errorf("relative path with state = absent: err = %v", err)
	}
}

func TestLinkUnknownStatePlans(t *testing.T) {
	l := newLinkTest(t)
	cfg := l.config(map[string]cty.Value{"state": cty.UnknownVal(cty.String)})
	planned := l.plan(cty.NullVal(cfg.Type()), cfg).PlannedState
	if planned.GetAttr("in_sync").IsKnown() {
		t.Errorf("in_sync = %#v, want unknown", planned.GetAttr("in_sync"))
	}
}

func TestLinkUpgradesStateFromBeforeAbsent(t *testing.T) {
	old := `{"id":"h:/srv/app/current","path":"/srv/app/current","target":"releases/1","on_remove":"forget","ssh":null}`
	resp := upgradeWith(linkSchema)(providers.UpgradeResourceStateRequest{TypeName: "conn3x_link", RawStateJSON: []byte(old)})
	if resp.Diagnostics.HasErrors() {
		t.Fatal(resp.Diagnostics.Err())
	}
	if !resp.UpgradedState.GetAttr("state").IsNull() || !resp.UpgradedState.GetAttr("in_sync").IsNull() {
		t.Fatalf("upgraded state = %#v", resp.UpgradedState)
	}
}
