# Usage examples

Copy-pasteable, scenario-oriented commands. This directory holds documentation only — no pre-generated sample reports, because a report contains real measurements from a specific machine and should be produced on your own host.

Every example works as a one-shot run too: replace `ecs` with

```sh
curl -fsSL https://raw.githubusercontent.com/CST-Cat/ecs/main/run.sh | sh -s --
```

The options are identical. The full option list is `ecs run --help`; modules and exposure levels are in `ecs list`.

## First run

```sh
ecs                                   # standard profile: 19 of the 21 modules
ecs --interactive                     # wizard: profile, exposure level, module groups
ecs doctor                            # check the benchmark tools first
ecs list                              # what modules exist and how far each reaches out
```

The wizard reads from `/dev/tty`, so it works even under `curl … | sh`. Without a usable terminal (cron, containers, CI) it never blocks — the run proceeds with defaults.

## Trim by purpose

### Fully offline

```sh
ecs --exposure local
```

Not a single network packet. Local benchmarks and inventory only; modules above the ceiling are skipped and the report says so.

### Local performance only

```sh
ecs --only system,cpu,memory,disk,zstd,npb,crypto
```

Seven `local` modules: host inventory plus sysbench CPU, STREAM memory, fio disk, zstd compression, NPB EP/FT and OpenSSL cryptography.

### Network diagnostics only

```sh
ecs --only dns,latency,speed,ports,nat --exposure public
```

`--exposure public` guarantees the run stays networked without handing your egress IP to any commercial intelligence API.

### China route inspection

```sh
ecs --only cnspeed,backtrace,route --backtrace-city all
```

Carrier HTTP bandwidth, return-path detection and forward routing. Return paths default to Beijing and Guangzhou; `all` adds Shanghai and Chengdu and roughly doubles the runtime.

### Mail-server readiness

```sh
ecs --only blacklist,ports --strict
```

17 DNS blocklists, a reverse-DNS FCrDNS check, and whether outbound ports such as 25/465/587 are blocked. `--strict` turns any warning or failure into exit code 2, which suits a monitoring script.

### Streaming availability

```sh
ecs --only media                          # all regions
ecs --only media --media-region jp,hk,tw  # Japan, Hong Kong, Taiwan only
```

Verdicts rest on public-page evidence and **do not imply that an account can play, register or pay**.

### Multi-source IP quality

```sh
ecs --only network                                                   # all 13 sources
ecs --only network --ip-quality-sources ipinfo,scamalytics,abuseipdb  # just these three
```

This module is `thirdparty`: it submits your egress IP to commercial risk APIs. The `standard` profile excludes it; `full` includes it.

## Control time and resources

```sh
# Quick smoke test: shorter rounds, smaller disk footprint
ecs --cpu-time 3s --disk-mib 256 --iperf-duration 5s --only cpu,memory,disk,speed

# Thorough disk test: larger file, specific directory, extra mounts
ecs --only disk --disk-mib 8192 --disk-path /mnt/data --disk-multi

# Review burst credit and tail latency (fixed transfer per block; can exceed 20 minutes)
ecs --only disk --disk-matrix-mode fixed
```

`--disk-matrix-mode fixed` and the default `time` are **different measurement contracts and their numbers must not be compared**; `fixed` exists for review only. The disk module caps its temporary file at 20% of the free space measured beforehand and removes it on completion, error or cancellation.

Before starting, the terminal prints the estimated duration and disk footprint for this run. When `speed` is selected it states explicitly that iperf3 saturates the link for the configured duration and that **traffic is uncapped**.

## Protocol family

```sh
ecs -4                       # IPv4 only
ecs -6                       # IPv6 only
ecs --ip-version auto        # default: select by host and module protocol capabilities
```

Under `auto`, ecs selects available protocol families based on host capabilities and each module's own protocol capabilities; modules that support independent dual-stack measurements record IPv4/IPv6 separately. IPv6 stress probes are skipped when there is no real global IPv6 route.

## Custom targets

Every built-in endpoint list can be replaced wholesale, using `[name=]address` entries separated by commas:

```sh
ecs --only speed --iperf-targets "self=iperf.example.net:5201-5210" --speed-threads 16
ecs --only dns   --dns-resolvers "Cloudflare=1.1.1.1:53,AliDNS=223.5.5.5:53" --dns-attempts 12
ecs --only latency --latency-targets "api=api.example.com:443" --latency-attempts 20
ecs --only route --route-targets "Google=8.8.8.8,AliDNS=223.5.5.5"
ecs --only nat   --stun-servers "Xiaomi=stun.miwifi.com:3478"
ecs --only backtrace --backtrace-targets "Shanghai Telecom=202.96.209.133"
```

Ookla carrier server IDs must come from the official client — the catalogue changes often and ecs does not fetch it:

```sh
ecs --only ookla --ookla-servers "telecom=1234,unicom=5678,mobile=9012"
```

## Reports

```sh
ecs --output ./reports --name my-vps        # directory and filename prefix
ecs --format json                           # JSON only
ecs --format json,html                      # JSON and the web page
ecs --reveal                                # keep full local IPs (think before sharing)
ecs --color always                          # write ANSI colour into the txt file too
```

Output is `<prefix>.{json,txt,md,html}`, defaulting to `ecs-report-YYYYMMDD-HHMMSS`. JSON is the single source of truth, so other formats can be regenerated at any time without re-probing:

```sh
ecs render --input reports/ecs-report-20260813-075451.json --format html,md
ecs render --input report.json --output /tmp/out --name renamed
```

`render` loads reports according to the report schemas supported by the current binary; `compare` can compare reports in the `ecs.report/*` family. Across schemas, it compares only metrics present in both reports with identical signatures and marks the result as partially comparable. The report structure evolves additively (new fields are always optional), so optional fields the current implementation does not recognise are ignored without affecting the known ones.

## Compare runs

```sh
ecs compare yesterday.json today.json
ecs compare a.json b.json c.json --reference 2 --format json,txt,md,html --output ./compare
```

`--reference` picks which report is the baseline (1-based). Report paths may appear before or after the flags.

## Scoring and leaderboard references

A dimension score is measured ÷ reference mean × 1000. The reference is a replaceable data file, so **scores computed against different references are not comparable**.

```sh
# Aggregate a reference from your own batch of reports
ecs leaderboard reports/*.json --source "my fleet 2026-08" --output my-baseline.json

# Score against it
ecs --score-baseline my-baseline.json

# Rescore the same data without re-running anything
ecs render --input report.json --score-baseline my-baseline.json
```

`leaderboard` accepts full reports and slim submissions alike, and directories are walked recursively for `.json` files. The output lists the sample count behind every metric, the vCPU tiers, and any tier that fell back to the global mean for lack of samples — all of which you need in order to judge how much a score is worth.

## Submitting to the leaderboard

```sh
ecs submit --input report.json --provider vultr --region jp-tokyo --note "monthly run"
```

A submission is **a different artifact, not a compressed report**: a field whitelist carries host specs and benchmark values only, while egress IP, hostname, route paths and ASN are never included. Rules: [../submissions/README_EN.md](../submissions/README_EN.md).

Run the test and produce the submission in one shot:

```sh
curl -fsSL https://raw.githubusercontent.com/CST-Cat/ecs/main/run.sh | sh -s -- \
  --submit --profile full --yes --provider vultr --region jp-tokyo
```

## CI and automation

```sh
ecs --yes --strict --format json --output ./ci-reports --lang en
```

- `--yes` skips the wizard (it would not trigger without a terminal anyway, but being explicit is safer);
- `--strict` returns exit code 2 on warnings or failures; 0 is success and 130 means interrupted;
- Emit JSON only and generate human-readable formats later with `render`.

Combined with `--exposure local` this runs local benchmarks inside a fully isolated build environment with no outbound traffic at all.

## Configuration file

```sh
ecs config example > ecs.json
ecs --config ecs.json
ecs --config ecs.json --only cpu,memory     # command-line flags win
```

The generated sample:

```json
{
  "profile": "standard",
  "exposure": "thirdparty",
  "reveal": false,
  "ip_version": "auto",
  "ip_quality_sources": ["all"],
  "formats": ["json", "txt", "md", "html"],
  "output": "./reports",
  "disk_path": ".",
  "iperf_duration": "5s",
  "http_timeout": "10s"
}
```

The file also accepts `only` / `skip` arrays and the endpoint lists `dns_resolvers`, `latency_targets`, `route_targets`, `backtrace_targets`, `stun_servers`, `iperf_targets`, `ookla_servers` and `media_regions`. **Unknown fields are rejected** — a mistyped key is never ignored silently.

## Environment variables

For the one-shot runner `run.sh`:

| Variable | Effect |
| --- | --- |
| `ECS_REPOSITORY` | Release repository, defaults to `CST-Cat/ecs` |
| `ECS_VERSION` | Release tag, defaults to `latest` |
| `ECS_AUTO_DEPS=0` | Disable automatic tool staging and let ecs report what is missing |
| `ECS_KEEP=1` | Keep the temporary work directory for troubleshooting |
| `ECS_LANG` | Language of the script's own messages |
| `TMPDIR` | Work directory and default report location; must be an absolute path |

For `install.sh`: `ECS_REPOSITORY`, `ECS_RELEASE_BASE`, `ECS_INSTALL_DIR`, `ECS_VERSION`.

---

中文：[README.md](README.md) · Back to the [project overview](../README_EN.md)
