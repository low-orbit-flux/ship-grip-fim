# ship-grip-fim — Usage Guide

A File Integrity Monitoring (FIM) tool that scans directory trees, computes MD5 checksums, stores reports, and detects changes between scans.

## Build

```bash
go build .
```

## Commands

### scan
Recursively scan the configured path and save a timestamped report.

```bash
./ship-grip-fim scan
./ship-grip-fim --path=/etc --reportName=etc_baseline scan
```

### list
List all stored reports.

```bash
./ship-grip-fim list
```

### data
Display file paths and checksums from a report.

```bash
./ship-grip-fim data <REPORT_ID>
# Example:
./ship-grip-fim data default_adhoc_report_2025-09-15_23:09:49
```

### compare
Compare two reports and show what changed, was added, removed, or moved.

```bash
./ship-grip-fim compare <REPORT_ID_1> <REPORT_ID_2>
# Example:
./ship-grip-fim compare default_adhoc_report_2025-09-15_23:09:49 default_adhoc_report_2025-09-15_23:10:28
```

Output categories: `NEW`, `MISSING`, `CHANGED`, `MOVED`.

If comparing reports from a remounted filesystem (base path differs):

```bash
./ship-grip-fim --removeBasePath=true compare <REPORT_ID_1> <REPORT_ID_2>
```

### gui
Open the graphical interface. Requires a display (`DISPLAY` must be set, or use X11 forwarding).

```bash
./ship-grip-fim gui
./ship-grip-fim --configFile=/etc/fim.conf gui   # load a specific config first
```

**Tabs:**

| Tab | What it does |
|-----|--------------|
| Scan & Reports | Configure path/reportName/reportDir, run scans, browse the report list, view data, compare two reports |
| Agent | Start/stop the local agent server with a button; bind address and port are editable |
| Remote | Connect to any running agent by host:port; list reports, view data, run scans, compare |
| Hosts | Load `hosts.conf`, see all configured hosts in a table, Ping All, Sync Reports, Start All (SSH), or run any command on all hosts in parallel |

The GUI reuses the same config, config file, and hosts.conf as the CLI.

---

### agent
Start the TCP agent server (address and port from config / `--agentHost` / `--agentPort`).

```bash
./ship-grip-fim agent
./ship-grip-fim --agentHost=0.0.0.0 --agentPort=9000 agent
```

The server accepts multiple simultaneous connections. Each connection can issue any of the commands below. A scan or compare that is in progress blocks a second scan/compare from starting (check with `status`).

### remote
Connect to a running agent and execute a command.

```bash
./ship-grip-fim remote <host> <port> <command> [args...]
```

| Remote command | Example |
|----------------|---------|
| `scan` | `./ship-grip-fim remote localhost 8080 scan` |
| `list` | `./ship-grip-fim remote localhost 8080 list` |
| `data <ID>` | `./ship-grip-fim remote localhost 8080 data report_id` |
| `compare <ID1> <ID2>` | `./ship-grip-fim remote localhost 8080 compare id1 id2` |
| `status` | `./ship-grip-fim remote localhost 8080 status` |

**scan** and **compare** run in a background goroutine on the server side; the client blocks until the operation finishes and then receives the result. Use `status` from a second terminal to check whether an operation is in progress.

```bash
# Terminal 1 — start a scan (blocks until done)
./ship-grip-fim remote localhost 8080 scan

# Terminal 2 — check what's happening while terminal 1 waits
./ship-grip-fim remote localhost 8080 status
```

## CLI Flags

All flags are optional and override config file values.

