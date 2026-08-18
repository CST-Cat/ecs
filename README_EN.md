# ecs

An ad-free VPS benchmark suite for Linux that uploads nothing by default. One run covers 21 modules — local performance benchmarks, network quality, routing and China return paths, streaming availability and IP reputation — and writes the results to four local reports: JSON, txt, Markdown and HTML.

The project commits to:

- no advertising, sponsorship or referral content of any kind;
- reports written to `/tmp` by default, with no automatic upload path;
- every module declares its methodology (`inventory` / `standard-benchmark` / `protocol-measurement` / `heuristic`) and comparison scope, and a score is one division you can verify by hand;
- pure Go standard library — `go.mod` has no third-party requires — shipped as a single static binary.

The one exception is the `ookla` module: it invokes the official speedtest client on the host, Ookla processes that measurement data independently, and the report labels its separate privacy terms explicitly.

## Quick start

### Run once, install nothing

```sh
curl -fsSL https://raw.githubusercontent.com/CST-Cat/ecs/main/run.sh | sh
```

The script downloads a checksummed release, stages any missing fixed-release benchmark tools for your architecture under a temporary PATH (**never writing to system directories or installing through the system package manager**), and cleans up when it exits. If `ookla` is explicitly selected while `speedtest` is missing, `run.sh` separately uses a verified official package-source extraction path scoped to this run's `$WORK`; it still does not install anything on the system. A bare pipe enters the interactive wizard; passing arguments skips it and starts immediately:

```sh
curl -fsSL https://raw.githubusercontent.com/CST-Cat/ecs/main/run.sh | sh -s -- --profile full --lang en
```

Reports go to `${TMPDIR:-/tmp}` by default and no new directory is created; pass `--output PATH` to choose a destination.

### Compare once, install nothing

When you already have two or more JSON reports, you do not need to install ecs first:

```sh
curl -fsSL https://raw.githubusercontent.com/CST-Cat/ecs/main/compare.sh | sh -s -- yesterday.json today.json
```

The script downloads only the main binary — comparison is pure local computation, so no benchmark tools or corpus are involved — verifies its SHA-256 before running it, and **removes the binary on exit while keeping the comparison**. Results are written to a private directory under `/tmp` whose path is printed; nothing is created in your current directory.

The verified binary is cached under `${XDG_CACHE_HOME:-~/.cache}/ecs/<tag>` and reused on later runs without network access; its digest is re-checked every time, so a tampered cache is never executed. `--no-cache` disables it and `--install` hands the binary to `install.sh` to put it on PATH. Every other option is passed straight to `ecs compare`, in either `--format txt` or `--format=txt` form.

Input reports may declare different schema versions: across versions only metrics present in all inputs with identical signatures are compared, comparability drops to partial, and the overview and notices say so.

If you already use `run.sh`, `--compare` reaches the same path — it hands over to `compare.sh`, so the two entry points are equivalent:

```sh
curl -fsSL https://raw.githubusercontent.com/CST-Cat/ecs/main/run.sh | sh -s -- --compare yesterday.json today.json
```

### Install the binary

```sh
ECS_REPOSITORY=CST-Cat/ecs ./install.sh                     # ecs only
ECS_REPOSITORY=CST-Cat/ecs ./install.sh --with-benchmarks   # also install sysbench / fio / iperf3 via the system package manager
./install.sh --from ./bin/ecs                               # install a locally built binary
```

The installer downloads exactly one release asset and verifies its SHA-256 against `checksums.txt`, exiting on any mismatch. It targets `/usr/local/bin` (falling back to `~/.local/bin` when that is not writable). A default binary installation uses neither `sudo` nor the system package manager; only explicit `--with-benchmarks` installs `sysbench`, `fio` and `iperf3` through the detected system package manager, which may require `sudo`.

### Build from source

```sh
make build          # → bin/ecs
make test           # default Go tests: go test ./...
make check          # ./scripts/ci/check.sh: static/source-rule/schema/workflow YAML/shell syntax and other quality gates
make cross          # cross-compile all seven Linux architectures into dist/
```

