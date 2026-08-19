# ecs

An ad-free, local-first Linux VPS benchmark. One run covers local performance, network quality, routing, media reachability and IP reputation, and writes JSON, txt, Markdown and HTML reports locally.

Project boundaries:

- no ads, sponsorships or affiliate links;
- reports are not uploaded; `run.sh` defaults to `${TMPDIR:-/tmp}`, while a directly invoked binary defaults to `./reports`;
- JSON is the canonical machine-data source; the other formats are rendered from it, and `--lang` changes only terminal and human-readable txt/Markdown/HTML output;
- local performance uses standard benchmark tools. Missing or contract-mismatched tools are reported as not run; no substitute score is synthesized;
- `ookla` invokes the official client and follows its independent data-handling terms; see [THIRD_PARTY.md](THIRD_PARTY.md).

## Quick start

Run without installing anything:

```sh
curl -fsSL https://raw.githubusercontent.com/CST-Cat/ecs/main/run.sh | sh
curl -fsSL https://raw.githubusercontent.com/CST-Cat/ecs/main/run.sh | sh -s -- --profile full --lang en
```

`run.sh` verifies downloaded files and stages only the fixed tools needed for this run in a temporary PATH. It does not write system directories and cleans up on exit. Use `--output PATH` to choose the report location.

Compare existing reports by downloading only the main program:

```sh
curl -fsSL https://raw.githubusercontent.com/CST-Cat/ecs/main/compare.sh | sh -s -- yesterday.json today.json
```

Install a release binary:

```sh
ECS_REPOSITORY=CST-Cat/ecs ./install.sh
ECS_REPOSITORY=CST-Cat/ecs ./install.sh --with-benchmarks
./install.sh --from ./bin/ecs
```

The default installation installs only `ecs`; `--with-benchmarks` is the explicit option that uses the system package manager for `sysbench`, `fio` and `iperf3`.

## Commands

| Command | Purpose |
| --- | --- |
| `ecs [run]` | Run the selected profile, defaulting to `standard` |
| `ecs list` | List profiles, modules and exposure levels |
| `ecs doctor` | Check external benchmark tools |
| `ecs render --input FILE` | Re-export a report from JSON without probing again |
| `ecs compare REPORT...` | Compare two or more reports |
| `ecs config example` | Print a JSON configuration example |
| `ecs leaderboard REPORT...` | Aggregate a leaderboard reference |
| `ecs submit --input FILE` | Export a slim JSON suitable for public submission |
| `ecs version` | Show the version |

All commands accept `--lang zh|en`. Exit codes are `0` (success), `1` (argument or run error), `2` (warnings or failed probes with `--strict`) and `130` (interrupted).

## Reports, rendering and comparison

A run can write four formats:

```text
ecs-report-YYYYMMDD-HHMMSS.json    # ecs.report/v1, canonical machine data
ecs-report-YYYYMMDD-HHMMSS.txt
ecs-report-YYYYMMDD-HHMMSS.md
ecs-report-YYYYMMDD-HHMMSS.html
```

```sh
ecs --format json,html --output ./reports --name my-run
ecs render --input ./reports/my-run.json --format html,md --lang en
ecs compare yesterday.json today.json --format txt,html
ecs compare a.json b.json c.json --reference 2
```

JSON preserves canonical fields, tables and raw evidence. Rendering in Chinese or English does not change machine data, and the same JSON can be rendered again in another language. Across report schemas, `compare` uses only metrics with matching signatures and marks the result as partially comparable. See [docs/schema.md](docs/schema.md) for field definitions.

## Profiles and modules

| Profile | Contents |
| --- | --- |
| `standard` (default) | Local and public modules, excluding multi-source IP quality and Ookla |
| `full` | All modules, including `network` and `ookla` |

`--only` explicitly selects modules and `--skip` excludes them. A profile module above the `--exposure` ceiling is filtered; an explicitly named module above the ceiling is rejected.

