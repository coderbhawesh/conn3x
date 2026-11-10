// Copyright (c) The Conn3x Authors
// SPDX-License-Identifier: MPL-2.0

package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/opentofu/opentofu/internal/command"
	"github.com/opentofu/opentofu/internal/command/arguments"
	runreport "github.com/opentofu/opentofu/internal/conn3x/report"
)

const reportFlagHelp = "Write a JSON report of the run to this file: each host's steps and what happened to them, and the errors as printed. The format: https://docs.conn3x.org/reference/cli/apply/#-report"

// withReport gives apply a -report flag. The apply runs as before, with its
// -json stream also sent to a temporary file through -json-into (or to the
// caller's own -json-into file), and the report is built from that stream.
func withReport(cmd *command.Command) {
	var path string
	cmd.CommandLine.StringVar(&path, "report", "", reportFlagHelp).SetDisplay("=run.json")
	view := cmd.CommandLine.View
	run := cmd.Run
	cmd.Run = func(m command.Meta) int {
		if path == "" {
			return run(m)
		}
		if view.ViewType == arguments.ViewJSON {
			fmt.Fprintln(stderr, "Error: -report doesn't go with -json, whose stream on standard output already has everything; use -json-into to keep the human output")
			return 1
		}
		var stream string
		if view.JSONInto != nil {
			stream = view.JSONInto.Name()
		} else {
			tmp, err := os.CreateTemp("", "conn3x-report-*.json")
			if err != nil {
				fmt.Fprintf(stderr, "Error: making a file for the report's stream: %s\n", err)
				return 1
			}
			defer os.Remove(tmp.Name())
			defer tmp.Close()
			view.JSONInto, stream = tmp, tmp.Name()
		}
		started := time.Now()
		code := run(m)
		err := writeApplyReport(path, stream, started, code == 0)
		var long runreport.LongLines
		switch {
		case errors.As(err, &long):
			// The report is written and says what the apply did; only
			// what those lines carried, such as a very large output or
			// error, is missing from it.
			fmt.Fprintf(stderr, "Warning: the report at %s is incomplete: %s\n", path, err)
		case err != nil && code == 0:
			fmt.Fprintf(stderr, "Error: the apply succeeded, but its report wasn't written: %s\n", err)
			code = 1
		case err != nil:
			fmt.Fprintf(stderr, "Error: writing the report: %s\n", err)
		}
		return code
	}
}

func writeApplyReport(path, stream string, started time.Time, success bool) error {
	in, err := os.Open(stream)
	if err != nil {
		return err
	}
	defer in.Close()
	r := runreport.New("apply", started)
	streamErr := r.FromApply(in, success)
	if _, long := streamErr.(runreport.LongLines); streamErr != nil && !long {
		return streamErr
	}
	if err := writeReport(path, r); err != nil {
		return err
	}
	return streamErr
}

// writeReport writes r to path, readable by its owner only: its errors can
// carry what a failed command printed, and reports get copied about. It
// writes a new file beside path and renames it into place, so a file
// already there doesn't keep a wider mode and a link there is replaced, not
// followed. Something that isn't a file, such as /dev/stdout, is written to
// as it is.
func writeReport(path string, r *runreport.Report) error {
	if st, err := os.Lstat(path); err == nil && !st.Mode().IsRegular() && st.Mode()&os.ModeSymlink == 0 {
		out, err := os.OpenFile(path, os.O_WRONLY, 0)
		if err != nil {
			return err
		}
		if err := r.Write(out, time.Now()); err != nil {
			out.Close()
			return err
		}
		return out.Close()
	}
	out, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*") // mode 0600
	if err != nil {
		return err
	}
	defer os.Remove(out.Name()) // a no-op after the rename
	if err := r.Write(out, time.Now()); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Rename(out.Name(), path)
}

// withoutReport takes -report and its value out of a command line, for the
// plan drift runs, which doesn't take it.
func withoutReport(args []string) []string {
	out := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-report" || a == "--report":
			i++ // and its value
		case strings.HasPrefix(a, "-report=") || strings.HasPrefix(a, "--report="):
		default:
			out = append(out, a)
		}
	}
	return out
}

// driftToReport fills a drift report from what drift found.
func driftToReport(r driftReport, code int, started time.Time) *runreport.Report {
	rep := runreport.New("drift", started)
	rep.Success = code != 1
	for _, line := range append(append([]string{}, r.hosts...), r.cloud...) {
		gone := strings.HasSuffix(line, "  gone")
		addr := strings.TrimSpace(line)
		addr = strings.TrimPrefix(strings.TrimPrefix(addr, "~ "), "- ")
		addr = strings.TrimSuffix(strings.TrimSuffix(addr, "  changed"), "  gone")
		rep.AddDrift(addr, gone)
	}
	rep.Errors = append(rep.Errors, r.errors...)
	return rep
}
