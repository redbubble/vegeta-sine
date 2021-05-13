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
	period    time.Duration
	mean      int
	amplitude int
	startAt   float64
	url       string
	duration  time.Duration
	timeout   time.Duration
	keepalive bool
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
	flag.DurationVar(&opts.period, "period", 10*time.Minute, "Period of the sine wave")
	flag.IntVar(&opts.mean, "mean", 2, "The Mean req/1s of the sine wave")
	flag.IntVar(&opts.amplitude, "amplitude", 1, "The Amplitude in req/1s of the sine wave")
	flag.Float64Var(&opts.startAt, "startAt", 0, "The phase at which to start the sine wave, in radians")
	flag.DurationVar(&opts.duration, "duration", 0, "Duration of the test in seconds")
	flag.DurationVar(&opts.timeout, "timeout", vegeta.DefaultTimeout, "Requests timeout")
	flag.BoolVar(&opts.keepalive, "keepalive", true, "Use persistent connections")
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

	var pacer vegeta.SinePacer
	pacer = vegeta.SinePacer{
		Period: opts.period,
		// The mid-point of the sine wave in freq-per-Duration,
		// MUST BE > 0
		Mean: vegeta.Rate{
			Freq: opts.mean,
			Per:  time.Second},
		// The amplitude of the sine wave in freq-per-Duration,
		// MUST NOT BE EQUAL TO OR LARGER THAN MEAN
		Amp: vegeta.Rate{
			Freq: opts.amplitude,
			Per:  time.Second},
		StartAt: opts.startAt,
	}

	fmt.Fprintf(os.Stderr, "Using pacer: %v\n", pacer)
	if invalid(pacer) {
		msg := fmt.Errorf("Sorry, your Sine pacer config is invalid. Mean must be positive, Amplitude must not be larger than Mean.")
		log.Fatal(msg)
	}

	duration := opts.duration * time.Second

	fmt.Fprintf(os.Stderr, "🚀  Starting sine load test against %q for %v\n", opts.url, round(duration))

	targeter := vegeta.NewStaticTargeter(vegeta.Target{
		Method: "GET",
		URL:    opts.url,
	})
	attacker := vegeta.NewAttacker(
		vegeta.KeepAlive(opts.keepalive),
		vegeta.Timeout(opts.timeout),
	)
	enc := vegeta.NewEncoder(os.Stdout)
	var metrics vegeta.Metrics
	startedAt := time.Now()

	for res := range attacker.Attack(targeter, pacer, duration, "test name") {
		metrics.Add(res)
		if err = enc.Encode(res); err != nil {
			msg := fmt.Errorf("error during attack: %s", err)
			log.Fatal(msg)
		}
	}

	metrics.Close()

	reporter := vegeta.NewTextReporter(&metrics)
	reporter.Report(os.Stdout)

	attackDuration := time.Since(startedAt)
	fmt.Fprintf(os.Stderr, "✨  Variable load test against %q completed in %v\n", opts.url, round(attackDuration))
}
