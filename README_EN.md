# ecs

语言 / Language：[简体中文](README.md) | [English](README_EN.md)

An ad-free, locally written, and auditable all-in-one benchmark for Linux VPS instances.

`ecs` combines system inventory, CPU, memory, disk, network, IP quality, streaming-service checks, route diagnostics, and China return-path analysis into one structured result. The same data can be rendered as JSON, plain text, Markdown, or a standalone HTML report. Every result retains its method, parameters, tool, duration, warnings, valid/planned sample coverage, and raw evidence so it can be reviewed or rendered again later.

The project follows three rules:

- no ads, sponsorships, or referral content;
- reports are written only to a local path and are never uploaded automatically;
- missing tools do not produce fabricated substitute scores, and incompatible measurements are not forced into one conclusion.

## Quick start

The one-line runner supports Linux only. It downloads and verifies the latest Release, reuses tools already installed on the system, and stages missing components inside a temporary directory for the current run. The temporary files are removed afterward, without installing anything into system directories.

`ecs` provides 18 test modules in total. Standard runs 16 of them by default, while Full runs all 18.

### Standard: everyday benchmark

```bash
curl -fsSL https://raw.githubusercontent.com/CST-Cat/ecs/main/run.sh | \
  sh -s -- --profile standard --yes --output ./reports
```

Tests: system and hardware inventory, BGP, CPU, memory, disk, DNS, network latency, iperf3 throughput, ports, NAT, blocklists, common services, China Telecom/Unicom/Mobile HTTP speed, streaming services, route diagnostics, and China return paths.

Module IDs: `system`, `bgp`, `cpu`, `memory`, `disk`, `dns`, `latency`, `speed`, `ports`, `nat`, `blacklist`, `apps`, `cnspeed`, `media`, `route`, and `backtrace`.

### Full: all tests

```bash
curl -fsSL https://raw.githubusercontent.com/CST-Cat/ecs/main/run.sh | \
  sh -s -- --profile full --yes --output ./reports
```

Full adds multi-source IP quality (`network`) and the official Ookla Speedtest (`ookla`) to Standard's 16 modules, for all 18 modules in total. Both depend on external commercial services; free or public access paths may be affected by quotas, caches, and database update delays, so their results are supplemental. Ookla can also consume substantial traffic and processes measurement data under its own terms; it is outside `ecs`'s local-report guarantee.

> `speed`, `cnspeed`, and `ookla` may saturate the connection. Traffic is duration-based and has no fixed byte limit.

### Only: choose specific tests

`--only` is not a third preset. It replaces the Standard or Full default module set with a comma-separated list of module IDs, so only the selected tests run. It works with both the one-line runner and an installed `ecs` binary.

```bash
curl -fsSL https://raw.githubusercontent.com/CST-Cat/ecs/main/run.sh | \
  sh -s -- --only system,cpu,memory,disk \
  --exposure local --yes --format json,txt --output ./reports
```

Common selections:

| Goal | `--only` value | Tests |
| --- | --- | --- |
| System overview | `system` | Operating system, virtualization, and hardware inventory |
| Local performance | `system,cpu,memory,disk` | System, CPU, memory, and disk with no network requests |
| BGP and reputation | `bgp,blacklist` | Public BGP observations and DNS blocklists |
| Multi-source IP quality | `network` | Commercial and third-party IP intelligence; included by Full |
| Network quality | `dns,latency,speed,ports,nat` | DNS, latency, iperf3, ports, and NAT |
| Service access | `apps,media` | Common services and streaming-service checks |
| Route diagnostics | `route,backtrace` | Forward routes and China return paths |
| China bandwidth | `cnspeed` | Community HTTP nodes; included by Standard |
| Ookla speed test | `ookla` | Official proprietary client; included by Full |

