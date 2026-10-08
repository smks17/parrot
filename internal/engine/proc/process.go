package proc

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"parrot/internal/engine/user"
)

type PID int

type ProcessState int

const (
	Running              ProcessState = iota // on the CPU now
	Runnable                                 // ready, waiting in the run queue for the CPU
	InterruptSleep                           // waiting on something: a pipe, a timer, a child
	UninterruptibleSleep                     // waiting on a real program it started
	Stop                                     // stopped by a signal, until SIGCONT
	Zombie                                   // finished, waiting for its parent to collect it
	Idle                                     // a kernel thread with nothing to do
)

// Letter is the state as ps writes it. Running and Runnable are both R, the
// way Linux reports them: the difference is only who holds the CPU.
func (s ProcessState) Letter() byte { return "RRSDTZI"[s] }

func (s ProcessState) String() string {
	return [...]string{"running", "runnable", "sleeping", "disk sleep", "stopped", "zombie", "idle"}[s]
}

// Signal numbers are the Linux ones, so that "kill -9" means what it says.
type Signal int

const (
	SIGHUP   Signal = 1
	SIGINT   Signal = 2
	SIGQUIT  Signal = 3
	SIGABRT  Signal = 6
	SIGKILL  Signal = 9
	SIGUSR1  Signal = 10
	SIGUSR2  Signal = 12
	SIGPIPE  Signal = 13
	SIGALRM  Signal = 14
	SIGTERM  Signal = 15
	SIGCHLD  Signal = 17
	SIGCONT  Signal = 18
	SIGSTOP  Signal = 19
	SIGTSTP  Signal = 20
	SIGTTIN  Signal = 21
	SIGTTOU  Signal = 22
	SIGURG   Signal = 23
	SIGWINCH Signal = 28
)

// signals is every signal this system knows: its name, what it does to a
// process that has not asked for it, and how a shell reports a death by it.
var signals = map[Signal]struct {
	name     string
	action   Action
	describe string
}{
	SIGHUP:   {"HUP", Terminate, "Hangup"},
	SIGINT:   {"INT", Terminate, "Interrupt"},
	SIGQUIT:  {"QUIT", Terminate, "Quit"},
	SIGABRT:  {"ABRT", Terminate, "Aborted"},
	SIGKILL:  {"KILL", Terminate, "Killed"},
	SIGUSR1:  {"USR1", Terminate, "User defined signal 1"},
	SIGUSR2:  {"USR2", Terminate, "User defined signal 2"},
	SIGPIPE:  {"PIPE", Terminate, "Broken pipe"},
	SIGALRM:  {"ALRM", Terminate, "Alarm clock"},
	SIGTERM:  {"TERM", Terminate, "Terminated"},
	SIGCHLD:  {"CHLD", Ignore, ""},
	SIGCONT:  {"CONT", Continue, ""},
	SIGSTOP:  {"STOP", StopAction, "Stopped (signal)"},
	SIGTSTP:  {"TSTP", StopAction, "Stopped"},
	SIGTTIN:  {"TTIN", StopAction, "Stopped (tty input)"},
	SIGTTOU:  {"TTOU", StopAction, "Stopped (tty output)"},
	SIGURG:   {"URG", Ignore, ""},
	SIGWINCH: {"WINCH", Ignore, ""},
}

// Signals lists the signals this system knows, in number order.
func Signals() []Signal {
	var all []Signal
	for n := Signal(1); n <= 31; n++ {
		if _, ok := signals[n]; ok {
			all = append(all, n)
		}
	}
	return all
}

func (s Signal) String() string {
	if sig, ok := signals[s]; ok {
		return sig.name
	}
	return strconv.Itoa(int(s))
}

// ParseSignal reads a signal the ways kill accepts one: 9, KILL, SIGKILL, kill.
func ParseSignal(text string) (Signal, bool) {
	if n, err := strconv.Atoi(text); err == nil {
		_, known := signals[Signal(n)]
		return Signal(n), known || n == 0
	}
	name := strings.TrimPrefix(strings.ToUpper(text), "SIG")
	for sig, known := range signals {
		if known.name == name {
			return sig, true
		}
	}
	return 0, false
}

// Describe is how a shell reports a job that a signal ended.
func (s Signal) Describe() string {
	if sig, ok := signals[s]; ok && sig.describe != "" {
		return sig.describe
	}
	return "Signal " + s.String()
}

// Catchable reports whether a process may handle the signal itself.
func (s Signal) Catchable() bool { return s != SIGKILL && s != SIGSTOP }

// Action is what a signal does to a process that has not asked otherwise.
type Action int

const (
	Terminate Action = iota
	Ignore
	StopAction
	Continue
)

