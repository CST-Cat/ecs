# ecs

An ad-free, local-first Linux VPS benchmark. One run covers local performance, network quality, routing, media reachability and IP reputation, and writes JSON, Markdown and HTML reports locally.

Project boundaries:

- no ads, sponsorships or affiliate links;
- reports are not uploaded; `run.sh` defaults to `${TMPDIR:-/tmp}`, while a directly invoked binary defaults to `./reports`;
- JSON is the machine truth; human-readable output includes terminal text, Markdown and HTML, all rendered from JSON; `--lang` changes only these human-readable outputs;
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

`compare.sh` is the only shell compare entry. When run through a pipe, the script is not saved to disk; it downloads only a temporary `ecs`, removes the temporary file after comparison, and preserves the result.

Install a release binary:

```sh
./install.sh
./install.sh --with-benchmarks
# Override the default repository for a mirror or fork
ECS_REPOSITORY=owner/ecs ./install.sh
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
| `ecs plan` | Print the resolved machine execution plan as JSON without probing |
| `ecs version` | Show the version |
| `ecs help` | Show the command overview |

For non-run commands, put the interface language in the global prefix, for example `ecs --lang en compare ...`. `run` and `plan` also accept `--lang zh|en` in their own options. Exit codes are `0` (success), `1` (argument or run error), `2` (warnings or failed probes with `--strict`) and `130` (interrupted).

### Full option reference

Every option each command accepts. Defaults are the built-in defaults; a `--config` file overrides them, and explicit command-line options override the file.

**Global prefix** (before the subcommand)

| Option | Default | Description |
| --- | --- | --- |
| `--lang zh\|en` | environment, falling back to `zh` | Interface language; affects human-readable output only |

**`ecs [run]` / `ecs plan`** (same option set; `plan` does not accept `--version`)

Selection and scope:

| Option | Default | Description |
| --- | --- | --- |
| `--profile standard\|full` | `standard` | Profile |
| `--config FILE` | none | JSON configuration file; unknown fields are rejected |
| `--only MODULE,...` | none | Run only these modules |
| `--skip MODULE,...` | none | Skip these modules |
| `--exposure local\|public\|thirdparty\|any` | `thirdparty` | Maximum outbound exposure |
| `--ip-version auto\|4\|6` | `auto` | IP family; network probes record families separately |
| `-4` / `-6` | off | Shortcuts for `--ip-version 4` / `6`; mutually exclusive |
| `--ip-quality-sources all\|none\|NAME,...` | `all` | IP quality sources; names come from `ecs list` |

Output and presentation:

| Option | Default | Description |
| --- | --- | --- |
| `--format json,md,html` | `json,md,html` | Output formats, comma-separated |
| `--output DIR` | `./reports` (temporary directory under `run.sh`) | Report output directory |
| `--name PREFIX` | timestamped | Report file name prefix |
| `--lang zh\|en` | same as global | Interface language |
| `--color auto\|none\|basic\|256\|truecolor\|always` | `auto` | Terminal color mode |
| `--no-color` | off | Disable ANSI colors |
| `--reveal` | off | Keep full local IP addresses in local reports |
| `--score-baseline FILE` | embedded reference | Scoring leaderboard reference file |

Local benchmark intensity:

| Option | Default | Range | Description |
| --- | --- | --- | --- |
| `--cpu-time DURATION` | `15s` | 100ms–30s | Duration of each CPU/memory run |
| `--disk-mib N` | `2048` | 16–16384 | Disk temp file size in MiB |
| `--disk-path DIR` | `.` | — | Disk test directory |
| `--disk-multi` | off | — | Also test mount points beyond the system disk |
| `--disk-matrix-mode time\|fixed` | `time` | — | Disk matrix measurement mode; `fixed` is review-only and can exceed 20 minutes |

Network probe intensity and targets:

| Option | Default | Range | Description |
| --- | --- | --- | --- |
| `--timeout DURATION` | `10s` | 1s–60s | Single HTTP request timeout |
| `--dns-attempts N` | `8` | 1–20 | Samples per DNS resolver |
| `--latency-attempts N` | `10` | 1–20 | Samples per latency target |
| `--speed-threads N` | `8` | 1–32 | Concurrent speed test streams |
| `--iperf-duration DURATION` | `15s` | 1s–30s | iperf3 duration per node and direction |
| `--dns-resolvers [name=]host:port,...` | built-in list | — | Override DNS resolvers |
| `--latency-targets [name=]host:port,...` | built-in list | — | Override latency targets |
| `--route-targets [name=]host,...` | built-in list | — | Override route targets |
| `--stun-servers [name=]host:port,...` | built-in list | — | Override STUN servers |
| `--iperf-targets [name=]host:start[-end],...` | built-in list | — | Override iperf3 nodes |
| `--media-region global,jp,tw,hk,cn` | all | — | Media detection regions |
| `--backtrace-city beijing\|guangzhou\|shanghai\|chengdu\|all` | `beijing,guangzhou` | — | China return-path cities |
| `--backtrace-targets carrier:Name=IP/hostname,...` | expanded from cities | — | Override China return targets; `carrier` must be `telecom`, `unicom` or `mobile` |
| `--ookla-servers telecom=ID,unicom=ID,mobile=ID` | auto-selected | — | Pin Ookla carrier servers; IDs come from the official client |

Behavior and exit:

| Option | Default | Description |
| --- | --- | --- |
| `--interactive` | off | Start the interactive wizard; skipped automatically without a terminal |
| `--yes` | off | Skip the wizard and run with the current options |
| `--strict` | off | Return exit code `2` on probe warnings or errors |
| `--version` | — | Print the version and exit (not supported by `plan`) |
| `--help` / `-h` | — | Show all run options |

**`ecs render --input FILE`**

| Option | Default | Description |
| --- | --- | --- |
| `--input FILE` | required | Path to an ecs JSON report |
| `--format json,md,html` | `json,md,html` | Output formats |
| `--output DIR` | same directory as the input | Output directory |
| `--name PREFIX` | input file name without extension | Report file name prefix |
| `--score-baseline FILE` | embedded reference | Scoring leaderboard reference file |

**`ecs compare REPORT...`** (two or more JSON reports)

| Option | Default | Description |
| --- | --- | --- |
| `--reference N` | `1` | Use the Nth report as the baseline (1-based) |
| `--format json,md,html` | `json,md,html` | Output formats |
| `--output DIR` | `./reports` | Output directory |
| `--name PREFIX` | timestamped | Output file name prefix |
| `--color auto\|none\|basic\|256\|truecolor\|always` | `auto` | Terminal color mode |
| `--no-color` | off | Disable ANSI colors |

**`ecs leaderboard REPORT...`**

| Option | Default | Description |
| --- | --- | --- |
| `--output FILE` | `ecs-baseline.json` | Path for the generated leaderboard reference |
| `--source TEXT` | empty | Where this reference came from; stored in the file and shown in reports |
| `--annotate` | off | Emit GitHub Actions check annotations so outliers show up in the check page |
| `--verbose` | off | Also list tier/metric combinations with too few samples to judge |
| `--strict` | off | Fail on invalid inputs without writing a baseline file |

**`ecs submit --input FILE`**

| Option | Default | Description |
| --- | --- | --- |
| `--input FILE` | required | Path to an ecs JSON report |
| `--output PATH` | auto-named by content | Output path or directory for the submission |
| `--provider NAME` | empty | Self-reported provider (e.g. `vultr`, `hetzner`) for leaderboard grouping |
| `--region NAME` | empty | Self-reported region (e.g. `jp`, `us-west`) for leaderboard grouping |
| `--note TEXT` | empty | Free-form note, up to 200 characters |

**`ecs list` / `ecs doctor` / `ecs version` / `ecs help`** take no options; **`ecs config`** accepts only the `example` subcommand.

## Reports, rendering and comparison

A run can write three file formats:

```text
ecs-report-YYYYMMDD-HHMMSS.json    # ecs.report/v1, canonical machine data
ecs-report-YYYYMMDD-HHMMSS.md
ecs-report-YYYYMMDD-HHMMSS.html
```

```sh
ecs --format json,html --output ./reports --name my-run
ecs --lang en render --input ./reports/my-run.json --format html,md
ecs compare yesterday.json today.json --format md,html
ecs compare a.json b.json c.json --reference 2
```

JSON preserves canonical fields, tables and raw evidence. Rendering in Chinese or English does not change machine data, and the same JSON can be rendered again in another language. `compare` accepts only JSON reports that pass the current `ecs.report/v1` exact loader; reports with a different schema version are rejected. See [docs/schema.md](docs/schema.md) for field definitions.

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
The leaderboard counts benchmark samples by the full report's `run.id`. A submission carries its stable anonymous derivative as `sample_id` and never includes the raw `run.id`; `id` remains the artifact content identity. Thus a report and submission from one run count once, while different runs count separately even when their metrics match.

## Configuration file

The complete option list is in the [full option reference](#full-option-reference) above; `ecs run --help` is authoritative at runtime. Generate a configuration file as follows; unknown fields are rejected and command-line options take precedence:

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
