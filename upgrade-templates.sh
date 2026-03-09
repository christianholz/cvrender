#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
Convert one legacy XML template from pycv:* tags to cv:* tags.

Usage:
  ./upgrade-templates.sh <input.xml>
  ./upgrade-templates.sh -   # read from stdin

Examples:
  ./upgrade-templates.sh cv.xml > cv-new.xml
  cat cv.xml | ./upgrade-templates.sh - > cv-new.xml
EOF
}

if (($# == 1)) && [[ "$1" == "-h" || "$1" == "--help" ]]; then
  usage
  exit 0
fi

if (($# != 1)); then
  echo "error: expected exactly one input file path (or '-' for stdin)" >&2
  usage >&2
  exit 2
fi

in="$1"

if [[ "$in" == "-" ]]; then
  perl -0777 -pe '
    s#<\s*pycv:#<cv:#g;
    s#</\s*pycv:#</cv:#g;
    s#xmlns:pycv#xmlns:cv#g;
  '
else
  if [[ ! -f "$in" ]]; then
    echo "error: input file not found: $in" >&2
    exit 2
  fi
  perl -0777 -pe '
    s#<\s*pycv:#<cv:#g;
    s#</\s*pycv:#</cv:#g;
    s#xmlns:pycv#xmlns:cv#g;
  ' "$in"
fi
