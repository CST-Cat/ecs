# Usage examples

Copy-pasteable scenario commands. This directory contains documentation only, not pre-generated reports; create reports on the target machine. Shared boundaries, privacy and the complete option list live in [../README_EN.md](../README_EN.md), [../SECURITY.md](../SECURITY.md) and `ecs run --help`.

Every example can be run without installing first:

```sh
curl -fsSL https://raw.githubusercontent.com/CST-Cat/ecs/main/run.sh | sh -s --
```

## First run

```sh
ecs
ecs --interactive
ecs doctor
ecs list
```

The wizard does not block without a usable terminal; automation can use `--yes` explicitly.

## Select by purpose

Fully offline:

```sh
ecs --exposure local
```

Local performance:

```sh
ecs --only system,cpu,memory,disk,zstd,npb,crypto
```

Network diagnostics:

```sh
ecs --only dns,latency,speed,ports,nat --exposure public
```

China routing:

```sh
ecs --only cnspeed,backtrace,route --backtrace-city all
```

Mail-server checks:

```sh
ecs --only blacklist,ports --strict
```

Media reachability:

```sh
ecs --only media
ecs --only media --media-region jp,hk,tw
```

Multi-source IP quality (sends the egress IP to commercial intelligence services):

```sh
ecs --only network
ecs --only network --ip-quality-sources ipinfo,scamalytics,abuseipdb
```

## Control duration and resources

```sh
# Quick smoke run
ecs --cpu-time 3s --disk-mib 256 --iperf-duration 5s --only cpu,memory,disk,speed

# Choose the disk directory and size
ecs --only disk --disk-mib 8192 --disk-path /mnt/data --disk-multi

# Fixed-transfer matrix (not comparable with the default time mode)
ecs --only disk --disk-matrix-mode fixed
```

The run prints an estimated duration and disk footprint first. `speed` sends traffic for the configured duration without a traffic cap. Temporary disk files are removed on completion, error or cancellation.

## IP versions and custom targets

```sh
ecs -4
ecs -6
ecs --ip-version auto
```

`auto` chooses a protocol family from host and module capabilities; dual-stack modules record IPv4 and IPv6 separately.

Endpoint lists use `[name=]address`, separated by commas:

```sh
ecs --only speed --iperf-targets "custom=iperf.example.net:5201-5210" --speed-threads 16
ecs --only dns --dns-resolvers "Cloudflare=1.1.1.1:53,AliDNS=223.5.5.5:53" --dns-attempts 12
ecs --only latency --latency-targets "private=api.example.com:443" --latency-attempts 20
ecs --only route --route-targets "Google=8.8.8.8,AliDNS=223.5.5.5"
ecs --only nat --stun-servers "Xiaomi=stun.miwifi.com:3478"
ecs --only backtrace --backtrace-targets "Shanghai Telecom=202.96.209.133"
ecs --only ookla --ookla-servers "Telecom=1234,Unicom=5678,Mobile=9012"
```

Get Ookla server IDs from the official client; its catalogue changes and ecs does not fetch it. See [../THIRD_PARTY.md](../THIRD_PARTY.md) for external-service boundaries.

## Reports and comparison

```sh
ecs --output ./reports --name my-vps
ecs --format json
ecs --format json,html
ecs --reveal
ecs --color always
```

Outputs are `<prefix>.{json,md,html}`. JSON is canonical and can be rendered later:

```sh
ecs render --input reports/ecs-report-20260813-075451.json --format html,md
ecs --lang en render --input report.json --output /tmp/out --name renamed
ecs compare yesterday.json today.json
ecs compare a.json b.json c.json --reference 2 --format json,md,html --output ./compare
curl -fsSL https://raw.githubusercontent.com/CST-Cat/ecs/main/compare.sh | sh -s -- yesterday.json today.json
```

`compare` accepts only JSON reports that pass the current `ecs.report/v1` exact loader; reports with a different schema version are rejected, and it never probes again. See [../docs/schema.md](../docs/schema.md) for fields.

## Scoring and submissions

```sh
ecs leaderboard reports/*.json --source "my fleet 2026-08" --output my-baseline.json
ecs --score-baseline my-baseline.json
ecs render --input report.json --score-baseline my-baseline.json
ecs submit --input report.json --provider vultr --region jp-tokyo --note "monthly run"
```

Run and submit in one step:

```sh
curl -fsSL https://raw.githubusercontent.com/CST-Cat/ecs/main/run.sh | sh -s -- \
  --submit --provider vultr --region jp-tokyo -- \
  --profile full --yes
```

The second exact `--` separates wrapper options from `ecs run` options; every later argument is passed to `ecs` unchanged.

The submission whitelist, directory rules and local validation are in [../submissions/README_EN.md](../submissions/README_EN.md); privacy and leaderboard policy are not duplicated here.

## Automation and configuration

```sh
ecs --yes --strict --format json --output ./ci-reports --lang en
ecs --yes --strict --exposure local --format json --output ./reports
```

`--strict` returns 2 for warnings or failures, 0 for success and 130 when interrupted; use `render` afterwards for human-readable formats.

```sh
ecs config example > ecs.json
ecs --config ecs.json
ecs --config ecs.json --only cpu,memory
```

Command-line options override the configuration, and unknown fields are rejected. The file can contain `only`, `skip`, durations and attempt counts, plus lists such as `dns_resolvers`, `latency_targets`, `route_targets`, `backtrace_targets`, `stun_servers`, `iperf_targets`, `ookla_servers` and `media_regions`.

Common one-shot script variables:

| Variable | Purpose |
| --- | --- |
| `ECS_REPOSITORY` | Release repository override (default: `CST-Cat/ecs`) |
| `ECS_VERSION` | Release tag |
| `ECS_AUTO_DEPS=0` | Disable temporary tool staging |
| `ECS_KEEP=1` | Keep the temporary work directory for debugging |
| `ECS_LANG` | Script prompt language |
| `TMPDIR` | Temporary directory and default report location |

The installer additionally accepts `ECS_RELEASE_BASE`, `ECS_INSTALL_DIR` and `ECS_VERSION`.

---

中文：[README.md](README.md) · Back to the [project overview](../README_EN.md)
