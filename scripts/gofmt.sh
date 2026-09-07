#!/usr/bin/env bash
set -euo pipefail

goroot="$(GOTOOLCHAIN=go1.27.1 go env GOROOT)"
exec "$goroot/bin/gofmt" "$@"
