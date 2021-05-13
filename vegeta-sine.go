package main

import (
	"flag"
	"fmt"
	"log"
	"net/url"
	"os"
	"time"

	vegeta "github.com/tsenart/vegeta/lib"
)

// Rounding support lifted from Vegeta reporters since it is private.
var durations = [...]time.Duration{
	time.Hour,
	time.Minute,
	time.Second,
	time.Millisecond,
	time.Microsecond,
	time.Nanosecond,
}

// round to the next most precise unit.
func round(d time.Duration) time.Duration {
	for i, unit := range durations {
		if d >= unit && i < len(durations)-1 {
			return d.Round(durations[i+1])
		}
	}
	return d
}

// paceOpts aggregates the pacing command line options
type paceOpts struct {
	url      string
	pacer    string
	pacing   string
	duration time.Duration
}

// hitsPerNs returns the attack rate this ConstantPacer represents, in
// fractional hits per nanosecond.
func hitsPerNs(cp vegeta.ConstantPacer) float64 {
	return float64(cp.Freq) / float64(cp.Per)
}

func invalid(sp vegeta.SinePacer) bool {
	return sp.Period <= 0 || hitsPerNs(sp.Mean) <= 0 || hitsPerNs(sp.Amp) >= hitsPerNs(sp.Mean)
}

func main() {
	// Parse the commandline options
	opts := paceOpts{}
	flag.StringVar(&opts.url, "url", "http://localhost:8080/", "The URL to attack")
	flag.StringVar(&opts.pacing, "pacing", "", "String describing the pace")
	flag.DurationVar(&opts.duration, "duration", 0, "Duration of the test in seconds.")
	flag.Parse()

	if len(os.Args) == 1 {
		flag.Usage()
		os.Exit(1)
	}

	_, err := url.ParseRequestURI(opts.url)
	if err != nil {
		msg := fmt.Errorf("invalid URL %q: %s", opts.url, err)
		log.Fatal(msg)
	}

	if opts.pacing != "" {
		msg := fmt.Errorf("sorry, for now pacing is hard-coded")
		log.Fatal(msg)
	}

	var pacer vegeta.SinePacer
	pacer = vegeta.SinePacer{
		Period: 5 * time.Minute,
		// The mid-point of the sine wave in freq-per-Duration,
		// MUST BE > 0
		Mean: vegeta.Rate{
			Freq: 2,
			Per:  time.Second},
		// The amplitude of the sine wave in freq-per-Duration,
		// MUST NOT BE EQUAL TO OR LARGER THAN MEAN
		Amp: vegeta.Rate{
			Freq: 1,
			Per:  time.Second},
		StartAt: 0,
	}

	fmt.Printf("Using pacer: %v\n", pacer)
	if invalid(pacer) {
		msg := fmt.Errorf("sorry, that pacer is invalid.")
		log.Fatal(msg)
	}

	duration := opts.duration * time.Second

	fmt.Printf("🚀  Starting sine load test against %q for %v\n", opts.url, round(duration))

	targeter := vegeta.NewStaticTargeter(vegeta.Target{
		Method: "GET",
		URL:    opts.url,
	})
	attacker := vegeta.NewAttacker()
	var metrics vegeta.Metrics
	startedAt := time.Now()

	for res := range attacker.Attack(targeter, pacer, duration, "test name") {
		metrics.Add(res)
		// fmt.Printf("asdf, %v\n", res)
	}

	metrics.Close()

	reporter := vegeta.NewTextReporter(&metrics)
	reporter.Report(os.Stdout)

	attackDuration := time.Since(startedAt)
	fmt.Printf("✨  Variable load test against %q completed in %v\n", opts.url, round(attackDuration))
}
