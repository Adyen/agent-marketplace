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

type samples struct {
	time, bytes, allocs []float64
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
	slices.Sort(values)
	middle := len(values) / 2
	if len(values)%2 == 0 {
		return (values[middle-1] + values[middle]) / 2
	}
	return values[middle]
}

func metric(values []float64, scale float64, decimals int) string {
	if len(values) == 0 {
		return "—"
	}
	return fmt.Sprintf("%.*f", decimals, median(values)/scale)
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
	var table strings.Builder
	table.WriteString("| Action | Base ms/op | Head ms/op | Time change | Base → head B/op | Base → head allocs/op | Samples base/head |\n|---|---:|---:|---:|---:|---:|---:|\n")
	for _, key := range keys {
		before, after := base[key], head[key]
		change := "n/a"
		switch {
		case len(before.time) == 0:
			change = "new"
		case len(after.time) == 0:
			change = "removed"
		case median(before.time) != 0:
			change = fmt.Sprintf("%+.1f%%", 100*(median(after.time)/median(before.time)-1))
		}
		label := strings.NewReplacer("|", "&#124;", "<", "&lt;", ">", "&gt;").Replace(key)
		fmt.Fprintf(&table, "| %s | %s | %s | %s | %s → %s | %s → %s | %d/%d |\n", label,
			metric(before.time, 1e6, 3), metric(after.time, 1e6, 3), change,
			metric(before.bytes, 1, 0), metric(after.bytes, 1, 0),
			metric(before.allocs, 1, 0), metric(after.allocs, 1, 0), len(before.time), len(after.time))
	}
	table.WriteString("\nMedians of repeated samples; time changes are descriptive, not significance tests. See benchstat below for confidence intervals. New benchmarks have no base comparison.\n")
	_, err = io.WriteString(out, table.String())
	return err
}