| Flag | Default | Description |
|------|---------|-------------|
| `--path` | `/storage1` | Directory to scan |
| `--reportDir` | `~/integirty_reports` | Where reports are stored |
| `--reportName` | `default_adhoc_report` | Prefix for report filenames |
| `--host` | `duck-puppy` | Hostname embedded in reports |
| `--paraCount` | `8` | Parallel worker count for checksums |
| `--dataSource` | `file` | Storage backend (`file` only) |
| `--removeBasePath` | `false` | Strip base path in comparisons |
| `--configFile` | `integrity.conf` | Path to main config file |
| `--ignorePathConfig` | `integrity_ignore.cfg` | Regex patterns for files to exclude |
| `--ignorePathNoWalkConfig` | `integrity_ignore_no_walk.cfg` | Regex patterns for directories to skip |
| `--agentHost` | `localhost` | Agent server bind address / interface |
| `--agentPort` | `8080` | Agent server port |
| `--hostsConfig` | `hosts.conf` | Remote hosts config file path |

## Configuration

Settings are applied in priority order: hardcoded defaults → `integrity.conf` → CLI flags.

**`integrity.conf`** — key-value format (`key="value"`, `#` for comments):

```ini
reportName="my_report"
host="my-hostname"
path="/data"
reportDir="../integrity_reports"
paraCount="8"
removeBasePath="false"
ignorePathConfig="integrity_ignore.cfg"
ignorePathNoWalkConfig="integrity_ignore_no_walk.cfg"
agentHost="localhost"
agentPort="8080"
hostsConfig="hosts.conf"
```

**`integrity_ignore.cfg`** — regex patterns for files to exclude from results (e.g. `.*\.tmp$`).

**`integrity_ignore_no_walk.cfg`** — regex patterns for directories to skip entirely during traversal (e.g. `^/proc`).

## Scheduling (agent-side)

The scheduler lives entirely inside the agent process. Jobs run whether or not any client is connected.

### schedule.conf
Schedules are persisted in `schedule.conf` (loaded on agent start, rewritten on every add/remove).

```
# name|cronExpression|command
daily_scan|@daily|scan
nightly|0 2 * * *|scan
every_6h|0 */6 * * *|scan
```

Supported cron descriptors: `@hourly`, `@daily`, `@weekly`, `@monthly`.  
Full 5-field cron syntax is also accepted (`minute hour dom month dow`).

### Remote schedule commands

```bash
# list scheduled jobs (name, cron, command, next run)
./ship-grip-fim remote localhost 8080 schedule list

# add a job  — use | to separate name, cron expression, command
./ship-grip-fim remote localhost 8080 schedule add 'daily_scan|@daily|scan'
./ship-grip-fim remote localhost 8080 schedule add 'nightly|0 2 * * *|scan'

# remove a job
./ship-grip-fim remote localhost 8080 schedule remove daily_scan

# show job run history
./ship-grip-fim remote localhost 8080 schedule history

# combined overview: running + scheduled + recent history
./ship-grip-fim remote localhost 8080 jobs
```

**Concurrency rule**: a scheduled job is skipped (recorded as "skipped" in history) if a scan or compare is already running. Only one operation at a time per agent.

### GUI Schedule tab
Connect to any running agent by host:port, then:
- View all scheduled jobs in a table (name, schedule, command, next run)
- Add a job with the Name / Cron / Command form
- Select a row and click **Remove Selected**
- View run history (newest first) in the right-hand table

---

## Multi-Host / Remote Agent Workflow

### hosts.conf
Configure remote agents in `hosts.conf` (pipe-delimited, one per line):

```
# alias|address|port|path|reportName[|sshUser[|binaryPath]]
server1|192.168.1.10|8080|/data|server1
server2|192.168.1.20|9000|/storage|server2|admin|/usr/local/bin/ship-grip-fim
```

`sshUser` and `binaryPath` are only needed for the `start` command.

### hosts
List all configured remote hosts.

```bash
./ship-grip-fim hosts
```

### pingall
Check connectivity and status of every configured agent in parallel.

```bash
./ship-grip-fim pingall
```

### remoteall
Run any command on all configured agents in parallel and print each host's output.

```bash
./ship-grip-fim remoteall scan
./ship-grip-fim remoteall list
./ship-grip-fim remoteall status
```