All 18 module IDs can be combined freely. See the [test modules and tools](#test-modules-and-tools) table below for every ID, test, and implementation.

Running the one-line script without test options opens the interactive wizard when a terminal is available. Supplying `--profile`, `--only`, or other test options runs directly; cron and CI environments are always non-interactive.

## Report output

All four formats are generated from the same result. Use `--format` to select one or more formats and explicitly set the destination directory with `--output`:

```bash
ecs --profile standard \
  --format json,txt,md,html \
  --output ./reports
```

The one-line runner accepts the same options:

```bash
curl -fsSL https://raw.githubusercontent.com/CST-Cat/ecs/main/run.sh | \
  sh -s -- --profile standard --yes \
  --format json,txt,md,html --output ./reports
```

| Format | Option | Best for |
| --- | --- | --- |
| JSON | `--format json` | Automation, archival, further analysis, and re-rendering |
| Plain text | `--format txt` | Terminals, chat, and forum posts |
| Markdown | `--format md` | GitHub, issue trackers, and Markdown-enabled forums |
| HTML | `--format html` | Browsing, printing, and offline sharing; standalone with no external assets |

All four formats show valid/planned observations and a proportional bar for every module. This describes collection and parsing completeness, not the accuracy of commercial IP intelligence or streaming-platform decisions.

Evidence also carries a stable grade: `complete`, `partial`, `insufficient`, or `not_planned`. Timeouts, DNS errors, refused connections, rate limits, HTTP rejections, parse errors, missing tools, and related failures are stored as stable machine fields in `failures[]` instead of being buried only in prose; txt, Markdown, and HTML render them as well.

`system` reports effective cgroup CPU/memory limits, cpusets, CPU throttling, PSI CPU/memory/I/O pressure, and OOM events. CPU, STREAM, and fio are retried once only when pre-test load, steal, PSI, cgroup throttling, or OOM interference is detected; clean runs are never extended unconditionally. Selection first discards attempts without valid evidence, then uses the less-interfered attempt, keeping the first on a tie—never whichever benchmark number is higher. JSON retains both attempts, their evidence, metrics, and interference reasons.

`--output` names a directory, which is created if necessary. Use `--name` to set a stable filename prefix:

```bash
ecs --profile standard \
  --format json,txt,md,html \
  --output ./reports \
  --name my-vps
```

This creates:

```text
./reports/my-vps.json
./reports/my-vps.txt
./reports/my-vps.md
./reports/my-vps.html
```

An existing JSON report can be rendered again without rerunning any tests:

```bash
ecs render \
  --input ./reports/my-vps.json \
  --format json,txt,md,html \
  --output ./exported
```

See the [report schema](docs/schema.md) for fields and stable identifiers.

## Comparing multiple reports

`ecs compare` safely compares anywhere from two to many JSON reports and emits JSON, plain text, Markdown, and HTML from one comparison. `--reference` is a 1-based report number and defaults to the first input.

```bash
# Before/after or two-machine comparison
ecs compare ./reports/old.json ./reports/new.json \
  --reference 1 \
  --format json,txt,md,html \
  --output ./compare \
  --name old-vs-new

# Two to many machines; the shell expands every JSON file in the directory
ecs compare ./fleet/*.json \
  --reference 1 \
  --format json,txt,md,html \
  --output ./compare \
  --name fleet
```

The presentation adapts to the input count:

| Reports | Terminal / Markdown | HTML |
| ---: | --- | --- |
| 2 | Reference and candidate side by side with the direct change | Two-column cards emphasizing the before/after relationship |
| 3–5 | A compact one-row-per-metric matrix | Adaptive multi-column cards |
| 6 or more | A vertical ranking per metric, avoiding squeezed columns | Ranked lists that collapse to one column on narrow screens |

This is not a concatenation of complete reports. Within each comparable group, `★`, bold text, colour, and proportional bars automatically highlight the best number; `▲` / `▼` mark improvement or regression relative to the reference. Symbols, ranks, and density bars still carry the hierarchy in a colourless terminal. The comparison keeps status, evidence, key performance metrics, factual changes, and method issues, making it materially shorter than reading every full report.

A numeric comparison is made only when module ID, metric key, `method`, unit, performance direction, and machine parameter scope all match. New reports record thread count, duration, tool version/hash, file size, and target-set fingerprints directly. Values from different sysbench/fio/iperf3 parameters or method versions, or values without `higher_is_better` or a valid parameter scope, are never forced into one ranking; they appear under incompatible methods instead. Parameters are not guessed from older report formats, so rerun the tests to produce fresh inputs. Discrete IP, ASN, route, and platform-status changes are shown as facts without claiming that one value is better. All comparison work stays local and no input report is uploaded.

## Test modules and tools

“Built-in” means the protocol is implemented by `ecs` itself or the data is read from Linux system interfaces; no additional executable is required.

| Module | What it measures | Tool or data source | Default profile |
| --- | --- | --- | --- |
| `system` | OS, virtualization, CPU, memory, disk, cgroup limits, PSI, and OOM inventory | Built-in collector using `/proc`, `/sys`, cgroups, DMI, and other system interfaces | Standard / Full |
| `network` | Egress IP, ASN, geolocation, and multi-source IP quality | Built-in HTTP client + multiple IP intelligence providers | Full |
| `bgp` | Egress prefix, origin ASN, RPKI, and AS path samples | RouteViews public RIB API | Standard / Full |
| `cpu` | Single/multi-thread rate and P95, scaling, per-thread efficiency, pressure diagnostics, and conditional retry | `sysbench` + built-in statistics | Standard / Full |
| `memory` | Copy, Scale, Add, and Triad bandwidth, scaling, stability, pressure diagnostics, and conditional retry | Official STREAM + built-in statistics | Standard / Full |
| `disk` | Direct I/O, mixed workloads, Crystal/ATTO matrices, latency, pressure diagnostics, and conditional retry | `fio` + built-in statistics | Standard / Full |
| `dns` | Per-resolver P50, P95, jitter, and success rate | Built-in DNS/UDP client | Standard / Full |
| `latency` | Per-target TCP P50/P95/jitter/success and ICMP round trip | Built-in TCP client + `ping` | Standard / Full |
| `speed` | Multi-node TCP throughput, interval stability, retransmits, and UDP loss/jitter | `iperf3` + built-in statistics | Standard / Full |
| `ports` | Common service-port reachability | Built-in TCP client | Standard / Full |
| `nat` | UDP mapping and filtering behavior | Built-in STUN client | Standard / Full |
| `blacklist` | DNSBL listed/clean/refused/failed counts and reverse-DNS consistency | Built-in DNS client + public DNSBL services | Standard / Full |
| `apps` | Reachability of Telegram, code hosts, registries, and package mirrors | Built-in TCP client | Standard / Full |
| `cnspeed` | HTTP download speed to China Telecom, Unicom, and Mobile nodes | Built-in HTTP client + community node list | Standard / Full |
| `ookla` | Latency and throughput through the official speed-test service | Official Ookla Speedtest CLI (`speedtest`) | Full |
| `media` | Region and availability signals for streaming platforms | Built-in HTTP client + per-platform rules | Standard / Full |
| `route` | Forward path, probed/visible/timed-out hops, and per-target duration | Official NextTrace Tiny + built-in statistics | Standard / Full |
| `backtrace` | China return paths and backbone signatures across four cities and three carriers | Official NextTrace Tiny + built-in signature table | Standard / Full |

The one-line runner stages verified, architecture-matched copies of missing `sysbench`, STREAM, `fio`, `iperf3`, `ping`, and NextTrace Tiny tools. Ookla is never included in that tool bundle. If `ookla` is selected and `speedtest` is missing, the runner downloads and extracts it temporarily from a separate, signed official package source. If no verifiable tool is available, the affected module is explicitly skipped instead of producing a substitute score.

## Selecting tests

Profiles select the default module set only. Standard and Full use the same test depth for every module they share. Use `--only` to choose exact modules or `--skip` to remove modules from a profile.

```bash
# Local inventory and performance only
ecs --only system,cpu,memory,disk \
  --exposure local \
  --format json,txt \
  --output ./reports

# Full profile without streaming-service checks
ecs --profile full \
  --skip media \
  --format json,txt,md,html \
  --output ./reports

# IPv6 China return-path checks only
ecs --only backtrace -6 \
  --backtrace-city all \
  --format txt,html \
  --output ./reports

# Run Ookla by itself from any profile
ecs --only ookla \
  --ookla-servers "telecom=123,unicom=456,mobile=789" \
  --format json,txt \
  --output ./reports
```

Common options:

| Option | Purpose |
| --- | --- |
| `--profile standard\|full` | Select a default module set |
| `--only MODULES` | Run only the comma-separated module IDs |
| `--skip MODULES` | Skip comma-separated module IDs |
| `--exposure LEVEL` | Set the maximum allowed network exposure |
| `-4` / `-6` | Test IPv4 or IPv6 only |
| `--lang zh\|en` | Set the CLI and report language |
| `--reveal` | Keep the complete local IP address in reports |
| `--config FILE` | Read options from a JSON configuration file |
| `--strict` | Return a non-zero status when a probe reports warnings or errors |

Run `ecs run --help` for every option and `ecs list` for the current modules and their exposure levels.

## Privacy and network exposure

Profiles control what is tested. `--exposure` independently controls which destinations and services may be contacted.

| Level | Behavior |
| --- | --- |
| `local` | Run local inventory and benchmarks only; make no network requests |
| `public` | Allow public infrastructure, but do not query commercial IP intelligence services |
| `thirdparty` | Allow third-party intelligence and measurement services; this is the default ceiling |
| `any` | Allow every registered external module |

```bash
# Complete local performance set with no network requests
ecs --only system,cpu,memory,disk \
  --exposure local \
  --format json,txt \
  --output ./reports

# Use the Full module set without commercial IP intelligence or Ookla
ecs --profile full \
  --exposure public \
  --format json,txt,md,html \
  --output ./reports
```

By default, the last two segments of the local IPv4 address and the last four segments of the local IPv6 address are masked. Redaction occurs before JSON is written, so all four formats remain consistent. `--reveal` disables local-IP masking only; remote targets, BGP prefixes, and route-hop addresses remain available for route verification.

`ecs` itself contains no telemetry, run counter, Pastebin integration, hosted report site, or hidden upload path. Networked modules still reveal the egress IP to their destinations. `network` sends the queried IP to the selected intelligence providers, while Ookla operates under its own terms and data-processing rules. See [third-party components and online services](THIRD_PARTY.md) for the complete list.

## Installation and building

The one-line runner is intended for temporary tests. For persistent use, build from source and install the local binary:

```bash
go build -trimpath -o ecs ./cmd/ecs
./install.sh --from ./ecs
```

Install the standard benchmark tools at the same time:

```bash
./install.sh --from ./ecs --with-benchmarks
```

The installer does not invoke `sudo`, modify a package manager, or disable TLS verification by default. STREAM, NextTrace Tiny, and Ookla are still staged on demand by the one-line runner and are not installed persistently by `--with-benchmarks`.

Supported Linux architectures are `amd64`, `arm64`, `armv7`, `386`, `s390x`, `riscv64`, and `ppc64le`.

## Configuration file

Generate an example configuration and run it:

```bash
ecs config example > ecs.json
ecs --config ecs.json \
  --format json,txt,md,html \
  --output ./reports
```

The configuration file can store module selection, exposure level, IP family, report formats, benchmark settings, and custom DNS, latency, iperf3, route, STUN, or Ookla endpoints. Unknown fields are rejected, and command-line options take precedence over file settings.

## Leaderboards and scoring

Raw measurements are always preserved. A combined score is generated only when a valid leaderboard baseline exists; use `--score-baseline` to provide your own. Missing dimensions are not filled with zero, and built-in constants are not used to fabricate rankings.

```bash
# Build a comparable baseline from JSON reports collected on multiple machines
ecs leaderboard \
  --source "My VPS fleet" \
  --output baseline.json \
  ./reports

# Run against the same baseline and write all report formats
ecs --score-baseline baseline.json \
  --format json,txt,md,html \
  --output ./scored-reports
```

For a minimized leaderboard submission that excludes public IPs, hostnames, and route-hop details, see the [submission guide](submissions/README.md).

## Development and verification

```bash
go test ./...
go vet ./...
go test -race ./...
make build
```

More documentation:

- [Contributing guide](CONTRIBUTING.md)
- [Security policy](SECURITY.md)
- [Report schema](docs/schema.md)
- [Upstream and competitor research](docs/research.md)
- [Third-party components and licenses](THIRD_PARTY.md)

## License

[GNU Affero General Public License](LICENSE). See [NOTICE](NOTICE) for IPQuality attribution and modification notes.
