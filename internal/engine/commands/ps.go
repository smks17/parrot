package commands

import (
	"fmt"
	"time"
)

type Ps struct{}

var _ Command = Ps{}

func (c Ps) Name() string { return "ps" }

func (c Ps) Usage() string { return "ps [-e]  — list processes" }

func (c Ps) Run(ctx *Context, args []string) int {
	all := false
	for _, arg := range args {
		switch arg {
		case "-e", "-A", "-ef", "aux":
			all = true
		default:
			fmt.Fprintf(ctx.Stderr, "ps: unsupported option %q\n", arg)
			return 1
		}
	}

	fmt.Fprintf(ctx.Stdout, "%5s %5s %-4s %8s %s\n", "PID", "PPID", "STAT", "TIME", "CMD")
	for _, info := range ctx.Proc.List() {
		// Without -e, only the caller's own processes, as ps does for its tty.
		if !all && info.User.UID != ctx.User.UID {
			continue
		}
		fmt.Fprintf(ctx.Stdout, "%5d %5d %-4c %8s %s\n",
			info.PID, info.PPID, info.State.Letter(), clockTime(info.CPU), info.Cmdline())
	}
	return 0
}

// clockTime is a CPU time as ps prints it: HH:MM:SS.
func clockTime(d time.Duration) string {
	s := int(d.Seconds())
	return fmt.Sprintf("%02d:%02d:%02d", s/3600, s/60%60, s%60)
}

func init() { Register(Ps{}) }
