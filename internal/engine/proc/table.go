package proc

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"time"

	"parrot/internal/engine/clock"
	"parrot/internal/engine/user"
)

const (
	firstPID PID = 300 // where Linux starts again after wrapping; below it is taken
	maxPID   PID = 32768
)

// Table is every process in one session ans sub sessions
type Table struct {
	mu              sync.Mutex
	processes       map[PID]*Process
	groups          map[PID]*Group
	nextPID         PID
	changeBroadcast chan struct{} // closed and replaced whenever any process changes
	foreground      *Group        // the process group the terminal belongs to, 0 for none
	boot            time.Time
	cpuScheduler    *scheduler
}

func NewTable() *Table {
	t := &Table{
		processes:       map[PID]*Process{},
		nextPID:         firstPID,
		groups:          map[PID]*Group{},
		changeBroadcast: make(chan struct{}),
		boot:            clock.Now(),
		cpuScheduler:    newScheduler(virtualCPUs, defaultTimeSlice),
	}
	root := user.Identity{Name: user.RootName, UID: user.RootUID}
	t.attachLocked(1, 0, root, "init", InterruptSleep)
	t.attachLocked(2, 0, root, "kthreadd", Idle)
	return t
}

// waitUntilLocked blocks until cond holds, or ctx is cancelled. It is called, and
// returns, with t.mu held; it lets go of the lock while it waits, so whoever
// changes the table can get in and wake it.
func (t *Table) waitUntilLocked(ctx context.Context, cond func() bool) error {
	for !cond() {
		changed := t.changeBroadcast // taken under the lock, so no change can slip by
		t.mu.Unlock()
		select {
		case <-changed:
		case <-ctx.Done():
			t.mu.Lock()
			return context.Cause(ctx)
		}
		t.mu.Lock()
	}
	return nil
}

// Boot is when the system started, for uptime.
func (t *Table) Boot() time.Time { return t.boot }

// broadcastChangeLocked tells everyone waiting on the table that something changed.
func (t *Table) broadcastChangeLocked() {
	close(t.changeBroadcast)
	t.changeBroadcast = make(chan struct{})
}

func (t *Table) allocatePIDLocked() PID {
	for {
		pid := t.nextPID
		if t.nextPID++; t.nextPID >= maxPID {
			t.nextPID = firstPID
		}
		// A PID still naming a group is taken too, as on Linux: a new
		// process must not become the leader of someone else's job.
		_, isProcess := t.processes[pid]
		_, isGroup := t.groups[pid]
		if !isProcess && !isGroup {
			return pid
		}
	}
}

func newProcess(t *Table, parent context.Context, pid, ppid PID, id user.Identity, name string, args []string) *Process {
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithCancelCause(parent)
	return &Process{
		PID: pid, PPID: ppid, Start: clock.Now(), table: t,
		user: id, name: name, args: args,
		ctx: ctx, cancel: cancel,
	}
}

func (t *Table) attachLocked(pid, ppid PID, id user.Identity, name string, state ProcessState) *Process {
	p := newProcess(t, nil, pid, ppid, id, name, nil)
	p.attached, p.state = true, state
	t.processes[pid] = p
	t.NewGroup().joinLocked(p)
	return p
}

func (t *Table) Attach(parent PID, id user.Identity, name string) *Process {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.attachLocked(t.allocatePIDLocked(), parent, id, name, InterruptSleep)
}

// Spec is what Spawn needs to start a process.
type Spec struct {
	Parent *Process
	Group  *Group          // the process group to join; 0 starts a new one, led by this process
	Ctx    context.Context // cancelling it cancels the process too; nil for none
	User   user.Identity
	Name   string
	Args   []string
	Run    func(p *Process) int // the command; its return is the exit status
}

// Spawn starts a process. Run begins only once the runner gives it the CPU.
func (t *Table) Spawn(spec Spec) *Process {
	t.mu.Lock()
	ppid := PID(1)
	if spec.Parent != nil {
		ppid = spec.Parent.PID
	}
	p := newProcess(t, spec.Ctx, t.allocatePIDLocked(), ppid, spec.User, spec.Name, spec.Args)
	group := spec.Group
	if group == nil {
		group = t.NewGroup()
	}
	group.joinLocked(p)
	p.state = Runnable
	t.processes[p.PID] = p
	t.mu.Unlock()

	// The worker: the process's one goroutine. It ends when Run does.
	go func() {
		code := 0
		if p.acquireCPU() == nil {
			code = spec.Run(p)
		}
		p.releaseCPU(Runnable)
		t.recordExit(p, code)
	}()
	return p
}

// applySignalLocked does what a signal does to the process. It runs under the
// table's lock, which is what keeps two signals to one process in order.
func (t *Table) applySignalLocked(p *Process, sig Signal) {
	if p.state == Zombie {
		return // too late: a signal to a zombie changes nothing
	}
	p.lastSignal = sig
	for _, forward := range p.signalForwarders {
		if forward != nil {
			forward(sig)
		}
	}

	if c, ok := p.caughtSignals[sig]; ok && sig.Catchable() {
		deliverCaughtSignal(c, sig)
		return
	}
	switch DefaultAction(sig) {
	case Ignore:
	case StopAction:
		// The process sees it at its next checkpoint, and parks there.
		if p.state != Stop {
			p.stateBeforeStop, p.state = p.state, Stop
		}
	case Continue:
		if p.state == Stop {
			p.state = p.stateBeforeStop
		}
	case Terminate:
		p.cancel(SignalError(sig)) // ctx.Done() wakes every select the process is in, a stopped one too
		if p.state == Stop {
			p.state = p.stateBeforeStop
		}
		if sig == SIGKILL {
			// No waiting for the command to notice: SIGKILL cannot be put
			// off. The process is over now; its worker finds out later.
			t.makeZombieLocked(p, 128+int(SIGKILL))
		}
	}
	t.broadcastChangeLocked()
}

