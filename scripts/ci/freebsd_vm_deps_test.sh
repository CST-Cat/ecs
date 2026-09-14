#!/usr/bin/env bash
set -euo pipefail

lock=${1:-tools/freebsd-vm-deps.lock}
test -s "$lock"

declare -A count
declare -A seen
while IFS='|' read -r target name version url sha; do
  case "$target" in
    ''|'#'*) continue ;;
  esac
  [[ "$target" == amd64 || "$target" == aarch64 ]]
  [[ "$name" =~ ^(indexinfo|gettext-runtime|oniguruma|bash|jq|file|ca_root_nss)$ ]]
  [[ -n "$version" && "$url" == "https://pkg.freebsd.org/FreeBSD:15:$target/quarterly/All/Hashed/$name-$version~"*.pkg ]]
  [[ "$url" != *latest* && "$url" != *stable* && "$url" != *current* ]]
  [[ "$sha" =~ ^[0-9a-f]{64}$ ]]
  key="$target/$name"
  [[ -z "${seen[$key]:-}" ]] || { echo "duplicate lock row: $key" >&2; exit 1; }
  seen[$key]=1
  count[$target]=$(( ${count[$target]:-0} + 1 ))
done < "$lock"

[[ "${count[amd64]:-0}" -eq 7 ]]
[[ "${count[aarch64]:-0}" -eq 7 ]]
[[ "${#seen[@]}" -eq 14 ]]
echo "freebsd VM dependency lock: 14 exact package rows validated"
