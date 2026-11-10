#!/usr/bin/env bash
# Copyright (c) The Conn3x Authors
# SPDX-License-Identifier: MPL-2.0
#
# Fail if a conn3x binary still names OpenTofu while it runs, or prints
# control characters from a configuration or a host as they are.
#
#   scripts/brand-check.sh <path-to-conn3x>
set -euo pipefail

bin=$(cd "$(dirname "$1")" && pwd)/$(basename "$1")
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
cd "$work"
printf 'resource "terraform_data" "x" {\n  input = "hi"\n}\n' > main.tf

out=$( { "$bin" -help; "$bin" version; "$bin" init -no-color; "$bin" plan -no-color; "$bin" nope || true; } 2>&1 )
leaks=$(grep -n 'OpenTofu' <<<"$out" | grep -v 'built on OpenTofu' || true)
if [ -n "$leaks" ]; then
  echo "conn3x output still names OpenTofu:" >&2
  echo "$leaks" >&2
  exit 1
fi
grep -q 'Conn3x will perform the following actions' <<<"$out" || { echo "plan output is not branded" >&2; exit 1; }

# The build also patches how plan output and errors print text that came
# from a host or a circuit: control characters are written out as escapes,
# with color and without. \u001b is HCL for the escape character.
cat > main.tf <<'EOF'
resource "terraform_data" "x" {
  input = "one\n\u001b[2Jtwo\n"
}
check "c" {
  assert {
    condition     = terraform_data.x.input == ""
    error_message = "warned \u001b[2J."
  }
}
variable "v" {
  default = "x"
  validation {
    condition     = var.v == "y"
    error_message = "failed \u001b[2J."
  }
}
EOF
esc=$(printf '\033')
shown() { # shown <want> <plan flags...>: plan prints want, and no raw escape sequence from the file
  local want=$1 out
  shift
  out=$("$bin" plan "$@" 2>&1 || true)
  if grep -q "${esc}\[2J" <<<"$out"; then
    echo "plan $* printed a control character from the configuration as it is" >&2
    exit 1
  fi
  grep -qF -- "$want" <<<"$out" || { echo "plan $* didn't print $want" >&2; exit 1; }
}
shown 'failed \x1b[2J.' -no-color
shown 'failed \x1b[2J.'
shown 'warned \x1b[2J.' -no-color -var v=y
shown 'warned \x1b[2J.' -var v=y
shown '\x1b[2Jtwo' -no-color -var v=y

echo "brand check passed"
