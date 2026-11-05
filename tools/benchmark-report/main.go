package main

import (
	"bufio"
	"fmt"
	"io"
	"math"
	"os"
	"slices"
	"strconv"
	"strings"
)

const (
	alpha     = 0.05
	threshold = 10.0
)

type samples struct {
	time, bytes, allocs []float64
}

type delta struct {
	pct, p, adjustedP float64
	ok                bool
}

type comparison struct {
	key                 string
	base, head          samples
	time, bytes, allocs delta
}

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: benchmark-report BASE HEAD")
		os.Exit(1)
	}
	if err := run(os.Args[1], os.Args[2]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(basePath, headPath string) error {
	base, err := os.Open(basePath)
	if err != nil {
		return err
	}
	defer func() { _ = base.Close() }()
	head, err := os.Open(headPath)
	if err != nil {
		return err
	}
	defer func() { _ = head.Close() }()
	return report(base, head, os.Stdout)
}

func readSamples(in io.Reader) (map[string]samples, error) {
	results := make(map[string]samples)
	pkg := ""
	scanner := bufio.NewScanner(in)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "pkg: ") {
			pkg = strings.TrimPrefix(line, "pkg: ")
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 || !strings.HasPrefix(fields[0], "Benchmark") {
			continue
		}
		if _, err := strconv.ParseUint(fields[1], 10, 64); err != nil {
			continue
		}
		metrics := make(map[string]float64)
		for i := 2; i+1 < len(fields); i += 2 {
			value, err := strconv.ParseFloat(fields[i], 64)
			if err != nil || math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
				return nil, fmt.Errorf("invalid benchmark metric: %s", line)
			}
			metrics[fields[i+1]] = value
		}
		for _, unit := range []string{"ns/op", "B/op", "allocs/op"} {
			if _, ok := metrics[unit]; !ok {
				return nil, fmt.Errorf("benchmark missing %s: %s", unit, line)
			}
		}
		name := fields[0]
		if index := strings.LastIndexByte(name, '-'); index >= 0 {
			if _, err := strconv.Atoi(name[index+1:]); err == nil {
				name = name[:index]
			}
		}
		key := strings.TrimPrefix(pkg, "github.com/Adyen/agent-marketplace/") + " / " + strings.TrimPrefix(name, "Benchmark")
		s := results[key]
		s.time = append(s.time, metrics["ns/op"])
		s.bytes = append(s.bytes, metrics["B/op"])
		s.allocs = append(s.allocs, metrics["allocs/op"])
		results[key] = s
	}
	return results, scanner.Err()
}

func median(values []float64) float64 {
	sorted := slices.Sorted(slices.Values(values))
	middle := len(sorted) / 2
	if len(sorted)%2 == 0 {
		return (sorted[middle-1] + sorted[middle]) / 2
	}
	return sorted[middle]
}

// mannWhitneyP returns the two-sided Mann-Whitney U p-value using the normal
// approximation with tie and continuity corrections.
func mannWhitneyP(a, b []float64) float64 {
	type sample struct {
		value float64
		first bool
	}
	all := make([]sample, 0, len(a)+len(b))
	for _, v := range a {
		all = append(all, sample{v, true})
	}
	for _, v := range b {
		all = append(all, sample{v, false})
	}
	slices.SortFunc(all, func(x, y sample) int { return compareFloat(x.value, y.value) })
	n := float64(len(all))
	rankSum, ties := 0.0, 0.0
	for i := 0; i < len(all); {
		j := i
		for j < len(all) && all[j].value == all[i].value {
			j++
		}
		rank, t := float64(i+j+1)/2, float64(j-i)
		ties += t*t*t - t
		for _, s := range all[i:j] {
			if s.first {
				rankSum += rank
			}
		}
		i = j
	}
	n1, n2 := float64(len(a)), float64(len(b))
	u := rankSum - n1*(n1+1)/2
	variance := n1 * n2 / 12 * ((n + 1) - ties/(n*(n-1)))
	if variance <= 0 {
		return 1
	}
	z := math.Max(0, math.Abs(u-n1*n2/2)-0.5) / math.Sqrt(variance)
	return math.Erfc(z / math.Sqrt2)
}

func compareFloat(x, y float64) int {
	switch {
	case x < y:
		return -1
	case x > y:
		return 1
	}
	return 0
}

func compare(before, after []float64) delta {
	if len(before) == 0 || len(after) == 0 {
		return delta{}
	}
	b, a := median(before), median(after)
	pct := 0.0
	switch {
	case b != 0:
		pct = 100 * (a/b - 1)
	case a != 0:
		pct = math.Inf(1)
	}
	return delta{pct: pct, p: mannWhitneyP(before, after), ok: true}
}

func (d delta) notable() bool {
	return d.ok && d.adjustedP < alpha && math.Abs(d.pct) >= threshold
}

func (d delta) String() string {
	switch {
	case !d.ok:
		return "—"
	case d.adjustedP >= alpha || math.Abs(d.pct) < 0.05:
		return "~"
	case math.IsInf(d.pct, 1):
		return "+∞"
	case d.notable():
		return fmt.Sprintf("**%+.1f%%**", d.pct)
	}
	return fmt.Sprintf("%+.1f%%", d.pct)
}

func (c comparison) deltas() []delta { return []delta{c.time, c.bytes, c.allocs} }

func (c comparison) notable() bool { return slices.ContainsFunc(c.deltas(), delta.notable) }

