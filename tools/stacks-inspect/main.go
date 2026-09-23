// Command stacks-inspect captures bounded read-only protocol observations.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/signal"
	"regexp"
	"syscall"
	"time"

	bitcoin "github.com/cylewitruk-stacks/stacks-k8s/libs/bitcoin/rpc"
)

const maxBlocks = 128

// request selects one endpoint and bounded protocol reads; attribution is caller supplied.
type request struct {
	Endpoint       string              `json:"endpoint"`
	Credentials    bitcoin.Credentials `json:"credentials,omitempty"`
	Attribution    string              `json:"attribution"`
	TimeoutSeconds int                 `json:"timeoutSeconds"`
	Cycles         []uint64            `json:"cycles,omitempty"`
	Tip            string              `json:"tip,omitempty"`
	CompareTip     string              `json:"compareTip,omitempty"`
	Blocks         int                 `json:"blocks,omitempty"`
}

// record contains selected public facts without endpoint or credential material.
type record struct {
	Kind        recordKind `json:"kind"`
	Attribution string     `json:"callerAttribution"`
	StartedAt   time.Time  `json:"startedAt"`
	ObservedAt  time.Time  `json:"observedAt"`
	Subject     *subject   `json:"subject,omitempty"`
	Facts       any        `json:"facts,omitempty"`
	Error       string     `json:"error,omitempty"`
}

// subject identifies the requested read even when its result is unavailable.
type subject struct {
	BlockHash    string  `json:"blockHash,omitempty"`
	IndexBlockID string  `json:"indexBlockID,omitempty"`
	Cycle        *uint64 `json:"cycle,omitempty"`
}

// writer streams independently timestamped observations and retains output failures.
type writer struct {
	encoder     *json.Encoder
	attribution string
	err         error
}

func (w *writer) emit(kind recordKind, start time.Time, target *subject, facts any, err error) {
	r := record{
		Kind:        kind,
		Attribution: w.attribution,
		StartedAt:   start,
		ObservedAt:  time.Now().UTC(),
		Subject:     target,
		Facts:       facts,
	}
	if err != nil {
		r.Error = err.Error()
		r.Facts = nil
	}
	if w.err == nil {
		w.err = w.encoder.Encode(r)
	}
}

var (
	hashPattern        = regexp.MustCompile(`^[0-9a-f]{64}$`)
	attributionPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.:/-]{0,255}$`)
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err := run(ctx, os.Args[1:], os.Stdin, os.Stdout)
	cancel()
	if err != nil {
		fmt.Fprintln(os.Stderr, "stacks-inspect:", err)
		os.Exit(1)
	}
}

// run validates all inputs before creating a protocol client.
func run(ctx context.Context, args []string, input io.Reader, output io.Writer) error {
	if len(args) != 1 || (args[0] != "stacks" && args[0] != "bitcoin") {
		return errors.New("usage: stacks-inspect <stacks|bitcoin> < request.json")
	}
	var r request
	data, readErr := io.ReadAll(io.LimitReader(input, 65537))
	if readErr != nil || len(data) > 65536 {
		return errors.New("request exceeds byte bound or cannot be read")
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if d.Decode(&r) != nil || d.Decode(&struct{}{}) != io.EOF {
		return errors.New("expected one bounded request")
	}
	u, err := url.Parse(r.Endpoint)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" ||
		u.Fragment != "" ||
		(u.Path != "" && u.Path != "/") ||
		!attributionPattern.MatchString(r.Attribution) ||
		r.TimeoutSeconds < 1 ||
		r.TimeoutSeconds > 120 {
		return errors.New("invalid endpoint, attribution or deadline")
	}
	if args[0] == "stacks" {
		if len(r.Cycles) > 8 || r.Blocks != 0 || r.CompareTip != "" || r.Tip != "" || r.Credentials.Username != "" ||
			r.Credentials.Password != "" {
			return errors.New("invalid Stacks read selection")
		}
		for _, cycle := range r.Cycles {
			if cycle > 9999999999 {
				return errors.New("cycle exceeds bound")
			}
		}
	} else if r.Blocks < 1 || r.Blocks > maxBlocks || len(r.Cycles) != 0 ||
		(r.Tip != "" && !hashPattern.MatchString(r.Tip)) || (r.CompareTip != "" && !hashPattern.MatchString(r.CompareTip)) {
		return errors.New("invalid Bitcoin read selection")
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(r.TimeoutSeconds)*time.Second)
	defer cancel()
	w := &writer{encoder: json.NewEncoder(output), attribution: r.Attribution}
	start := time.Now().UTC()
	if args[0] == "stacks" {
		err = inspectStacks(ctx, r, w)
	} else {
		err = inspectBitcoin(ctx, r, w)
	}
	if err != nil {
		err = errors.Join(err, ctx.Err())
	}
	w.emit(recordCapture, start, nil, map[string]bool{"complete": err == nil && w.err == nil}, nil)
	return errors.Join(err, w.err)
}
