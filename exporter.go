package main

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// hostMetrics holds all values collected from one agent in a single scrape.
type hostMetrics struct {
	Up               float64
	ScanRunning      float64
	CompareRunning   float64
	ReportsTotal     float64
	LastReportTime   float64 // unix seconds; 0 if unknown
	LastScanTime     float64 // unix seconds; 0 if unknown
	LastScanFiles    float64
	LastScanDuration float64 // seconds
	LastCompareTime  float64 // unix seconds; 0 if unknown
	LastCompareChanged float64
	LastCompareNew   float64
	LastCompareMissing float64
	LastCompareMoved float64
	JobsTotal        float64
	Jobs             []jobMetric
	CollectDuration  float64 // seconds to query this agent
}

// jobMetric holds per-job data from the agent metrics response.
type jobMetric struct {
	Name       string
	NextRun    float64 // unix seconds; 0 if unknown / not scheduled
	LastRun    float64 // unix seconds; 0 if never run
	LastStatus string  // "complete", "skipped", "running", "error: …", "never"
}

// fimCollector implements prometheus.Collector.  On every Prometheus scrape it
// queries all configured agents in parallel, then emits per-host gauge metrics.
type fimCollector struct {
	config configInfo

	// Descriptors — one per metric family.
	descUp               *prometheus.Desc
	descScanRunning      *prometheus.Desc
	descCompareRunning   *prometheus.Desc
	descReportsTotal     *prometheus.Desc
	descLastReportTime   *prometheus.Desc
	descLastScanTime     *prometheus.Desc
	descLastScanFiles    *prometheus.Desc
	descLastScanDuration *prometheus.Desc
	descLastCompareTime  *prometheus.Desc
	descLastCompareChanged *prometheus.Desc
	descLastCompareNew   *prometheus.Desc
	descLastCompareMissing *prometheus.Desc
	descLastCompareMoved *prometheus.Desc
	descJobsTotal        *prometheus.Desc
	descJobNextRun       *prometheus.Desc
	descJobLastRun       *prometheus.Desc
	descJobLastOk        *prometheus.Desc
	descCollectDuration  *prometheus.Desc
}

func newFimCollector(config configInfo) *fimCollector {
	host := []string{"host"}
	hostJob := []string{"host", "job"}
	return &fimCollector{
		config:               config,
		descUp:               prometheus.NewDesc("fim_up", "1 if the FIM agent is reachable, 0 otherwise", host, nil),
		descScanRunning:      prometheus.NewDesc("fim_scan_running", "1 if a scan is currently running on the agent", host, nil),
		descCompareRunning:   prometheus.NewDesc("fim_compare_running", "1 if a compare is currently running on the agent", host, nil),
		descReportsTotal:     prometheus.NewDesc("fim_reports_total", "Total number of scan reports stored on the agent", host, nil),
		descLastReportTime:   prometheus.NewDesc("fim_last_report_time_seconds", "Unix timestamp of the most recent scan report file", host, nil),
		descLastScanTime:     prometheus.NewDesc("fim_last_scan_time_seconds", "Unix timestamp of when the most recent scan completed", host, nil),
		descLastScanFiles:    prometheus.NewDesc("fim_last_scan_files", "Number of files checksummed in the most recent scan", host, nil),
		descLastScanDuration: prometheus.NewDesc("fim_last_scan_duration_seconds", "Wall-clock seconds the most recent scan took", host, nil),
		descLastCompareTime:  prometheus.NewDesc("fim_last_compare_time_seconds", "Unix timestamp of when the most recent compare completed", host, nil),
		descLastCompareChanged: prometheus.NewDesc("fim_last_compare_changed_total", "Number of changed files in the most recent compare", host, nil),
		descLastCompareNew:   prometheus.NewDesc("fim_last_compare_new_total", "Number of new files in the most recent compare", host, nil),
		descLastCompareMissing: prometheus.NewDesc("fim_last_compare_missing_total", "Number of missing files in the most recent compare", host, nil),
		descLastCompareMoved: prometheus.NewDesc("fim_last_compare_moved_total", "Number of moved files in the most recent compare", host, nil),
		descJobsTotal:        prometheus.NewDesc("fim_jobs_total", "Number of scheduled cron jobs on the agent", host, nil),
		descJobNextRun:       prometheus.NewDesc("fim_job_next_run_time_seconds", "Unix timestamp of the next scheduled run for this job (0 if not scheduled)", hostJob, nil),
		descJobLastRun:       prometheus.NewDesc("fim_job_last_run_time_seconds", "Unix timestamp of the last run for this job (0 if never run)", hostJob, nil),
		descJobLastOk:        prometheus.NewDesc("fim_job_last_run_ok", "1 if the last run of this job completed successfully, 0 otherwise", hostJob, nil),
		descCollectDuration:  prometheus.NewDesc("fim_collect_duration_seconds", "Seconds spent querying this agent during the last scrape", host, nil),
	}
}

