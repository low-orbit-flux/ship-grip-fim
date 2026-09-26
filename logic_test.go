package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

func mustRe(t *testing.T, exprs ...string) []*regexp.Regexp {
	t.Helper()
	var out []*regexp.Regexp
	for _, e := range exprs {
		out = append(out, regexp.MustCompile(e))
	}
	return out
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestValidReportID(t *testing.T) {
	good := []string{"default_adhoc_report_2025-09-15_23:09:49", "compare__a__b__2025-01-01_00:00:00", "x"}
	bad := []string{"", ".", "..", "../etc/passwd", "a/b", `a\b`, "a b", "a\n", "a\x00b"}
	for _, id := range good {
		if !validReportID(id) {
			t.Errorf("expected %q to be valid", id)
		}
	}
	for _, id := range bad {
		if validReportID(id) {
			t.Errorf("expected %q to be rejected", id)
		}
	}
}

func TestParseReportTimestampAndName(t *testing.T) {
	want := time.Date(2025, 9, 15, 23, 9, 49, 0, time.Local)
	for _, name := range []string{
		"my_report_name_2025-09-15_23-09-49", // current, Windows-safe format
		"my_report_name_2025-09-15_23:09:49", // legacy format from older reports
	} {
		if ts := parseReportTimestamp(name); !ts.Equal(want) {
			t.Errorf("%s: timestamp = %v, want %v", name, ts, want)
		}
		if got := extractReportName(name); got != "my_report_name" {
			t.Errorf("extractReportName = %q", got)
		}
	}
	if strings.ContainsAny(time.Now().Format(reportTimeFormat), `:/\`) {
		t.Errorf("reportTimeFormat produces characters that are illegal in Windows file names")
	}
	if !parseReportTimestamp("compare__a_2025-01-01_00:00:00__b_2025-01-02_00:00:00__2025-01-03_01:02:03").Equal(
		time.Date(2025, 1, 3, 1, 2, 3, 0, time.Local)) {
		t.Error("compare report timestamp not parsed from trailing tokens")
	}
	if !parseReportTimestamp("junk").IsZero() {
		t.Error("expected zero time for unparseable name")
	}
}

func TestParallelFileCheck(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a.txt"), "hello")
	writeFile(t, filepath.Join(dir, "sub", "b.txt"), "")
	writeFile(t, filepath.Join(dir, "skip", "c.txt"), "no")
	unreadable := filepath.Join(dir, "secret.txt")
	writeFile(t, unreadable, "x")
	if err := os.Chmod(unreadable, 0000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(unreadable, 0644) })

	for _, para := range []int{0, 1, 3, 50} {
		cfg := configInfo{path: dir, paraCount: para, ignorePathNoWalk: mustRe(t, `/skip$`)}
		fm := SafeFileMap{v: map[string]string{}}
		parallelFileCheck(cfg, &fm)

		if got := fm.v[dir+"/a.txt"]; got != "5d41402abc4b2a76b9719d911017c592" {
			t.Errorf("para=%d: md5(a.txt) = %q", para, got)
		}
		if got := fm.v[dir+"/sub/b.txt"]; got != "d41d8cd98f00b204e9800998ecf8427e" {
			t.Errorf("para=%d: md5(empty) = %q", para, got)
		}
		if _, ok := fm.v[dir+"/skip/c.txt"]; ok {
			t.Errorf("para=%d: no-walk dir was scanned", para)
		}
		if os.Geteuid() != 0 {
			if _, ok := fm.v[unreadable]; ok {
				t.Errorf("para=%d: unreadable file must not be recorded (would hash as empty)", para)
			}
		}
	}

	// An empty directory used to panic with a slice-bounds error.
	empty := t.TempDir()
	fm := SafeFileMap{v: map[string]string{}}
	parallelFileCheck(configInfo{path: empty, paraCount: 8}, &fm)
	if len(fm.v) != 0 {
		t.Errorf("expected no files, got %d", len(fm.v))
	}
}

func TestScanAndCompareRoundTrip(t *testing.T) {
	data := t.TempDir()
	reports := t.TempDir()
	cfg := configInfo{
		dataSource: "file",
		reportDir:  reports,
		reportName: "rt",
		host:       "h",
		path:       data,
		paraCount:  2,
		ignorePath: mustRe(t, `.*/\.DS_Store$`, `.*/Thumbs\.db$`),
	}

	writeFile(t, filepath.Join(data, "same.txt"), "same")
	writeFile(t, filepath.Join(data, "changed.txt"), "v1")
	writeFile(t, filepath.Join(data, "gone.txt"), "gone")
	writeFile(t, filepath.Join(data, "moveme.txt"), "moving")
	writeFile(t, filepath.Join(data, ".DS_Store"), "junk1")
	if err := callScan(cfg); err != nil {
		t.Fatal(err)
	}
	time.Sleep(1100 * time.Millisecond) // report names have 1s resolution

	writeFile(t, filepath.Join(data, "changed.txt"), "v2")
	os.Remove(filepath.Join(data, "gone.txt"))
	os.Rename(filepath.Join(data, "moveme.txt"), filepath.Join(data, "moved.txt"))
	writeFile(t, filepath.Join(data, "new.txt"), "new")
	writeFile(t, filepath.Join(data, ".DS_Store"), "junk2")
	if err := callScan(cfg); err != nil {
		t.Fatal(err)
	}

	older, newer, err := findLastTwoReports(cfg, "rt")
	if err != nil {
		t.Fatal(err)
	}
	cr, _, _, err := buildCompareReport(cfg, older, newer)
	if err != nil {
		t.Fatal(err)
	}

	if len(cr.changedFiles) != 1 || cr.changedFiles[0].path != data+"/changed.txt" {
		t.Errorf("changedFiles = %+v (ignore filter used to add one duplicate per non-matching pattern)", cr.changedFiles)
	}
	if _, ok := cr.newFiles[data+"/new.txt"]; !ok || len(cr.newFiles) != 1 {
		t.Errorf("newFiles = %v", cr.newFiles)
	}
	if _, ok := cr.missingFiles[data+"/gone.txt"]; !ok || len(cr.missingFiles) != 1 {
		t.Errorf("missingFiles = %v", cr.missingFiles)
	}
	if len(cr.movedFiles) != 1 || cr.movedFiles[0].newPath != data+"/moved.txt" {
		t.Errorf("movedFiles = %+v", cr.movedFiles)
	}

	// Identical files moving in bulk must pair one-to-one, not N×N; empty
	// files are never treated as moves (they have no content identity).
	time.Sleep(1100 * time.Millisecond)
	for _, n := range []string{"e1", "e2", "e3"} {
		writeFile(t, filepath.Join(data, n), "duplicate content")
	}
	writeFile(t, filepath.Join(data, "empty_a"), "")
	if err := callScan(cfg); err != nil {
		t.Fatal(err)
	}
	time.Sleep(1100 * time.Millisecond)
	for _, n := range []string{"e1", "e2", "e3"} {
		os.Rename(filepath.Join(data, n), filepath.Join(data, "x"+n))
	}
	os.Rename(filepath.Join(data, "empty_a"), filepath.Join(data, "empty_b"))
	if err := callScan(cfg); err != nil {
		t.Fatal(err)
	}
	older2, newer2, err := findLastTwoReports(cfg, "rt")
	if err != nil {
		t.Fatal(err)
	}
	cr2, _, _, err := buildCompareReport(cfg, older2, newer2)
	if err != nil {
		t.Fatal(err)
	}
	if len(cr2.movedFiles) != 3 || len(cr2.newFiles) != 1 || len(cr2.missingFiles) != 1 {
		t.Errorf("bulk move: moved=%d new=%d missing=%d (want 3 moved, empty file as 1 new + 1 missing)\n%+v",
			len(cr2.movedFiles), len(cr2.newFiles), len(cr2.missingFiles), cr2.movedFiles)
	}
	if _, ok := cr2.newFiles[data+"/empty_b"]; !ok {
		t.Errorf("empty file rename should appear as NEW, got %v", cr2.newFiles)
	}

	// The string form is what the agent/web/GUI return.
	out := compareReportsString(cfg, older, newer)
	for _, want := range []string{"CHANGED - ", "NEW - ", "MISSING - ", "MOVED - "} {
		if !strings.Contains(out, want) {
			t.Errorf("compare output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, ".DS_Store") {
		t.Errorf("ignored file leaked into compare output:\n%s", out)
	}

	// Missing / hostile report IDs must produce an error, not a panic.
	if _, _, _, err := buildCompareReport(cfg, older, "does_not_exist"); err == nil {
		t.Error("expected error for missing report")
	}
	if out := listReportDataString(cfg, "../../etc/passwd"); !strings.HasPrefix(out, "ERROR") {
		t.Errorf("path traversal not rejected: %q", out)
	}

	// list output: one line per report, compare reports included, no duplicates.
	list := listReports(cfg)
	if strings.Count(list, older+"\n") != 1 || strings.Count(list, newer+"\n") != 1 {
		t.Errorf("unexpected list output:\n%s", list)
	}
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, "NEW - ") || strings.HasPrefix(l, "CHANGED - ") || strings.HasPrefix(l, "MOVED - ") || strings.HasPrefix(l, "MISSING - ") {
			if strings.Contains(l, ",") {
				t.Errorf("hash carries a trailing comma: %s", l)
			}
		}
	}
}

func TestParseHostsConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hosts.conf")
	writeFile(t, path, `# comment
a|10.0.0.1|8080|/data|a
b | 10.0.0.2 | 9000 | /home | b | admin | /usr/local/bin/ship-grip-fim
short|only|three
`)
	hosts, err := parseHostsConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(hosts) != 2 {
		t.Fatalf("got %d hosts, want 2", len(hosts))
	}
	if hosts[1].sshUser != "admin" || hosts[1].binaryPath != "/usr/local/bin/ship-grip-fim" || hosts[1].port != "9000" {
		t.Errorf("host b parsed as %+v", hosts[1])
	}
}

func TestParseScheduleOutput(t *testing.T) {
	raw := "NAME  SCHEDULE  COMMAND  NEXT_RUN\n" + strings.Repeat("-", 20) + "\n" +
		"nightly   0 2 * * *   scan   2025-09-26 02:00:00\n" +
		"daily     @daily      scan   -\n"
	rows := parseScheduleList(raw)
	if len(rows) != 2 || rows[0].name != "nightly" || rows[0].schedule != "0 2 * * *" ||
		rows[0].command != "scan" || rows[0].next != "2025-09-26 02:00:00" || rows[1].next != "-" {
		t.Errorf("rows = %+v", rows)
	}
	hist := parseHistoryList("h\n-\nnightly 2025-09-25 02:00:00 2025-09-25 02:10:00 error: something bad\n")
	if len(hist) != 1 || hist[0].start != "2025-09-25 02:00:00" || hist[0].end != "2025-09-25 02:10:00" || hist[0].status != "error: something bad" {
		t.Errorf("hist = %+v", hist)
	}
}

func TestParseMetricsResponse(t *testing.T) {
	var m hostMetrics
	parseMetricsResponse("scan_running=1\nreports_total=3\nlast_scan_time=2025-09-25T10:00:00Z\njob.nightly.next=2025-09-26 02:00:00\njob.nightly.status=complete\n", &m)
	if m.ScanRunning != 1 || m.ReportsTotal != 3 || m.LastScanTime == 0 {
		t.Errorf("m = %+v", m)
	}
	if len(m.Jobs) != 1 || m.Jobs[0].Name != "nightly" || m.Jobs[0].LastStatus != "complete" || m.Jobs[0].NextRun == 0 {
		t.Errorf("jobs = %+v", m.Jobs)
	}
}
