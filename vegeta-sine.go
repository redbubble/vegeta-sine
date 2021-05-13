package main

import (
	"flag"
	"fmt"
	"log"
	"math"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	vegeta "github.com/tsenart/vegeta/lib"
)

// RateDescriptor describes a rate in requests per second and duration.
type RateDescriptor struct {
	Rate     uint          `json:"rate"`
	Duration time.Duration `json:"duration"`
}

func (rs RateDescriptor) String() string {
	return fmt.Sprintf("%v req/s for %v", rs.Rate, rs.Duration)
}

// AttackDescriptor describes an attack by name and series of rates.
type AttackDescriptor struct {
	Name  string           `json:"name"`
	Rates []RateDescriptor `json:"rates"`
}

// Duration returns the aggregate time in an attack by summing all the duration of the rates.
func (attack AttackDescriptor) Duration() time.Duration {
	duration := time.Second
	for _, rate := range attack.Rates {
		duration += rate.Duration
	}
	return duration
}

// StepFunctionPacer paces an attack with specific request rates for specific durations.
type StepFunctionPacer struct {
	Attack AttackDescriptor
}

func (vrp StepFunctionPacer) String() string {
	return fmt.Sprintf("Variable Rates{%s: %d rates}", vrp.Attack.Name, len(vrp.Attack.Rates))
}

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

// pacerState maintains state for a Vegeta pacer.
type pacerState struct {
	Rate    RateDescriptor
	Metrics vegeta.Metrics
}

// activePacerState maintains state for the actively executing Vegeta pacer
// This is only necessary because the `Pace` function is called by value
// rather than by reference.
var activePacerState pacerState

// Pace determines the length of time to sleep until the next hit is sent.
func (pacer StepFunctionPacer) Pace(elapsed time.Duration, hits uint64) (time.Duration, bool) {
	// Determine which Rate is active and accumulate and expected number of hits
	var activeRate RateDescriptor
	aggregateDuration := time.Second
	expectedHits := pacer.hits(elapsed)
	for _, rate := range pacer.Attack.Rates {
		aggregateDuration += rate.Duration
		if elapsed <= aggregateDuration {
			activeRate = rate
			break
		}
	}

	// Use the last rate if we didn't find one
	if activeRate == (RateDescriptor{}) {
		activeRate = pacer.Attack.Rates[len(pacer.Attack.Rates)-1]
		fmt.Printf("🔥  Setting default rate of %d req/sec for remainder of attack\n", activeRate.Rate)
	}

	// Report when the rate changes
	if activePacerState.Rate != activeRate {
		if activePacerState.Metrics.Requests > 0 {
			activePacerState.Metrics.Close()
			reporter := vegeta.NewTextReporter(&activePacerState.Metrics)
			reporter.Report(os.Stdout)
			activePacerState.Metrics = vegeta.Metrics{}
		}

		activePacerState.Rate = activeRate
		elapsedSummary := func() string {
			if uint64(elapsed.Seconds()) > 0 {
				return fmt.Sprintf(" (%v elapsed)", round(elapsed))
			}
			return ""
		}
		fmt.Printf("💥  Attacking at a rate of %v%s\n", activeRate, elapsedSummary())
	}

	// Calculate when to send the next hit based on the active rate
	if hits < uint64(expectedHits) {
		// Running behind, send next hit immediately.
		return 0, false
	}

	nsPerHit := math.Round(1 / pacer.hitsPerNs(activeRate))
	hitsToWait := float64(hits+1) - float64(expectedHits)
	nextHitIn := time.Duration(nsPerHit * hitsToWait)
	return nextHitIn, false
}

// TODO: Move to RateDescriptor type
func (pacer StepFunctionPacer) hitsPerNs(rate RateDescriptor) float64 {
	return float64(rate.Rate) / float64(time.Second)
}