// Describe sends all descriptor pointers to ch.
func (c *fimCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.descUp
	ch <- c.descScanRunning
	ch <- c.descCompareRunning
	ch <- c.descReportsTotal
	ch <- c.descLastReportTime
	ch <- c.descLastScanTime
	ch <- c.descLastScanFiles
	ch <- c.descLastScanDuration
	ch <- c.descLastCompareTime
	ch <- c.descLastCompareChanged
	ch <- c.descLastCompareNew
	ch <- c.descLastCompareMissing
	ch <- c.descLastCompareMoved
	ch <- c.descJobsTotal
	ch <- c.descJobNextRun
	ch <- c.descJobLastRun
	ch <- c.descJobLastOk
	ch <- c.descCollectDuration
}

// Collect queries all configured agents in parallel and emits metrics for each.
func (c *fimCollector) Collect(ch chan<- prometheus.Metric) {
	hosts, err := parseHostsConfig(c.config.hostsConfig)
	if err != nil {
		fmt.Println("ERROR - exporter: reading hosts config:", err)
		return
	}

	type result struct {
		alias string
		m     *hostMetrics
	}
	results := make([]result, len(hosts))
	var wg sync.WaitGroup
	for i, h := range hosts {
		wg.Add(1)
		go func(idx int, host remoteHost) {
			defer wg.Done()
			results[idx] = result{alias: host.alias, m: collectFromHost(host)}
		}(i, h)
	}
	wg.Wait()

	for _, r := range results {
		alias := r.alias
		m := r.m

		// Helper — emit one GaugeValue metric.
		g := func(desc *prometheus.Desc, v float64, extra ...string) {
			labels := append([]string{alias}, extra...)
			ch <- prometheus.MustNewConstMetric(desc, prometheus.GaugeValue, v, labels...)
		}

		g(c.descUp, m.Up)
		if m.Up == 0 {
			// Agent unreachable — only emit fim_up=0 to avoid misleading zeroes.
			continue
		}
		g(c.descScanRunning, m.ScanRunning)
		g(c.descCompareRunning, m.CompareRunning)
		g(c.descReportsTotal, m.ReportsTotal)
		if m.LastReportTime > 0 {
			g(c.descLastReportTime, m.LastReportTime)
		}
		if m.LastScanTime > 0 {
			g(c.descLastScanTime, m.LastScanTime)
			g(c.descLastScanFiles, m.LastScanFiles)
			g(c.descLastScanDuration, m.LastScanDuration)
		}
		if m.LastCompareTime > 0 {
			g(c.descLastCompareTime, m.LastCompareTime)
			g(c.descLastCompareChanged, m.LastCompareChanged)
			g(c.descLastCompareNew, m.LastCompareNew)
			g(c.descLastCompareMissing, m.LastCompareMissing)
			g(c.descLastCompareMoved, m.LastCompareMoved)
		}
		g(c.descJobsTotal, m.JobsTotal)
		for _, j := range m.Jobs {
			g(c.descJobNextRun, j.NextRun, j.Name)
			g(c.descJobLastRun, j.LastRun, j.Name)
			var ok float64
			if j.LastStatus == "complete" {
				ok = 1
			}
			g(c.descJobLastOk, ok, j.Name)
		}
		g(c.descCollectDuration, m.CollectDuration)
	}
}

// collectFromHost queries a single agent's "metrics" command and parses the
// key=value response.
func collectFromHost(h remoteHost) *hostMetrics {
	start := time.Now()
	raw := runRemoteCommandToString(h.address, h.port, []string{"metrics"})
	elapsed := time.Since(start).Seconds()

	m := &hostMetrics{CollectDuration: elapsed}
	if strings.HasPrefix(raw, "ERROR") || strings.TrimSpace(raw) == "" {
		m.Up = 0
		return m
	}
	m.Up = 1
	parseMetricsResponse(raw, m)
	return m
}

