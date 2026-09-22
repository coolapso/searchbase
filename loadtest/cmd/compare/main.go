package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"

	"github.com/coolapso/searchbase/loadtest/internal/loadtest"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: compare report.json [...]")
		os.Exit(2)
	}
	var reports []loadtest.Report
	for _, path := range os.Args[1:] {
		b, err := os.ReadFile(path)
		if err != nil {
			panic(err)
		}
		var r loadtest.Report
		if err = json.Unmarshal(b, &r); err != nil {
			panic(err)
		}
		if r.SchemaVersion != loadtest.ReportSchemaVersion {
			panic("unsupported report schema")
		}
		reports = append(reports, r)
	}
	sort.Slice(reports, func(i, j int) bool { return reports[i].SafeCapacity > reports[j].SafeCapacity })
	fmt.Println("scenario\tinstance\tsafe_ops_s\tp95_ms\tcomparable")
	for _, r := range reports {
		fmt.Printf("%s\t%s\t%.2f\t%.2f\t%t\n", r.Scenario, r.InstanceLabel, r.SafeCapacity, r.Latency.P95MS, r.Comparable)
	}
}