| Module | Main coverage | Exposure |
| --- | --- | --- |
| `system` | System, virtualization, resources and kernel networking | local |
| `cpu` / `zstd` / `npb` | CPU, compression and EP/FT benchmarks | local |
| `memory` / `crypto` / `disk` | STREAM, OpenSSL and fio benchmarks | local |
| `dns` / `latency` / `speed` | DNS, TCP latency and iperf3 throughput | public |
| `ports` / `nat` / `route` / `backtrace` | Egress ports, STUN, forward and return routes | public |
| `bgp` / `apps` / `cnspeed` / `media` | Interconnection, service reachability, China speed and media | public |
| `blacklist` | IP reputation and reverse DNS | public |
| `network` / `ookla` | Multi-source IP quality and official Speedtest | thirdparty |

More runnable combinations are in [examples/README_EN.md](examples/README_EN.md).

## Exposure and privacy

`--exposure` caps what external services may see:

| Level | Behavior |
| --- | --- |
| `local` | No network traffic; local collection and benchmarks only |
| `public` | Public infrastructure only; services can see the egress IP |
| `thirdparty` (default) | Registered third-party intelligence services are allowed |
| `any` | All registered external services are allowed |

```sh
ecs --exposure local
ecs --exposure public
ecs --only cnspeed,backtrace,route --backtrace-city all
```

Reports redact this machine's IP addresses by default. Hostnames, remote IPs, hop-by-hop routes and BGP prefixes are not automatically redacted. `--reveal` keeps the full local IP; review a report before sharing it. See [SECURITY.md](SECURITY.md) and [THIRD_PARTY.md](THIRD_PARTY.md) for execution and data boundaries.

## Scoring and submissions

The score dimensions are `cpu`, `memory`, `disk` and `bandwidth`. A dimension score is measured value ÷ reference mean × 1000; missing dimensions are not counted as zero. The reference is replaceable local JSON:

```sh
ecs leaderboard reports/*.json --output my-baseline.json
ecs --score-baseline my-baseline.json
ecs render --input report.json --score-baseline my-baseline.json
ecs submit --input report.json --provider vultr --region jp-tokyo --note "monthly run"
```

When the embedded release reference is empty, provide your own `--score-baseline`. See [submissions/README_EN.md](submissions/README_EN.md) for the submission format and directory rules.

## Common options and configuration

```text
--profile standard|full          profile
--only / --skip MODULE,...       select or skip modules
--exposure local|public|thirdparty|any
--lang zh|en                     interface language
--format json,txt,md,html        output formats
--output DIR / --name PREFIX     report location and name
--reveal                         keep full local IP addresses
--strict                         return 2 on warnings or failures
--interactive / --yes            enable or skip the wizard
--score-baseline FILE            scoring reference
```

Use `ecs run --help` for the complete option list. Generate a configuration file as follows; unknown fields are rejected and command-line options take precedence:

```sh
ecs config example > ecs.json
ecs --config ecs.json
```

## Tools and platform boundary

Local performance calls `sysbench`, pinned `zstd`, NPB-OMP, OpenSSL, official STREAM, `fio` and `iperf3`; routing uses NextTrace Tiny and Ookla uses its official client. `ecs doctor` reports missing tools. A version check failure means the benchmark did not run; no in-house benchmark replaces it.

The project supports Linux only and publishes `amd64`, `arm64`, `armv7`, `386`, `s390x`, `riscv64` and `ppc64le` binaries. Native probes do not require root.

Source development and maintenance are described in [CONTRIBUTING.md](CONTRIBUTING.md). See [CHANGELOG.md](CHANGELOG.md) for changes and [NOTICE](NOTICE)/[LICENSE](LICENSE) for attribution and licensing.

## Other documentation

- [README.md](README.md) — 中文说明
- [docs/schema.md](docs/schema.md) — report JSON schema
- [examples/README_EN.md](examples/README_EN.md) — scenario commands
- [submissions/README_EN.md](submissions/README_EN.md) — leaderboard submission format
- [SECURITY.md](SECURITY.md) — security and privacy boundaries
- [THIRD_PARTY.md](THIRD_PARTY.md) — external tools and services