func adjustPValues(rows []comparison) int {
	var tests []*delta
	for i := range rows {
		for _, d := range []*delta{&rows[i].time, &rows[i].bytes, &rows[i].allocs} {
			if d.ok {
				tests = append(tests, d)
			}
		}
	}
	slices.SortStableFunc(tests, func(a, b *delta) int { return compareFloat(a.p, b.p) })
	adjusted := 0.0
	for i, d := range tests {
		adjusted = math.Min(1, math.Max(adjusted, float64(len(tests)-i)*d.p))
		d.adjustedP = adjusted
	}
	return len(tests)
}

func duration(values []float64) string {
	if len(values) == 0 {
		return "—"
	}
	ns := median(values)
	for _, unit := range []struct {
		scale float64
		name  string
	}{{1e9, "s"}, {1e6, "ms"}, {1e3, "µs"}} {
		if ns >= unit.scale {
			return fmt.Sprintf("%.3g %s", ns/unit.scale, unit.name)
		}
	}
	return fmt.Sprintf("%.3g ns", ns)
}

func escape(key string) string {
	return strings.NewReplacer("|", "&#124;", "<", "&lt;", ">", "&gt;").Replace(key)
}

func writeTable(out *strings.Builder, rows []comparison, detailed bool) {
	out.WriteString("| Benchmark | Base time | Head time | Time | B/op | allocs/op")
	if detailed {
		out.WriteString(" | Samples (base/head) | Adjusted p (time/B/allocs)")
	}
	out.WriteString(" |\n|---|---:|---:|---:|---:|---:|")
	if detailed {
		out.WriteString("---:|---:|")
	}
	out.WriteByte('\n')
	for _, c := range rows {
		fmt.Fprintf(out, "| %s | %s | %s | %s | %s | %s |", escape(c.key),
			duration(c.base.time), duration(c.head.time), c.time, c.bytes, c.allocs)
		if detailed {
			fmt.Fprintf(out, " %d/%d | %s / %s / %s |", len(c.base.time), len(c.head.time), adjustedP(c.time), adjustedP(c.bytes), adjustedP(c.allocs))
		}
		out.WriteByte('\n')
	}
}

func adjustedP(d delta) string {
	if !d.ok {
		return "—"
	}
	return fmt.Sprintf("%.3g", d.adjustedP)
}

func plural(n int, word string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, word)
	}
	return fmt.Sprintf("%d %ss", n, word)
}

func report(baseReader, headReader io.Reader, out io.Writer) error {
	base, err := readSamples(baseReader)
	if err != nil {
		return err
	}
	head, err := readSamples(headReader)
	if err != nil {
		return err
	}
	if len(head) == 0 {
		return fmt.Errorf("no head benchmark samples")
	}
	keys := make([]string, 0, len(base)+len(head))
	for key := range base {
		keys = append(keys, key)
	}
	for key := range head {
		if _, exists := base[key]; !exists {
			keys = append(keys, key)
		}
	}
	slices.Sort(keys)
	var all, notable []comparison
	var added, removed []string
	var timeChanges []float64
	for _, key := range keys {
		before, after := base[key], head[key]
		c := comparison{key: key, base: before, head: after,
			time: compare(before.time, after.time), bytes: compare(before.bytes, after.bytes), allocs: compare(before.allocs, after.allocs)}
		all = append(all, c)
		switch {
		case len(before.time) == 0:
			added = append(added, fmt.Sprintf("`%s` (%s)", key, duration(after.time)))
		case len(after.time) == 0:
			removed = append(removed, fmt.Sprintf("`%s`", key))
		default:
			timeChanges = append(timeChanges, c.time.pct)
		}
	}
	comparisons := adjustPValues(all)
	increases, decreases := 0, 0
	for _, c := range all {
		if c.notable() {
			notable = append(notable, c)
		}
		for _, d := range c.deltas() {
			if d.notable() {
				if d.pct > 0 {
					increases++
				} else {
					decreases++
				}
			}
		}
	}
	slices.SortStableFunc(notable, func(x, y comparison) int { return compareFloat(y.time.pct, x.time.pct) })

	var b strings.Builder
	fmt.Fprintf(&b, "**%s, %s** across %s",
		plural(increases, "notable increase"), plural(decreases, "notable decrease"), plural(len(timeChanges), "compared benchmark"))
	if len(timeChanges) > 0 {
		fmt.Fprintf(&b, " · median time change %+.1f%%", median(timeChanges))
	}
	b.WriteString("\nObserved differences; runner conditions may explain them.\n\n")
	if len(notable) > 0 {
		b.WriteString("### Notable changes\n\n")
		writeTable(&b, notable, false)
		b.WriteString("\n")
	}
	if len(added) > 0 {
		fmt.Fprintf(&b, "**New:** %s\n\n", strings.Join(added, ", "))
	}
	if len(removed) > 0 {
		fmt.Fprintf(&b, "**Removed:** %s\n\n", strings.Join(removed, ", "))
	}
	fmt.Fprintf(&b, "<details><summary>All %d benchmarks</summary>\n\n", len(all))
	writeTable(&b, all, true)
	b.WriteByte('\n')
	fmt.Fprintf(&b, "Times are medians. Highlights require ≥10%% change and Holm-adjusted p<0.05 across %d comparisons (time, bytes, allocations). `~` means insufficient evidence of a difference or a change below 0.05%%; it does not prove unchanged performance. Small samples can limit detection. Statistical differences do not establish that the PR caused a change.\n", comparisons)
	b.WriteByte('\n')
	b.WriteString("</details>\n\n")
	_, err = io.WriteString(out, b.String())
	return err
}