// parseMetricsResponse populates m from the key=value text returned by the
// agent "metrics" command.
func parseMetricsResponse(raw string, m *hostMetrics) {
	jobMap := make(map[string]*jobMetric)

	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		eq := strings.IndexByte(line, '=')
		if eq < 0 {
			continue
		}
		key := line[:eq]
		val := line[eq+1:]

		switch key {
		case "scan_running":
			m.ScanRunning = parseMetricFloat(val)
		case "compare_running":
			m.CompareRunning = parseMetricFloat(val)
		case "reports_total":
			m.ReportsTotal = parseMetricFloat(val)
		case "last_report_time":
			m.LastReportTime = parseMetricTime(val)
		case "last_scan_time":
			m.LastScanTime = parseMetricTime(val)
		case "last_scan_files":
			m.LastScanFiles = parseMetricFloat(val)
		case "last_scan_duration":
			m.LastScanDuration = parseMetricFloat(val)
		case "last_compare_time":
			m.LastCompareTime = parseMetricTime(val)
		case "last_compare_changed":
			m.LastCompareChanged = parseMetricFloat(val)
		case "last_compare_new":
			m.LastCompareNew = parseMetricFloat(val)
		case "last_compare_missing":
			m.LastCompareMissing = parseMetricFloat(val)
		case "last_compare_moved":
			m.LastCompareMoved = parseMetricFloat(val)
		case "jobs_total":
			m.JobsTotal = parseMetricFloat(val)
		case "last_scan_report":
			// string label — not a Prometheus metric
		default:
			if !strings.HasPrefix(key, "job.") {
				continue
			}
			// key format: job.<name>.<field>
			rest := key[4:] // strip "job."
			dot := strings.LastIndexByte(rest, '.')
			if dot < 0 {
				continue
			}
			name := rest[:dot]
			field := rest[dot+1:]
			jm, ok := jobMap[name]
			if !ok {
				jm = &jobMetric{Name: name}
				jobMap[name] = jm
			}
			switch field {
			case "next":
				jm.NextRun = parseMetricTime(val)
			case "last":
				jm.LastRun = parseMetricTime(val)
			case "status":
				jm.LastStatus = val
			}
		}
	}
	for _, jm := range jobMap {
		m.Jobs = append(m.Jobs, *jm)
	}
}

func parseMetricFloat(s string) float64 {
	v, _ := strconv.ParseFloat(strings.TrimSpace(s), 64)
	return v
}

// parseMetricTime converts a time string from the agent metrics payload to a
// Unix timestamp in seconds.  Handles RFC3339 (most fields) and the
// "2006-01-02 15:04:05" local format emitted by the scheduler for next-run
// times.  Returns 0 for "never", "-", or any unparseable value.
func parseMetricTime(s string) float64 {
	s = strings.TrimSpace(s)
	if s == "" || s == "never" || s == "-" {
		return 0
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return float64(t.Unix())
	}
	if t, err := time.ParseInLocation("2006-01-02 15:04:05", s, time.Local); err == nil {
		return float64(t.Unix())
	}
	return 0
}

// startExporter registers the FIM collector on a private registry and starts
// the Prometheus HTTP server on exporterHost:exporterPort.
func startExporter(config configInfo) {
	col := newFimCollector(config)
	reg := prometheus.NewRegistry()
	if err := reg.Register(col); err != nil {
		fmt.Println("ERROR - exporter: registering collector:", err)
		return
	}

	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{
		EnableOpenMetrics: false,
	}))
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, "ok")
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `<html><head><title>ship-grip-fim exporter</title></head><body>
<h1>ship-grip-fim Prometheus Exporter</h1>
<p><a href="/metrics">/metrics</a> — Prometheus scrape endpoint</p>
<p><a href="/health">/health</a> — Health check</p>
</body></html>`)
	})

	addr := config.exporterHost + ":" + config.exporterPort
	fmt.Printf("Exporter listening on http://%s/metrics\n", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		fmt.Println("ERROR - exporter:", err)
	}
}