`make test` runs only the default Go tests (`go test ./...`); `make check` invokes `./scripts/ci/check.sh` for static, source-rule, schema, workflow YAML, shell-syntax and other quality gates. CI splits `unit`, `compat`, `quality`, `race`, `integration`, `cross` and submission validation into separate jobs in `.github/workflows/ci.yml`: `compat` runs source and parser tests on `1.22.x` and `stable`, while every other CI job uses `stable`; `race` runs `go test -race ./...`, `integration` uses real benchmark tools, and `cross` builds all seven Linux architectures.

`go.mod`'s `go 1.22` only declares the minimum source-compatibility version; it does not select the CI or Release compiler. The Release workflow pins its compiler independently with `ECS_RELEASE_GO` (currently `1.26.5`) instead of deriving it from `go.mod`. Analysis tools, including `govulncheck`, are managed independently by `devtools/go.mod`. A `govulncheck` finding is a scan warning by default; ordinary, unknown-severity or unreachable findings in post-release review only warn and preserve JSON/triage, while scanner, network, input, command, JSON or output errors hard-fail. Only a finding that the Release workflow's `security` job confirms is explicitly severe (HIGH/CRITICAL) and reachable in the actual ECS binary blocks the release. An automatic Go upgrade proposal also requires an exact match in Go's official stable Release list.

Building ecs from source requires Go 1.22 or newer (the toolchain is fetched automatically). Running a release binary requires no Go toolchain at all — they are statically linked, so downloading and extracting is enough.

## Commands

| Command | Purpose |
| --- | --- |
| `ecs [run] [options]` | Run tests, `standard` profile by default |
| `ecs list` | Show profiles, modules, exposure levels and IP quality sources |
| `ecs render --input FILE` | Re-export all four formats from an existing JSON report, without re-probing |
| `ecs compare REPORTS...` | Compare two or more JSON reports |
| `ecs config example` | Print a sample configuration file |
| `ecs doctor` | Check that the standard benchmark tools are ready |
| `ecs leaderboard REPORTS...` | Aggregate a leaderboard reference from reports/submissions |
| `ecs baseline REPORTS...` | Same as `leaderboard` |
| `ecs submit --input FILE` | Export a minimized, publishable submission from a full report |
| `ecs version` | Show the version |

Every subcommand accepts `--lang zh|en`; without it the language comes from the environment, falling back to Chinese.

Exit codes: `0` success, `1` argument or runtime error, `2` (with `--strict` only) warnings or failed probes, `130` interrupted.

## Profiles and modules

| Profile | Modules | Contents |
| --- | --- | --- |
| `standard` (default) | 19 | Local and public modules including `cnspeed`; excludes multi-source IP quality and Ookla |
| `full` | 21 | Every module, adding `network` and `ookla` |

A profile is only a module-count shortcut: **both profiles use identical benchmark depth and node sets**, so a module means the same thing regardless of which shortcut selected it, and results stay directly comparable. `--only` can name any module from any profile: it replaces the profile's set outright rather than filtering within it, so every module stays reachable from every shortcut.

