#!/bin/sh
set -eu

if [ "$(id -u)" -eq 0 ]; then
  echo "freebsd-runtime: integration must run as an ordinary user" >&2
  exit 1
fi
if [ "$(uname -s)" != FreeBSD ]; then
  echo "freebsd-runtime: expected FreeBSD" >&2
  exit 1
fi

repo_root=$(CDPATH= cd -- "$(dirname "$0")/../.." && pwd)
cd "$repo_root"

freebsd-version
uname -a
go version
go test ./...
go test -tags=integration ./internal/probe -timeout 20m -count=1
