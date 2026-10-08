package commands

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"parrot/internal/engine/proc"
)

type Kill struct{}

var _ Command = Kill{}

func (c Kill) Name() string { return "kill" }

func (c Kill) Usage() string {
	return "kill [-s SIGNAL | -SIGNAL] PID...  |  kill -l  — send a signal to a process"
}

func (c Kill) Run(ctx *Context, args []string) int {
	sig := proc.SIGTERM
	// The signal is the one option, and only first: after it every "-N" is a
	// group, as in "kill -TERM -300".
	if len(args) > 0 && strings.HasPrefix(args[0], "-") && args[0] != "-" && args[0] != "--" {
		arg := args[0]
		switch {
		case arg == "-l":
			for _, s := range proc.Signals() {
				fmt.Fprintf(ctx.Stdout, "%d) SIG%s\n", int(s), s)
			}
			return 0
		case arg == "-s":
			if len(args) < 2 {
				fmt.Fprintln(ctx.Stderr, "kill: -s: option requires an argument")
				return 1
			}
			s, ok := proc.ParseSignal(args[1])
			if !ok {
				fmt.Fprintf(ctx.Stderr, "kill: %s: invalid signal specification\n", args[1])
				return 1
			}
			sig, args = s, args[2:]
		default:
			// -9, -KILL, -SIGKILL. A negative number is a group, not a signal.
			s, ok := proc.ParseSignal(arg[1:])
			if !ok {
				fmt.Fprintf(ctx.Stderr, "kill: %s: invalid signal specification\n", arg[1:])
				return 1
			}
			sig, args = s, args[1:]
		}
	}
	if len(args) > 0 && args[0] == "--" { // the end of options: what follows is PIDs, even -1
		args = args[1:]
	}
	if len(args) == 0 {
		fmt.Fprintln(ctx.Stderr, "kill: usage: kill [-s sigspec | -sigspec] pid ...")
		return 1
	}

	status := 0
	for _, arg := range args {
		if strings.HasPrefix(arg, "%") && ctx.JobProcessGroup != nil {
			// A job is its whole process group: every stage of a pipeline.
			pgid, err := ctx.JobProcessGroup(arg)
			if err != nil {
				fmt.Fprintf(ctx.Stderr, "kill: %v\n", err)
				status = 1
				continue
			}
			if err := ctx.Proc.KillGroup(ctx.Self, pgid, sig); err != nil {
				fmt.Fprintf(ctx.Stderr, "kill: %s - %s\n", arg, killMessage(err))
				status = 1
			}
			continue
		}
		pid, err := strconv.Atoi(arg)
		if err != nil {
			fmt.Fprintf(ctx.Stderr, "kill: %s: arguments must be process or job IDs\n", arg)
			status = 1
			continue
		}
		// The kill(2) convention: a negative number names a group, by the
		// ID of its leader.
		if pid < 0 {
			err = ctx.Proc.KillGroup(ctx.Self, proc.PID(-pid), sig)
		} else {
			err = ctx.Proc.KillProcess(ctx.Self, proc.PID(pid), sig)
		}
		if err != nil {
			fmt.Fprintf(ctx.Stderr, "kill: (%d) - %s\n", pid, killMessage(err))
			status = 1
		}
	}
	return status
}

// killMessage is the text bash prints for each way a kill can fail.
func killMessage(err error) string {
	switch {
	case errors.Is(err, proc.ErrNoProcess):
		return "No such process"
	case errors.Is(err, proc.ErrPermission):
		return "Operation not permitted"
	}
	return err.Error()
}

func init() { Register(Kill{}) }