// deliverCaughtSignal hands a caught signal to the command that asked for it.
func deliverCaughtSignal(c chan<- Signal, sig Signal) {
	select {
	case c <- sig:

	default:
	}
}

// recordExit is the worker's status arriving
func (t *Table) recordExit(p *Process, code int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if sig := p.Killed(); sig != 0 {
		code = 128 + int(sig)
	}
	t.makeZombieLocked(p, code)
}

// makeZombieLocked ends a process
func (t *Table) makeZombieLocked(p *Process, status int) {
	if p.state == Zombie {
		return
	}
	p.state, p.exitCode = Zombie, status
	p.cancel(context.Canceled) // nothing of it should outlive it; a no-op if a signal got there first
	p.group.leaveLocked(p)
	for _, child := range t.processes {
		if child.PPID == p.PID {
			child.PPID = 1
			if child.state == Zombie {
				delete(t.processes, child.PID)
			}
		}
	}
	if parent, ok := t.processes[p.PPID]; !ok || parent.PID == 1 {
		delete(t.processes, p.PID) // init waits on everything handed to it
	} else {
		parent.lastSignal = SIGCHLD // ignored by default, but on the record
	}
	t.broadcastChangeLocked()
}

// Kill sends sig to target, or to every process in group
func (t *Table) Kill(sender *Process, target PID, sig Signal) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	var targets []*Process
	if target < 0 {
		if g, ok := t.groups[-target]; ok {
			targets = slices.Clone(g.members)
		}
	} else if p, ok := t.processes[target]; ok {
		targets = append(targets, p)
	}
	if len(targets) == 0 {
		return ErrNoProcess
	}

	var allowed []*Process
	for _, p := range targets {
		if sender == nil || sender.user.IsRoot() || sender.user.UID == p.user.UID {
			allowed = append(allowed, p)
		}
	}
	if len(allowed) == 0 {
		return ErrPermission
	}

	for _, p := range allowed {
		switch {
		case sig == 0:
		case p.attached:
			// A shell, or init: they ignore what would end them, the way an
			// interactive shell does.
			p.lastSignal = sig
		default:
			t.applySignalLocked(p, sig)
		}
	}
	return nil
}

// Waits for processes, which should be children of the caller.
func (t *Table) Wait(procs []*Process, stops bool) (status int, stopped bool) {
	if len(procs) == 0 {
		return 0, false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.waitUntilLocked(context.Background(), func() bool {
		done, _ := t.doneLocked(procs)
		return done || stops && stoppedLocked(procs)
	})
	if done, last := t.doneLocked(procs); done {
		return last.Exit, false
	}
	for _, p := range procs {
		if p.state == Stop {
			return 128 + int(p.lastSignal), true // 148 for Ctrl-Z, as in bash
		}
	}
	return 0, true // unreachable: waitUntilLocked returned, so all are done or stopped
}

// Done reports whether every one of procs has ended, and how the last did.
func (t *Table) Done(procs []*Process) (done bool, last Info) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.doneLocked(procs)
}

func (t *Table) doneLocked(procs []*Process) (done bool, last Info) {
	if len(procs) == 0 {
		return true, Info{}
	}
	for _, p := range procs {
		if p.state != Zombie {
			return false, Info{}
		}
	}
	return true, procs[len(procs)-1].infoLocked()
}

// Stopped reports whether every one of procs still alive is stopped.
func (t *Table) Stopped(procs []*Process) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return stoppedLocked(procs)
}

func stoppedLocked(procs []*Process) bool {
	alive := false
	for _, p := range procs {
		if p.state == Zombie {
			continue
		}
		alive = true
		if p.state != Stop {
			return false
		}
	}
	return alive
}

// Reap collects processes that have ended, removing them from the table.
func (t *Table) Reap(procs []*Process) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, p := range procs {
		if p.state == Zombie && t.processes[p.PID] == p {
			delete(t.processes, p.PID)
		}
	}
}

// List is every process, in PID order.
func (t *Table) List() []Info {
	t.mu.Lock()
	defer t.mu.Unlock()
	infos := make([]Info, 0, len(t.processes))
	for _, p := range t.processes {
		infos = append(infos, p.infoLocked())
	}
	slices.SortFunc(infos, func(a, b Info) int { return int(a.PID - b.PID) })
	return infos
}

func (t *Table) Get(pid PID) (Info, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	p, ok := t.processes[pid]
	if !ok {
		return Info{}, false
	}
	return p.infoLocked(), true
}

// SetForeground gives the terminal to a process group; 0 takes it back.
func (t *Table) SetForeground(group *Group) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.foreground = group
}

// Signal sends sig to the foreground group, as the terminal does for Ctrl-C
// and Ctrl-Z. It reports whether anyone was there to receive it.
func (t *Table) SignalForeground(sig Signal) bool {
	t.mu.Lock()
	group := t.foreground
	t.mu.Unlock()
	return group != nil && group.Kill(nil, sig) == nil
}

func (t *Table) String() string {
	var out string
	for _, info := range t.List() {
		out += fmt.Sprintf("%d %c %s\n", info.PID, info.State.Letter(), info.Cmdline())
	}
	return out
}
