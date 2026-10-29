// Copyright (c) The Conn3x Authors
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"os"
	"strings"
	"testing"

	"github.com/zclconf/go-cty/cty"

	"github.com/opentofu/opentofu/internal/providers"
)

var absent = map[string]cty.Value{"state": cty.StringVal("absent")}

// applyAbsent plans and applies state = "absent" over prior.
func (f *fileTest) applyAbsent(prior cty.Value) cty.Value {
	f.t.Helper()
	cfg := f.config(absent)
	if prior.IsNull() {
		prior = cty.NullVal(cfg.Type())
	}
	return f.apply(prior, f.plan(prior, cfg).PlannedState, cfg)
}

func TestFileAbsentRemovesFile(t *testing.T) {
	f := newFileTest(t)
	_ = os.WriteFile(f.path, []byte("left behind"), 0o644)
	state := f.applyAbsent(cty.NilVal)
	if _, err := os.Stat(f.path); !os.IsNotExist(err) {
		t.Fatal("file still exists with state = absent")
	}
	if got := state.GetAttr("in_sync"); !got.RawEquals(cty.True) {
		t.Fatalf("in_sync = %#v, want true", got)
	}
	for _, attr := range []string{"content", "sha256", "mode", "content_hmac"} {
		if !state.GetAttr(attr).IsNull() {
			t.Errorf("%s = %#v, want null", attr, state.GetAttr(attr))
		}
	}

	// Once gone, the next plan changes nothing.
	read := f.read(state)
	if planned := f.plan(read, f.config(absent)).PlannedState; !planned.RawEquals(read) {
		t.Fatalf("second plan changes something:\nplanned %#v\nprior   %#v", planned, read)
	}
}

func TestFileAbsentMissingPathPlansClean(t *testing.T) {
	f := newFileTest(t)
	state := f.applyAbsent(cty.NilVal)
	if planned := f.plan(f.read(state), f.config(absent)).PlannedState; !planned.GetAttr("in_sync").RawEquals(cty.True) {
		t.Fatalf("in_sync = %#v, want true", planned.GetAttr("in_sync"))
	}
}

func TestFileAbsentKeepsItGone(t *testing.T) {
	f := newFileTest(t)
	state := f.applyAbsent(cty.NilVal)
	_ = os.WriteFile(f.path, []byte("back again"), 0o644)
	read := f.read(state)
	if got := read.GetAttr("in_sync"); !got.RawEquals(cty.False) {
		t.Fatalf("in_sync after the file came back = %#v, want false", got)
	}
	planned := f.plan(read, f.config(absent)).PlannedState
	if planned.GetAttr("in_sync").IsKnown() {
		t.Fatal("plan shows no change for a file that came back")
	}
	f.apply(read, planned, f.config(absent))
	if _, err := os.Stat(f.path); !os.IsNotExist(err) {
		t.Fatal("file not removed again")
	}
}

func TestFilePresentToAbsent(t *testing.T) {
	f := newFileTest(t)
	state := f.create("x", nil)
	cfg := f.config(absent)
	resp := f.plan(state, cfg)
	if len(resp.RequiresReplace) != 0 {
		t.Fatalf("RequiresReplace = %#v, want an update", resp.RequiresReplace)
	}
	state = f.apply(state, resp.PlannedState, cfg)
	if _, err := os.Stat(f.path); !os.IsNotExist(err) {
		t.Fatal("file still exists")
	}

	// And back: present writes it again, with in_sync no longer tracked.
	cfg = f.config(map[string]cty.Value{"content": cty.StringVal("y")})
	state = f.apply(state, f.plan(state, cfg).PlannedState, cfg)
	if got, _ := os.ReadFile(f.path); string(got) != "y" {
		t.Fatalf("content = %q", got)
	}
	if !state.GetAttr("in_sync").IsNull() || state.GetAttr("mode").AsString() != "0644" {
		t.Fatalf("in_sync = %#v, mode = %#v", state.GetAttr("in_sync"), state.GetAttr("mode"))
	}
}

func TestFileAbsentRefusesDirectory(t *testing.T) {
	f := newFileTest(t)
	if err := os.Mkdir(f.path, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := f.config(absent)
	null := cty.NullVal(cfg.Type())
	resp := f.p.ApplyResourceChange(ctx, providers.ApplyResourceChangeRequest{
		TypeName: "conn3x_file", PriorState: null, PlannedState: f.plan(null, cfg).PlannedState, Config: cfg,
	})
	if !resp.Diagnostics.HasErrors() || !strings.Contains(resp.Diagnostics.Err().Error(), "not a regular file") {
		t.Fatalf("apply over a directory: %v", resp.Diagnostics.Err())
	}
	if _, err := os.Stat(f.path); err != nil {
		t.Fatalf("directory removed: %v", err)
	}
}

func TestFileAbsentValidate(t *testing.T) {
	f := newFileTest(t)
	validate := func(attrs map[string]cty.Value) error {
		return f.p.ValidateResourceConfig(ctx, providers.ValidateResourceConfigRequest{TypeName: "conn3x_file", Config: f.config(attrs)}).Diagnostics.Err()
	}
	if err := validate(absent); err != nil {
		t.Fatalf("state = absent alone: %v", err)
	}
	for _, attr := range []string{"content", "content_wo", "mode", "owner", "group"} {
		attrs := map[string]cty.Value{"state": cty.StringVal("absent"), attr: cty.StringVal("0644")}
		if err := validate(attrs); err == nil || !strings.Contains(err.Error(), attr) {
			t.Errorf("%s with state = absent: err = %v", attr, err)
		}
	}
	if err := validate(map[string]cty.Value{"state": cty.StringVal("gone"), "content": cty.StringVal("x")}); err == nil {
		t.Error("state = \"gone\" accepted")
	}
	if err := validate(map[string]cty.Value{"state": cty.StringVal("present")}); err == nil {
		t.Error("state = present without content accepted")
	}
}

func TestFileUnknownStatePlans(t *testing.T) {
	f := newFileTest(t)
	cfg := f.config(map[string]cty.Value{"state": cty.UnknownVal(cty.String)})
	planned := f.plan(cty.NullVal(cfg.Type()), cfg).PlannedState
	for _, attr := range []string{"mode", "sha256", "in_sync"} {
		if planned.GetAttr(attr).IsKnown() {
			t.Errorf("%s = %#v, want unknown", attr, planned.GetAttr(attr))
		}
	}
}

func TestFileUpgradesStateFromBeforeAbsent(t *testing.T) {
	old := `{"id":"h:/etc/motd","path":"/etc/motd","content":"hi","mode":"0644","owner":null,"group":null,"on_remove":"forget","sha256":"x","content_hmac":null,"ssh":null}`
	resp := upgradeFile(providers.UpgradeResourceStateRequest{TypeName: "conn3x_file", RawStateJSON: []byte(old)})
	if resp.Diagnostics.HasErrors() {
		t.Fatal(resp.Diagnostics.Err())
	}
	if !resp.UpgradedState.GetAttr("state").IsNull() || !resp.UpgradedState.GetAttr("in_sync").IsNull() {
		t.Fatalf("upgraded state = %#v", resp.UpgradedState)
	}
}
