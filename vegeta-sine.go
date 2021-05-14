package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	vegeta "github.com/tsenart/vegeta/v12/lib"
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
	flag.DurationVar(&opts.period, "period", 10*time.Minute, "Period of the sine wave")
	flag.IntVar(&opts.mean, "mean", 2, "The Mean req/1s of the sine wave")
	flag.IntVar(&opts.amplitude, "amplitude", 1, "The Amplitude in req/1s of the sine wave")
	flag.Float64Var(&opts.startAt, "startAt", 0, "The phase at which to start the sine wave, in radians")
	flag.DurationVar(&opts.duration, "duration", 0, "Duration of the test in seconds")
	flag.DurationVar(&opts.timeout, "timeout", vegeta.DefaultTimeout, "Requests timeout")
	flag.BoolVar(&opts.keepalive, "keepalive", true, "Use persistent connections")
	flag.Parse()

	fmt.Fprintf(os.Stderr, "Options: %#v\n", opts)

	// These values are well-described at
	// https://github.com/tsenart/vegeta/blob/d73edf2bc2663d83848da2a97a8401a7ed1440bc/lib/pacer.go#L101-L132
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

	var duration_text string
	if opts.duration == 0 {
		duration_text = "infinity"
	} else {
		duration_text = fmt.Sprintf("%v", round(opts.duration))
	}

	targeter := vegeta.NewJSONTargeter(os.Stdin, []byte{}, http.Header{})

	// Let's check if there's anything on os.Stdin - otherwise it'll
	// just hang, waiting for an EOF.
	stat, _ := os.Stdin.Stat()
	if (stat.Mode() & os.ModeCharDevice) != 0 {
		msg := fmt.Errorf("Please provide targets on /dev/stdin, in JSON format.")
		log.Fatal(msg)
	}

	// Eagerly read all targets from os.Stdin.
	targets, err := vegeta.ReadAllTargets(targeter)
	if err != nil {
		msg := fmt.Errorf("Couldn't figure out JSON targets from /dev/stdin: %s", err)
		log.Fatal(msg)
	}
	targeter = vegeta.NewStaticTargeter(targets...)

	attacker := vegeta.NewAttacker(
		vegeta.KeepAlive(opts.keepalive),
		vegeta.Timeout(opts.timeout),
	)
	enc := vegeta.NewEncoder(os.Stdout)
	var metrics vegeta.Metrics
	fmt.Fprintf(os.Stderr, "🚀  Starting sine load test for %s\n", duration_text)
	startedAt := time.Now()

	for res := range attacker.Attack(targeter, pacer, opts.duration, "sine load") {
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
	fmt.Fprintf(os.Stderr, "✨  Variable load test completed in %v\n", round(attackDuration))
}