### sync
Pull all reports from one agent (by alias) or all agents into `<reportDir>/remote/<alias>/`.
Files already present locally are skipped — reports are immutable once written.

```bash
./ship-grip-fim sync              # all hosts
./ship-grip-fim sync server1      # one host
```

### start
Start the agent process on one (by alias) or all remote hosts via SSH.
Requires `sshUser` and `binaryPath` in `hosts.conf`. Uses `BatchMode=yes` (key-based auth).

```bash
./ship-grip-fim start             # all hosts
./ship-grip-fim start server1     # one host
```

---

---

## Prometheus Exporter

The exporter connects to every agent listed in `hosts.conf`, queries the `metrics` command in parallel on each Prometheus scrape, and exposes the results as `fim_*` gauge metrics.

### Start the exporter

```bash
./ship-grip-fim exporter
./ship-grip-fim --exporterHost=0.0.0.0 --exporterPort=9110 exporter
```

The exporter reads `hosts.conf` (or `--hostsConfig`) to discover agents.  
Scrape endpoint: `http://<host>:9110/metrics`  
Health check: `http://<host>:9110/health`

### Exporter config options

| Key in `integrity.conf` | CLI flag | Default | Description |
|-------------------------|----------|---------|-------------|
| `exporterHost` | `--exporterHost` | `0.0.0.0` | Bind address for the HTTP server |
| `exporterPort` | `--exporterPort` | `9110` | Port for the HTTP server |

### Prometheus scrape config example

```yaml
scrape_configs:
  - job_name: ship_grip_fim
    static_configs:
      - targets: ['localhost:9110']
```

### Exposed metrics

All metrics carry a `host` label set to the agent's alias from `hosts.conf`.  
Per-job metrics also carry a `job` label.

| Metric | Description |
|--------|-------------|
| `fim_up` | 1 if the agent is reachable, 0 otherwise |
| `fim_scan_running` | 1 if a scan is currently running |
| `fim_compare_running` | 1 if a compare is currently running |
| `fim_reports_total` | Number of scan reports stored on the agent |
| `fim_last_report_time_seconds` | Unix timestamp of the most recent report file |
| `fim_last_scan_time_seconds` | Unix timestamp of when the last scan completed |
| `fim_last_scan_files` | Files checksummed in the last scan |
| `fim_last_scan_duration_seconds` | Wall-clock time the last scan took |
| `fim_last_compare_time_seconds` | Unix timestamp of the last compare |
| `fim_last_compare_changed_total` | Changed files in the last compare |
| `fim_last_compare_new_total` | New files in the last compare |
| `fim_last_compare_missing_total` | Missing files in the last compare |
| `fim_last_compare_moved_total` | Moved files in the last compare |
| `fim_jobs_total` | Number of scheduled cron jobs |
| `fim_job_next_run_time_seconds` | Unix timestamp of the next scheduled run |
| `fim_job_last_run_time_seconds` | Unix timestamp of the last run (0 = never) |
| `fim_job_last_run_ok` | 1 if the last run succeeded, 0 otherwise |
| `fim_collect_duration_seconds` | Time spent querying the agent per scrape |

### Alerting examples (PromQL)

```promql
# Agent down
fim_up == 0

# Agent hasn't scanned in 24 hours
time() - fim_last_scan_time_seconds > 86400

# Integrity drift: changed or missing files detected
fim_last_compare_changed_total > 0 or fim_last_compare_missing_total > 0

# Scan is taking a long time
fim_scan_running == 1 and (time() - fim_last_scan_time_seconds) > 3600
```

---

## Typical Workflow

```bash
# 1. Take a baseline scan
./ship-grip-fim --path=/etc --reportName=etc scan

# 2. (Later) Take another scan
./ship-grip-fim --path=/etc --reportName=etc scan

# 3. List reports to get IDs
./ship-grip-fim list

# 4. Compare to detect drift
./ship-grip-fim compare etc_2025-09-15_08:00:00 etc_2025-09-15_20:00:00
```