// TODO: Move to RateDescriptor type
func (pacer StepFunctionPacer) hits(duration time.Duration) float64 {
	hits := float64(0)
	aggregateDuration := time.Second
	for _, rate := range pacer.Attack.Rates {
		hits += (float64(rate.Rate) * rate.Duration.Seconds())
		aggregateDuration += rate.Duration
		if duration <= aggregateDuration {
			break
		}
	}
	return hits
}

// paceOpts aggregates the pacing command line options
type paceOpts struct {
	url      string
	pacer    string
	pacing   string
	duration time.Duration
}

type dynamicPacer interface {
	vegeta.Pacer
	setAttack(attack AttackDescriptor)
	parsePacingStr(pacing string) []RateDescriptor
}

// parsePacingStr parses a string of the form "duration1@rate1, duration2@rate2"... into an array of rate descriptors
func (pacer StepFunctionPacer) parsePacingStr(pacing string) []RateDescriptor {
	var rates []RateDescriptor
	descriptors := strings.SplitN(pacing, ",", -1)
	for _, descriptor := range descriptors {
		components := strings.SplitN(descriptor, "@", 2)
		if components[0] == "" || components[1] == "" {
			msg := fmt.Errorf("invalid pacing descriptor %q", pacing)
			log.Fatal(msg)
		}

		duration, err := time.ParseDuration(strings.TrimSpace(components[0]))
		if err != nil {
			msg := fmt.Errorf("invalid pacing descriptor %q: %s", pacing, err)
			log.Fatal(msg)
		}
		rate, err := strconv.Atoi(strings.TrimSpace(components[1]))
		if err != nil {
			msg := fmt.Errorf("invalid pacing descriptor %q: %s", pacing, err)
			log.Fatal(msg)
		}
		rates = append(rates, RateDescriptor{
			Rate:     uint(rate),
			Duration: duration,
		})
	}
	return rates
}

func (pacer *StepFunctionPacer) setAttack(attack AttackDescriptor) {
	pacer.Attack = attack
}

func main() {
	// Parse the commandline options
	pacers := []string{"step-function", "sine"}
	opts := paceOpts{}
	flag.StringVar(&opts.url, "url", "http://localhost:8080/", "The URL to attack")
	flag.StringVar(&opts.pacer, "pacer", "",
		fmt.Sprintf("Pacer to use for governing load rate [%s]", strings.Join(pacers, ", ")))
	flag.StringVar(&opts.pacing, "pacing", "", "String describing the pace")
	flag.DurationVar(&opts.duration, "duration", 0, "Duration of the test.")
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

	if opts.pacing == "" {
		err := fmt.Errorf("-pacing must be provided")
		log.Fatal(err)
	}

	var pacer dynamicPacer
	switch opts.pacer {
	case "step-function":
		pacer = &StepFunctionPacer{}
	case "sine":
		err := fmt.Errorf("sine is not yet implemented")
		log.Fatal(err)
	default:
		err := fmt.Errorf("unknown pacer type: %q", opts.pacer)
		log.Fatal(err)
	}

	// Build the attack
	var attack AttackDescriptor
	attack.Name = "Variable Load Test"
	if opts.pacing != "" {
		attack.Rates = pacer.parsePacingStr(opts.pacing)
	}
	pacer.setAttack(attack)

	// Run the attack
	fmt.Printf("🚀  Starting variable load test against %q with %d load profiles for %v\n", opts.url, len(attack.Rates), round(attack.Duration()))
	targeter := vegeta.NewStaticTargeter(vegeta.Target{
		Method: "GET",
		URL:    opts.url,
	})
	attacker := vegeta.NewAttacker()
	startedAt := time.Now()
	for res := range attacker.Attack(targeter, pacer, attack.Duration(), attack.Name) {
		activePacerState.Metrics.Add(res)
	}

	activePacerState.Metrics.Close()

	reporter := vegeta.NewTextReporter(&activePacerState.Metrics)
	reporter.Report(os.Stdout)

	attackDuration := time.Since(startedAt)
	fmt.Printf("✨  Variable load test against %q completed in %v\n", opts.url, round(attackDuration))
}