func DefaultAction(sig Signal) Action {
	if s, ok := signals[sig]; ok {
		return s.action
	}
	return Terminate
}

// SignalError is the cause a fatal signal cancels a process's context with,
// so that context.Cause says which signal it was.
type SignalError Signal

func (e SignalError) Error() string { return "killed by SIG" + Signal(e).String() }

var (
	ErrNoProcess  = errors.New("no such process")
	ErrPermission = errors.New("operation not permitted")
)

type Process struct {
	PID   PID
	PPID  PID
	PGID  PID
	Start time.Time

	table *Table
	group *Group

	// guarded by table.mu
	user             user.Identity
	name             string
	args             []string
	state            ProcessState
	stateBeforeStop  ProcessState // what it goes back to on SIGCONT
	exitCode         int
	lastSignal       Signal
	attached         bool          // runs on a goroutine the table does not own
	cpuTime          time.Duration // time spent holding the CPU
	onCPUSince       time.Time     // when it last got the CPU
	holdsCPU         bool          // owns a CPU from the scheduler right now
	signalForwarders []func(Signal)
	caughtSignals    map[Signal]chan<- Signal // signals the command handles itself

	ctx    context.Context // cancelled by a fatal signal; context.Cause says which
	cancel context.CancelCauseFunc
}

// Info is a copy of what a process looks like right now, for ps and /proc.
type Info struct {
	PID, PPID, PGID PID
	User            user.Identity
	Name            string
	Args            []string
	State           ProcessState
	Start           time.Time
	CPU             time.Duration
	Exit            int
	LastSignal      Signal
}

// Cmdline is the command as it was run.
func (i Info) Cmdline() string {
	return strings.TrimSpace(i.Name + " " + strings.Join(i.Args, " "))
}

func (p *Process) infoLocked() Info {
	cpu := p.cpuTime
	if p.state == Running {
		cpu += time.Since(p.onCPUSince)
	}
	return Info{
		PID: p.PID, PPID: p.PPID, PGID: p.PGID, User: p.user, Name: p.name,
		Args: p.args, State: p.state, Start: p.Start, CPU: cpu, Exit: p.exitCode,
		LastSignal: p.lastSignal,
	}
}

func (p *Process) Info() Info {
	p.table.mu.Lock()
	defer p.table.mu.Unlock()
	return p.infoLocked()
}

func (p *Process) String() string { return fmt.Sprintf("%d(%s)", p.PID, p.Info().Name) }

// Context is the process's context: hand it to anything that should stop
// when the process is killed.
func (p *Process) Context() context.Context {
	if p == nil {
		return context.Background()
	}
	return p.ctx
}

// Attached reports whether the process runs on a goroutine of its own caller
func (p *Process) Attached() bool { return p == nil || p.attached }

// Exec replaces what the process is running, the way exec does: same PID,
// new name. A subshell made to run one command becomes that command.
func (p *Process) SetExec(name string, args []string) {
	p.table.mu.Lock()
	defer p.table.mu.Unlock()
	p.name, p.args = name, args
}

func (p *Process) SetUser(id user.Identity) {
	p.table.mu.Lock()
	defer p.table.mu.Unlock()
	p.user = id
}

// Killed reports the signal that is ending the process, or 0.
func (p *Process) Killed() Signal {
	if p == nil || p.ctx.Err() == nil {
		return 0
	}
	var sig SignalError
	if errors.As(context.Cause(p.ctx), &sig) {
		return Signal(sig)
	}
	return SIGINT // the shell's own context was cancelled: Ctrl-C
}

func (p *Process) OnSignal(forward func(Signal)) (stop func()) {
	if p == nil || p.table == nil {
		return func() {}
	}
	t := p.table
	t.mu.Lock()
	defer t.mu.Unlock()
	p.signalForwarders = append(p.signalForwarders, forward)
	n := len(p.signalForwarders) - 1
	return func() {
		t.mu.Lock()
		defer t.mu.Unlock()
		p.signalForwarders[n] = nil
	}
}

// Notify makes the process handle sigs itself
func (p *Process) Notify(c chan<- Signal, sigs ...Signal) {
	p.table.mu.Lock()
	defer p.table.mu.Unlock()
	if p.caughtSignals == nil {
		p.caughtSignals = make(map[Signal]chan<- Signal)
	}
	for _, sig := range sigs {
		if sig.Catchable() {
			p.caughtSignals[sig] = c
		}
	}
}

// StopNotify undoes Notify: the signals sent to c take their default action
// again.
func (p *Process) StopNotify(c chan<- Signal) {
	p.table.mu.Lock()
	defer p.table.mu.Unlock()
	if p.caughtSignals != nil {
		for key, sig := range p.caughtSignals {
			if sig == c {
				delete(p.caughtSignals, key)
			}
		}
	}
}