| Module | Name | Exposure | Description |
| --- | --- | --- | --- |
| `system` | System & Resources | local | System, virtualization, resources, kernel network stack |
| `network` | Network & IP Quality | thirdparty | IPv4/IPv6, native/broadcast, multi-source risk scores and factors |
| `bgp` | BGP & Peering Observation | public | RouteViews current RIB: egress prefix, origin ASN, RPKI and peering path samples |
| `cpu` | CPU Performance | local | sysbench standard CPU benchmark (prime=20000, single- and multi-threaded) |
| `zstd` | zstd Compression | local | Compression/decompression throughput on the fixed Silesia corpus (1T/NT) |
| `npb` | NPB EP + FT Compute | local | NASA NPB-OMP 3.4.4 EP + FT Class A floating-point, FFT and multicore throughput (1T/NT) |
| `memory` | Memory Performance | local | Official STREAM memory bandwidth (Copy/Scale/Add/Triad × 1T/NT) |
| `crypto` | Server Cryptography | local | OpenSSL speed AES-256-GCM, ChaCha20-Poly1305, SHA-256 (1/all workers) |
| `disk` | Disk Performance | local | fio Direct I/O benchmark + QD1 4K latency + Crystal/ATTO/mixed matrices |
| `dns` | DNS Quality | public | Public DNS latency, failure rate and jitter |
| `latency` | Network Latency | public | TCP connect latency and reachability |
| `speed` | Network Throughput | public | iperf3 multi-node throughput benchmark (bidirectional TCP multi-stream + UDP) |
| `ports` | Outbound Ports | public | Web, SSH, DNS and mail outbound ports |
| `nat` | NAT Type | public | STUN (RFC 5389/5780) probing of UDP mapping and filtering behaviour |
| `blacklist` | IP Reputation & Mail Readiness | public | 17 DNS blocklists + reverse DNS FCrDNS check |
| `apps` | Application Reachability | public | Telegram DCs, code/image/package/certificate services |
| `cnspeed` | China Carrier Speedtest | public | HTTP download bandwidth to nearby China Telecom/Unicom/Mobile nodes |
| `ookla` | Ookla Speedtest | thirdparty | Runs the local official speedtest; Ookla handles measurement data independently and this **falls outside the ecs no-upload guarantee** |
| `media` | Streaming & AI Services | public | Public-page evidence from 33 platforms (filter with `--media-region`) |
| `route` | Route Trace | public | NextTrace Tiny forward path |
| `backtrace` | China Return Path | public | China return-path detection (pick cities with `--backtrace-city`) |

```sh
ecs --only system,cpu,memory,disk     # just these four
ecs --profile full --skip media       # everything except streaming
```

## Exposure levels and privacy

Modules touch the outside world in very different ways: sysbench sends no packets at all, a public DNS query only reveals your egress IP, while submitting that IP to a commercial risk API hands over **the subject of the query itself**. `--exposure` sets a ceiling based on *what the other side gets to see*; modules above it simply do not run:

| Level | Meaning |
| --- | --- |
| `local` | No network at all |
| `public` | Public infrastructure only; peers see just the egress IP |
| `thirdparty` (default) | Includes third-party intelligence services |
| `any` | Allows all registered third-party services |

```sh
ecs --exposure local     # fully offline: local benchmarks and inventory only
ecs --exposure public    # networked, but no egress IP handed to commercial intelligence APIs
```

Modules pulled in by a profile are filtered out silently when they exceed the ceiling, but a module you named yourself with `--only` raises an **error** instead — what the user wrote down should never be dropped quietly.

**Redaction is on by default**: only this machine's IP addresses are redacted (including JSON and other output); hostnames are not redacted. Remote IPs, route hops and BGP prefixes are not redacted automatically. `--reveal` keeps this machine's full IP addresses, and the wizard warns explicitly when it is enabled. See [SECURITY.md](SECURITY.md) and [THIRD_PARTY.md](THIRD_PARTY.md).

## Composite score

Scoring covers only the four dimensions backed by standard benchmarks: `cpu`, `memory`, `disk` and `bandwidth`.

```
dimension score = measured value / leaderboard reference mean × 1000
total           = equal-weight mean of the covered dimensions
```

Every rule exists to keep the score **checkable** rather than flattering:

