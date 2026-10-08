package proc

import (
	"context"
	"sync/atomic"
	"time"
)

const (
	// virtualCPUs is how many processes can run at once
	virtualCPUs      = 0
	defaultTimeSlice = 10 * time.Millisecond
)

type scheduler struct {
	cpuSlots        chan struct{}
	timeSlice       time.Duration
	contextSwitches atomic.Int64
	waitingForCPU   atomic.Int64
}

func newScheduler(cpus int, slice time.Duration) *scheduler {
	s := &scheduler{timeSlice: slice}
	if cpus > 0 {
		s.cpuSlots = make(chan struct{}, cpus)
	}
	return s
}

// waitForFreeCPU waits for a free CPU, or until ctx is cancelled.
func (s *scheduler) waitForFreeCPU(ctx context.Context) error {
	if s.cpuSlots == nil {
		return nil
	}
	s.waitingForCPU.Add(1)
	defer s.waitingForCPU.Add(-1)
	select {
	case s.cpuSlots <- struct{}{}:
		s.contextSwitches.Add(1)
		return nil
	case <-ctx.Done():
		return context.Cause(ctx)
	}
}

// freeCPU frees a CPU that waitForFreeCPU handed out.
func (s *scheduler) freeCPU() {
	if s.cpuSlots != nil {
		<-s.cpuSlots
	}
}

// shouldPreempt reports whether a process that got the CPU at since should
// let the next one run: its slice is over, and someone is waiting.
func (s *scheduler) shouldPreempt(since time.Time) bool {
	return s.waitingForCPU.Load() > 0 && time.Since(since) >= s.timeSlice
}

// acquireCPU waits until the process holds a CPU.
func (p *Process) acquireCPU() error {
	if err := p.table.cpuScheduler.waitForFreeCPU(p.ctx); err != nil {
		return err
	}
	p.table.markOnCPU(p)
	return nil
}

// releaseCPU gives up the CPU, billing the process for the time it held it.
func (p *Process) releaseCPU(state ProcessState) {
	if p.table.markOffCPU(p, state) {
		p.table.cpuScheduler.freeCPU()
	}
}

// checkStopAndPreemption is where a scheduled process meets the scheduler
func (p *Process) checkStopAndPreemption() error {
	if p.Attached() {
		return nil
	}
	if p.ctx.Err() != nil {
		return context.Cause(p.ctx)
	}
	t := p.table

	t.mu.Lock()
	stopped := p.state == Stop
	due := t.cpuScheduler.shouldPreempt(p.onCPUSince)
	t.mu.Unlock()

	if stopped {
		p.releaseCPU(Stop)
		t.mu.Lock()
		err := t.waitUntilLocked(p.ctx, func() bool { return p.state != Stop })
		t.mu.Unlock()
		if err != nil {
			return err
		}
		return p.acquireCPU()
	}
	if due {
		p.releaseCPU(Runnable)
		return p.acquireCPU() // to the back of the queue
	}
	return nil
}

// Yield is a checkpoint for a command that loops without reading or writing.
func (p *Process) Yield() error {
	if p == nil {
		return nil
	}
	return p.checkStopAndPreemption()
}

// sleepWhile runs do off the CPU: do is going to wait
func (p *Process) sleepWhile(state ProcessState, do func()) error {
	if p.Attached() {
		p.sleepWhileAttached(state, do)
		return nil
	}
	p.releaseCPU(state)
	do()
	return p.acquireCPU()
}

// sleepWhileAttached is sleepWhile for a shell
func (p *Process) sleepWhileAttached(state ProcessState, do func()) {
	if p == nil {
		do()
		return
	}
	t := p.table
	t.mu.Lock()
	before := p.state
	p.state = state
	t.mu.Unlock()

	do()

	t.mu.Lock()
	p.state = before
	t.mu.Unlock()
}

// markOnCPU marks p as holding the CPU and starts its clock.
func (t *Table) markOnCPU(p *Process) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if p.state != Zombie && p.state != Stop {
		p.state = Running
	}
	p.holdsCPU = true
	p.onCPUSince = time.Now()
}

// markOffCPU bills p for the time it held the CPU and puts it in state. It
// reports whether p was holding the CPU at all.
func (t *Table) markOffCPU(p *Process, state ProcessState) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !p.holdsCPU {
		return false
	}
	p.holdsCPU = false
	p.cpuTime += time.Since(p.onCPUSince)
	if p.state != Zombie && p.state != Stop {
		p.state = state
	}
	return true
}

// Switches is how many times the CPU has changed hands.
func (t *Table) Switches() int { return int(t.cpuScheduler.contextSwitches.Load()) }

// RunQueue is how many processes are waiting for the CPU, which is what a
// load average averages.
func (t *Table) RunQueue() int { return int(t.cpuScheduler.waitingForCPU.Load()) }
