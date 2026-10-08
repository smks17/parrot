package commands

import (
	"errors"
	"fmt"
	"strconv"
	"time"

	"parrot/internal/engine/proc"
)

type Sleep struct{}

var _ Command = Sleep{}

func (s Sleep) Name() string { return "sleep" }

func (s Sleep) Usage() string { return "sleep SECONDS  — wait for the given time" }

func (s Sleep) Run(ctx *Context, args []string) int {
	if len(args) != 1 {
		fmt.Fprintln(ctx.Stderr, "sleep: usage: sleep SECONDS")
		return 1
	}
	sleepTime, err := parseDuration(args[0])
	if err != nil {
		fmt.Fprintf(ctx.Stderr, "sleep: invalid time interval %q\n", args[0])
		return 1
	}
	if err := ctx.Proc.Sleep(ctx.Self, sleepTime); err != nil {
		var sig proc.SignalError
		if errors.As(err, &sig) {
			return 128 + int(sig) // 130 for Ctrl-C, 143 for kill
		}
		return 1
	}
	return 0
}

// parseDuration reads seconds the way sleep does: 5, 0.5, or with a suffix
// s, m, h or d.
func parseDuration(s string) (time.Duration, error) {
	unit := time.Second
	if n := len(s); n > 0 {
		switch s[n-1] {
		case 's':
			s = s[:n-1]
		case 'm':
			unit, s = time.Minute, s[:n-1]
		case 'h':
			unit, s = time.Hour, s[:n-1]
		case 'd':
			unit, s = 24*time.Hour, s[:n-1]
		}
	}
	n, err := strconv.ParseFloat(s, 64)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("invalid time interval %q", s)
	}
	return time.Duration(n * float64(unit)), nil
}

func init() { Register(Sleep{}) }
