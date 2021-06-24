# vegeta-sine

For those times when you want to hit a service hard, and then you don't, and then you do.  Et
cetera.  Useful for seeing if autoscaling is working as it should.

Here's a picture of the requests per second as two instances of `vegeta-sine` hit a service with
mean and amplitude of 15±5/s and 10±5/s respectively.

![Example of attack load varying as a sine curve](/img/load-graph.png)

## Building

```
go build
```

Or if you want to be sure it'll work on Linux or indeed inside a Docker container, you might use:

```
CGO_ENABLED=0 GOOS=linux go build -a -ldflags '-s'
```

## Running

Vegeta-sine expects the list of targets in JSON format, via stdin.  The documentation for this
format can be found at the Vegeta repository: https://github.com/tsenart/vegeta#json-format. Note
that it's JSON lines, not an array or whatever.

You'll want to decide the parameters of the sine wave you want to hit the targets with.  The best
description appears to be here:
https://github.com/tsenart/vegeta/blob/d73edf2bc2663d83848da2a97a8401a7ed1440bc/lib/pacer.go#L101-L132

The short version is that `vegeta-sine` will vary the number of requests per second in the shape of
a sine wave, in the range `mean ± amplitude`.  Therefore, remember to make amplitude positive, and
smaller than the mean (because a negative number of requests a second, well, that'd be weird).  The
`period` parameter defines the "width" of the sine wave, or how frequently it oscillates.  Finally,
the `startAt` parameter is useful if you know exactly the phase offset you want to start the
oscillator at.

```ShellSession
$ ./vegeta-sine --help
Usage of ./vegeta-sine:
  -amplitude int
    	The Amplitude in req/1s of the sine wave (default 1)
  -duration duration
    	Duration of the test in seconds
  -keepalive
    	Use persistent connections (default true)
  -mean int
    	The Mean req/1s of the sine wave (default 2)
  -period duration
    	Period of the sine wave (default 10m0s)
  -startAt float
    	The phase at which to start the sine wave, in radians
  -timeout duration
    	Requests timeout (default 30s)
```

Note that `duration` is a thing Golang will [handle for
you](https://golang.org/pkg/time/#ParseDuration), so you can say things like `1m` or `1h`.

By default `vegeta-sine` will attack indefinitely, to avoid that you can pass `-duration`.

## A real-life example

Over in
[`haproxy-experiment`](https://github.com/redbubble/haproxy-experiment/blob/2d99a743c0bccb50160b1a56eb714ef4edfafe39/bin/http#L53-L102)
you can see an example of how we used `vegeta-sine`.  The important bits, to give you an idea of how
to configure it:

```
vegeta-sine \
  -period="20m" \
  -mean="100" \
  -amplitude="50" \
  -startAt="${startAt}" \
  -keepalive=true \
  -timeout="12s" \
  < "${TARGETS_FILE}" \
  | vegeta encode \
  | jq ...
```

This will make `vegeta-sine` call the targets defined in `${TARGETS_FILE}` (containing JSON lines)
between 50 and 150 times per second.  To understand how to work with the output, head to the
[official documentation](https://github.com/tsenart/vegeta#encode-command).


## A note on keeping many `vegeta-sine` workers in sync

You may want to keep separate `vegeta-sine` clients in sync.  Here's a thing we used elsewhere:

```Bash
minutesAfterHour=$(date +"%M")
startAt=$(echo "scale=5; pi=4*a(1); (${minutesAfterHour}/60)*2*pi" | bc -l)
```

This makes `startAt`, the phase offset of the sine wave, be the same for any worker started at that
time.  Handy!

### Who?  Why?

This is an SRE@Redbubble tool used to work out whether we were able to autoscale based on saturation
metrics.
