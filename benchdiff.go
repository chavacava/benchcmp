// Copyright 2014 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"flag"
	"fmt"
	"os"
	"sort"
	"strconv"
	"text/tabwriter"

	"golang.org/x/tools/benchmark/parse"
)

var (
	changedOnly = flag.Bool("changed", false, "show only benchmarks that have changed")
	magSort     = flag.Bool("mag", false, "sort benchmarks by magnitude of change")
	best        = flag.Bool("best", false, "compare best times from old and new")
	failOnDelta = flag.Bool("errdelta", false, "return error if there are delta")
	tNsPerOp    = flag.Float64("tnsop", 0.0, "tolerance for deltas of ns/op")
	tMbPerS     = flag.Float64("tmbs", 0.0, "tolerance for deltas of Mb/s")
	tAllPerOp   = flag.Float64("tallocop", 0.0, "tolerance for deltas of allocs/op")
	tBPerOp     = flag.Float64("tbop", 0.0, "tolerance for deltas of bytes/op")
)

const usageFooter = `
Each input file should be from:
	go test -run=NONE -bench=. > [old,new].txt

benchdiff compares old and new for each benchmark.

If -test.benchmem=true is added to the "go test" command
benchdiff will also compare memory allocations.
`

func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: %s old.txt new.txt\n\n", os.Args[0])
		flag.PrintDefaults()
		fmt.Fprint(os.Stderr, usageFooter)
		os.Exit(2)
	}
	flag.Parse()
	if flag.NArg() != 2 {
		flag.Usage()
	}

	if !*failOnDelta && (*tAllPerOp+*tBPerOp+*tMbPerS+*tNsPerOp) > 0 {
		fmt.Fprint(os.Stderr, "tolerances flags are only valid when -errdelta is true\n")
		os.Exit(2)
	}
	before := parseFile(flag.Arg(0))
	after := parseFile(flag.Arg(1))

	diffs, warnings := Correlate(before, after)

	for _, warn := range warnings {
		fmt.Fprintln(os.Stderr, warn)
	}

	if len(diffs) == 0 {
		fatal("benchdiff: no repeated benchmarks")
	}

	w := new(tabwriter.Writer)
	w.Init(os.Stdout, 0, 0, 5, ' ', 0)
	defer w.Flush()

	var header bool // Has the header has been displayed yet for a given block?

	if *magSort {
		sort.Sort(ByDeltaNsPerOp(diffs))
	} else {
		sort.Sort(ByParseOrder(diffs))
	}
	for _, diff := range diffs {
		if !diff.Measured(parse.NsPerOp) {
			continue
		}
		if delta := diff.DeltaNsPerOp(); !*changedOnly || delta.Changed() {
			if !header {
				fmt.Fprint(w, "benchmark\told ns/op\tnew ns/op\tdelta\n")
				header = true
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", diff.Name(), formatNs(diff.Before.NsPerOp), formatNs(diff.After.NsPerOp), delta.PercentAsStr())

			if *failOnDelta && delta.Percent() > *tNsPerOp {
				w.Flush()
				fatal(fmt.Sprintf("benchdiff: %s ns/op delta between benchmarks", delta.PercentAsStr()))
			}
		}
	}

	header = false
	if *magSort {
		sort.Sort(ByDeltaMBPerS(diffs))
	}
	for _, diff := range diffs {
		if !diff.Measured(parse.MBPerS) {
			continue
		}
		if delta := diff.DeltaMBPerS(); !*changedOnly || delta.Changed() {
			if !header {
				fmt.Fprint(w, "\nbenchmark\told MB/s\tnew MB/s\tspeedup\n")
				header = true
			}
			fmt.Fprintf(w, "%s\t%.2f\t%.2f\t%s\n", diff.Name(), diff.Before.MBPerS, diff.After.MBPerS, delta.Multiple())

			if *failOnDelta && delta.Percent() > *tMbPerS {
				w.Flush()
				fatal(fmt.Sprintf("benchdiff: %s Mb/s delta between benchmarks", delta.PercentAsStr()))
			}
		}
	}

	header = false
	if *magSort {
		sort.Sort(ByDeltaAllocsPerOp(diffs))
	}
	for _, diff := range diffs {
		if !diff.Measured(parse.AllocsPerOp) {
			continue
		}
		if delta := diff.DeltaAllocsPerOp(); !*changedOnly || delta.Changed() {
			if !header {
				fmt.Fprint(w, "\nbenchmark\told allocs\tnew allocs\tdelta\n")
				header = true
			}
			fmt.Fprintf(w, "%s\t%d\t%d\t%s\n", diff.Name(), diff.Before.AllocsPerOp, diff.After.AllocsPerOp, delta.PercentAsStr())

			if *failOnDelta && delta.Percent() > *tAllPerOp {
				w.Flush()
				fatal(fmt.Sprintf("benchdiff: %s allocs/op delta between benchmarks", delta.PercentAsStr()))
			}
		}
	}

	header = false
	if *magSort {
		sort.Sort(ByDeltaAllocedBytesPerOp(diffs))
	}
	for _, diff := range diffs {
		if !diff.Measured(parse.AllocedBytesPerOp) {
			continue
		}
		if delta := diff.DeltaAllocedBytesPerOp(); !*changedOnly || delta.Changed() {
			if !header {
				fmt.Fprint(w, "\nbenchmark\told bytes\tnew bytes\tdelta\n")
				header = true
			}
			fmt.Fprintf(w, "%s\t%d\t%d\t%s\n", diff.Name(), diff.Before.AllocedBytesPerOp, diff.After.AllocedBytesPerOp, diff.DeltaAllocedBytesPerOp().PercentAsStr())

			if *failOnDelta && delta.Percent() > *tBPerOp {
				w.Flush()
				fatal(fmt.Sprintf("benchdiff: %s bytes/op delta between benchmarks", delta.PercentAsStr()))
			}
		}
	}
}

func fatal(msg interface{}) {
	fmt.Fprintln(os.Stderr, msg)
	os.Exit(1)
}

func parseFile(path string) parse.Set {
	f, err := os.Open(path)
	if err != nil {
		fatal(err)
	}
	defer f.Close()
	bb, err := parse.ParseSet(f)
	if err != nil {
		fatal(err)
	}
	if *best {
		selectBest(bb)
	}
	return bb
}

func selectBest(bs parse.Set) {
	for name, bb := range bs {
		if len(bb) < 2 {
			continue
		}
		ord := bb[0].Ord
		best := bb[0]
		for _, b := range bb {
			if b.NsPerOp < best.NsPerOp {
				b.Ord = ord
				best = b
			}
		}
		bs[name] = []*parse.Benchmark{best}
	}
}

// formatNs formats ns measurements to expose a useful amount of
// precision. It mirrors the ns precision logic of testing.B.
func formatNs(ns float64) string {
	prec := 0
	switch {
	case ns < 10:
		prec = 2
	case ns < 100:
		prec = 1
	}
	return strconv.FormatFloat(ns, 'f', prec, 64)
}
