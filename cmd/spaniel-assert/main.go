// spaniel-assert applies portable trace assertions to local captures or a collector.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/zfogg/spaniel/traceassert"
)

type stringsFlag []string

func (s *stringsFlag) String() string     { return fmt.Sprint([]string(*s)) }
func (s *stringsFlag) Set(v string) error { *s = append(*s, v); return nil }

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("spaniel-assert", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var inputs, ids stringsFlag
	var specPath, format, baseURL, findingsPath, tokenEnv string
	var wait, interval time.Duration
	fs.Var(&inputs, "input", "capture file (repeatable)")
	fs.Var(&ids, "trace-id", "trace to fetch (repeatable)")
	fs.StringVar(&specPath, "spec", "", "v1 JSON assertion specification")
	fs.StringVar(&format, "format", "json", "json, otlp-json or otlp-proto")
	fs.StringVar(&baseURL, "spaniel-url", "", "collector base URL")
	fs.StringVar(&findingsPath, "findings", "", "normalized lint findings JSON file")
	fs.StringVar(&tokenEnv, "token-env", "SPANIEL_API_TOKEN", "environment variable containing a bearer token (never a literal token)")
	fs.DurationVar(&wait, "wait", 0, "wait for live capture assertions to pass (maximum 10m)")
	fs.DurationVar(&interval, "interval", 250*time.Millisecond, "live capture polling interval (50ms to 5s)")
	if fs.Parse(args) != nil {
		return 2
	}
	if specPath == "" || len(inputs)+len(ids) == 0 && findingsPath == "" || len(ids) > 0 && baseURL == "" || fs.NArg() != 0 {
		fmt.Fprintln(stderr, "provide --spec and --input, --findings or --spaniel-url with --trace-id")
		return 2
	}
	if wait < 0 || wait > 10*time.Minute || interval < 50*time.Millisecond || interval > 5*time.Second || wait > 0 && len(ids) == 0 {
		fmt.Fprintln(stderr, "invalid live polling configuration")
		return 2
	}
	f, err := os.Open(specPath)
	if err != nil {
		fmt.Fprintln(stderr, "cannot open assertion specification")
		return 2
	}
	spec, err := traceassert.DecodeSpec(f)
	f.Close()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	var spans []traceassert.Span
	for _, path := range inputs {
		f, err := os.Open(path)
		if err != nil {
			fmt.Fprintln(stderr, "cannot open capture")
			return 2
		}
		rows, err := traceassert.Decode(f, format)
		f.Close()
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 2
		}
		spans = append(spans, rows...)
	}
	var findings []traceassert.Finding
	if findingsPath != "" {
		f, err := os.Open(findingsPath)
		if err != nil {
			fmt.Fprintln(stderr, "cannot open findings")
			return 2
		}
		findings, err = traceassert.DecodeFindings(f)
		f.Close()
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 2
		}
	}

	parsed, err := url.Parse(baseURL)
	if len(ids) > 0 && (err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "") {
		fmt.Fprintln(stderr, "invalid collector URL")
		return 2
	}
	ctx, cancel := context.WithTimeout(context.Background(), wait+30*time.Second)
	defer cancel()
	client := &http.Client{Timeout: 30 * time.Second, Transport: bearerTransport{base: http.DefaultTransport, token: strings.TrimSpace(os.Getenv(tokenEnv)), origin: parsed}, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 || len(via) > 0 && (req.URL.Scheme != via[0].URL.Scheme || req.URL.Host != via[0].URL.Host) {
			return fmt.Errorf("collector redirect rejected")
		}
		return nil
	}}
	deadline := time.Now().Add(wait)
	for {
		capture := append([]traceassert.Span{}, spans...)
		var fetchErr error
		for _, id := range ids {
			rows, err := traceassert.FetchTrace(ctx, client, baseURL, id)
			if err != nil {
				fetchErr = err
				break
			}
			capture = append(capture, rows...)
		}
		report := traceassert.CheckWithFindings(capture, findings, spec)
		if fetchErr == nil && report.OK() {
			if json.NewEncoder(stdout).Encode(report) != nil {
				return 2
			}
			return 0
		}
		if wait == 0 || !time.Now().Before(deadline) {
			if fetchErr != nil {
				fmt.Fprintln(stderr, fetchErr)
				return 2
			}
			if json.NewEncoder(stdout).Encode(report) != nil {
				return 2
			}
			return 1
		}
		remaining := time.Until(deadline)
		pause := interval
		if remaining < pause {
			pause = remaining
		}
		timer := time.NewTimer(pause)
		select {
		case <-ctx.Done():
			timer.Stop()
			fmt.Fprintln(stderr, "capture wait cancelled")
			return 2
		case <-timer.C:
		}
	}

}

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

// bearerTransport scopes credentials to the configured collector origin.
type bearerTransport struct {
	base   http.RoundTripper
	token  string
	origin *url.URL
}

func (t bearerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	copy := req.Clone(req.Context())
	if t.token != "" && t.origin != nil && req.URL.Scheme == t.origin.Scheme && req.URL.Host == t.origin.Host {
		copy.Header.Set("Authorization", "Bearer "+t.token)
	}
	return t.base.RoundTrip(copy)
}