- One division, verifiable by hand; the reference mean is shown alongside the result.
- Only dimensions that actually ran are summed. A missing dimension counts as neither zero nor full marks, and coverage is displayed with the score.
- Inside a dimension, metrics are aggregated by equal-weight group before averaging — a wide matrix (ATTO's 18 block sizes) cannot outweigh CPU merely by having more cells.
- Reference values are selected by vCPU tier; when a tier has too few samples it falls back to the global mean, and the report states which one was used.
- A leaderboard position appears only when the reference file actually stores a score distribution. A percentile is never derived from a host count.

The reference is **replaceable data, not a constant baked into the algorithm**:

```sh
ecs leaderboard reports/*.json --output my-baseline.json   # aggregate your own samples
ecs --score-baseline my-baseline.json                      # score against it
ecs render --input old-report.json --score-baseline my-baseline.json   # rescore the same data
```

Release binaries embed a reference copy. The current embedded copy is empty (`sample_count: 0`), so until community submissions accumulate, scoring is only meaningful with a reference file you supply. See [submissions/README_EN.md](submissions/README_EN.md).

## Report output

One run produces all four formats at once. JSON is the single source of truth and the other three are rendered from it — **renderers never re-run probes or derive anything that disagrees with the JSON**.

```
reports/ecs-report-20260813-075451.json    # schema: ecs.report/v1
reports/ecs-report-20260813-075451.txt
reports/ecs-report-20260813-075451.md
reports/ecs-report-20260813-075451.html
```

Running through `run.sh` writes reports to `${TMPDIR:-/tmp}`; running the binary directly defaults to `./reports`. Either way, `--output` changes the directory, `--name` sets the filename prefix, and `--format json,html` emits a subset. The txt file is written without colour so it diffs and pastes cleanly; only `--color always` writes ANSI into files.

Field definitions live in [docs/schema.md](docs/schema.md).

```sh
ecs compare yesterday.json today.json            # compare two runs
ecs compare a.json b.json c.json --reference 2   # use the 2nd report as the reference
ecs render --input report.json --format html     # re-export from JSON
```

## Common options

```
--profile standard|full        profile
--only / --skip MODULE,...     add or remove modules explicitly
--exposure local|public|thirdparty|any   outbound exposure ceiling
--lang zh|en                   interface language
-4 / -6 / --ip-version auto|4|6   protocol family
--format json,txt,md,html      output formats
--output DIR / --name PREFIX   report location and naming
--reveal                       keep full local IP addresses
--strict                       exit 2 on any warning or failure
--interactive / --yes          enable / skip the wizard
--cpu-time 15s                 duration of each CPU/memory run
--disk-mib 2048 / --disk-path . / --disk-multi   disk footprint, directory, extra mounts
--iperf-duration 15s / --speed-threads 8         throughput duration and stream count
--dns-attempts 8 / --latency-attempts 10         sample counts
--backtrace-city beijing,guangzhou|all           return-path cities
--media-region global,jp,tw,hk,cn                streaming regions
--ip-quality-sources all|none|NAME,...            IP quality sources
--score-baseline FILE          scoring reference file
```

`auto` selects available protocol families based on the host's capabilities and each module's own protocol capabilities; modules that support independent dual-stack measurements record IPv4 and IPv6 separately.

Full list: `ecs run --help`. Copy-pasteable scenarios: [examples/README_EN.md](examples/README_EN.md).

## Configuration file

Anything available on the command line can also come from a JSON file. Unknown fields are rejected, so a mistyped key never gets ignored silently.

```sh
ecs config example > ecs.json
ecs --config ecs.json
```

Command-line flags take precedence over the file. Configurable entries include the profile, module add/remove lists, exposure level, protocol family, output formats and directory, every duration and sample count, plus full replacement of the endpoint lists — DNS resolvers, latency targets, route targets, STUN servers and iperf3 nodes.

## External tools

Local performance modules drive standard benchmark programs instead of reimplementing them — a home-grown algorithm has nothing to compare against. `ecs doctor` checks each one:

| Tool | Purpose | Required |
| --- | --- | --- |
| `sysbench` | CPU benchmark | yes |
| `zstd` 1.5.7 | Fixed Silesia corpus compression | yes |
| `npb-ep` / `npb-ft` 3.4.4 | NPB-OMP EP / FT Class A | yes |
| `openssl` 3.5.7 | Cryptography throughput | yes |
| `stream` | Official STREAM memory bandwidth | yes |
| `fio` | Disk Direct I/O | yes |
| `iperf3` | Network throughput | yes |
| `nexttrace-tiny` | Routing and return paths | optional |
| `ping` | ICMP round-trip and loss | optional |
| `speedtest` | Official Ookla client (external service) | optional |

`zstd`, `openssl` and NPB are **version-verified**: cross-machine comparison only means something when the measurement contract is fixed, so a mismatched version is treated as missing. When a tool is absent the report says plainly that the standard result did not run — **no substitute score is ever synthesized**.

Under `run.sh`, missing tools are staged temporarily from the architecture-matched `ecs-tools` release asset: the archive is verified against `checksums.txt`, then its `manifest.json` verifies each binary's SHA-256 individually before anything enters the temporary PATH, which is removed with the work directory on exit. `ECS_AUTO_DEPS=0` disables this staging entirely.

## Platform support

Linux only, released for seven architectures: `amd64`, `arm64`, `armv7`, `386`, `s390x`, `riscv64`, `ppc64le`. Native probes do not require root. No code paths or binaries are provided for other operating systems.

## Documentation

- [SECURITY.md](SECURITY.md) — execution model, boundaries and security guarantees
- [THIRD_PARTY.md](THIRD_PARTY.md) — third-party components, services and what gets sent
- [CONTRIBUTING.md](CONTRIBUTING.md) — requirements for new probes
- [CHANGELOG.md](CHANGELOG.md) — release history
- [docs/schema.md](docs/schema.md) — report JSON schema
- [docs/research.md](docs/research.md) — competitive and upstream capability research
- [examples/README_EN.md](examples/README_EN.md) — usage examples
- [submissions/README_EN.md](submissions/README_EN.md) — leaderboard submission repository

中文：[README.md](README.md)

## Frozen tool versions

Cross-machine comparison only means something when the measurement contract is identical, so the versions below are frozen as of v1. `scripts/build_tools.sh` pins every upstream to both a tag and the commit that tag resolved to when the pin was recorded — a moved or re-cut tag fails the build instead of silently changing what ships.

| Tool | Frozen version | Upstream pin | Verified at runtime |
| --- | --- | --- | --- |
| `sysbench` | 1.0.20 | tag `1.0.20` + commit | — |
| `zstd` | 1.5.7 | tag `v1.5.7` + commit | yes |
| NPB-OMP | 3.4.4 | official `NPB3.4.4.tar.gz` + SHA-256 | yes |
| `openssl` | 3.5.7 | tag `openssl-3.5.7` + commit | yes |
| STREAM | 5.10 | official single-file source + SHA-256 | — |
| `fio` | 3.42 | tag `fio-3.42` + commit | — |
| `iperf3` | 3.21 | tag `3.21` + commit | — |
| `ping` (iputils) | 20250605 | tag `20250605` + commit | — |
| `nexttrace-tiny` | 1.7.1 | tag `v1.7.1` + commit + official asset SHA-256 | — |
| `speedtest` | not frozen | distributed by Ookla, version outside our control | — |

"Verified at runtime" means the probe checks the tool's self-reported version before running and treats a mismatch as missing, producing no result — these three measure a software implementation, so a different version necessarily produces different numbers. The remaining tools measure hardware and link capability; their version is not enforced, but it is recorded in the report alongside the binary's SHA-256 and travels with a submission in the leaderboard's `tools` field, so results can be stratified by contract. `nexttrace-tiny` is the only upstream prebuilt binary in the package — everything else is built from source by this repository. Changing any row changes the measurement contract and requires bumping that module's `measurement.method` under the rules in [docs/schema.md](docs/schema.md).

## Licence

[AGPL-3.0-only](LICENSE). The IP quality module adapts the multi-source coverage, field mapping and risk-banding approach of [xykt/IPQuality](https://github.com/xykt/IPQuality), which is why the whole project is released under AGPL-3.0-only. Attribution and differences are recorded in [NOTICE](NOTICE).
