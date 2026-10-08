package shell

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
	"sync"

	"parrot/internal/engine/commands"
	"parrot/internal/engine/filesystem"
	"parrot/internal/engine/proc"
)

// A job is what one command line, or one "&", runs.
type job struct {
	*proc.Group
	id         int  // its number, as in "[1] 304"; 0 for a foreground job
	foreground bool // it started with the terminal: Ctrl-C and Ctrl-Z reach its group

	mu    sync.Mutex
	procs []*proc.Process // every process it started, in the order they started

	started chan struct{}   // closed once the first process starts, or the job ends
	ended   context.Context // done once the job has ended
	end     context.CancelFunc
	status  int // how it ended; read only once ended is done
}

func (sh *Shell) newJob(id int, foreground bool) *job {
	ended, end := context.WithCancel(context.Background())
	return &job{Group: sh.procHandler.NewGroup(), id: id, foreground: foreground,
		started: make(chan struct{}), ended: ended, end: end}
}

func (j *job) add(p *proc.Process) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.procs = append(j.procs, p)
}

func (j *job) processes() []*proc.Process {
	j.mu.Lock()
	defer j.mu.Unlock()
	return slices.Clone(j.procs)
}

func (j *job) finish(status int) {
	j.status = status
	if j.PGID() == 0 { // it never started a process, so nothing closed started
		close(j.started)
	}
	j.end()
}

func (j *job) text() string {
	var commands []string
	for _, p := range j.processes() {
		commands = append(commands, p.Info().Cmdline())
	}
	return strings.Join(commands, " | ")
}

// inJob runs fn as a foreground job of its own, unless the shell is already
// running inside a job
func (sh *Shell) inJob(fds *filesystem.FDTable, fn func() int) int {
	if sh.job != nil {
		return fn()
	}
	j := sh.newJob(0, true)
	sh.job = j

	stopForwarding := context.AfterFunc(sh.ctx, func() {
		sh.procHandler.SignalForeground(proc.SIGINT)
	})
	defer func() {
		stopForwarding()
		sh.job = nil
		sh.procHandler.SetForeground(nil)
	}()
	status := fn()
	if sh.procHandler.Stopped(j.processes()) {
		sh.suspend(j, fds)
	}
	return status
}

func (sh *Shell) Suspend() { sh.procHandler.SignalForeground(proc.SIGTSTP) }

// suspend keeps a foreground job that Ctrl-Z stopped as one of the shell's
// jobs, so that fg and bg can carry on with it. Its watcher is the
// goroutine that waits for it to end, wherever it then runs.
func (sh *Shell) suspend(j *job, fds *filesystem.FDTable) {
	j.id = sh.nextJobID()
	sh.jobs = append(sh.jobs, j)
	procs := j.processes()
	go func() {
		status, _ := sh.procHandler.Wait(procs, false)
		sh.procHandler.Reap(procs)
		j.finish(status)
	}()
	// TODO: Error should not handle here
	fmt.Fprintf(fds.Stderr(), "\n[%d]+  %-24s%s\n", j.id, "Stopped", j.text())
}

// runProcess runs a command as a child process of the shell, in the current
// job, and waits for it, or, in the foreground, until it is stopped.
func (sh *Shell) runProcess(cmd commands.Command, name string, args []string, fds *filesystem.FDTable) int {
	return sh.inJob(fds, func() int {
		j := sh.job
		// A copy, so the command sees p as its process. It is taken here, on
		// the shell's goroutine, since the shell goes on changing after Spawn.
		child := *sh
		spec := proc.Spec{
			Parent: sh.proc, User: sh.fs.Identity(), Name: name, Args: args,
			Run: func(p *proc.Process) int {
				child.proc, child.ctx = p, p.Context()
				return cmd.Run(child.context(fds), args)
			},
		}

		p := j.Spawn(spec)
		j.add(p)
		sh.lastProcess = p
		if j.PGID() == p.PID { // the first: it leads the job's group
			if j.foreground {
				sh.procHandler.SetForeground(j.Group)
			}
			close(j.started)
		}
		if j.foreground && sh.ctx.Err() != nil {
			j.Kill(nil, proc.SIGINT)
		}

		// Killed while it waits on a pipe: closing the pipe wakes it. The
		// callback can run after runProcess returns, while the shell runs the
		// next line, so it takes copies rather than reading sh.
		shellCtx, pipes := sh.ctx, sh.pipes
		stopWatching := context.AfterFunc(p.Context(), func() {
			var sig proc.SignalError
			if errors.As(context.Cause(p.Context()), &sig) || shellCtx.Err() != nil {
				for _, end := range pipes {
					end.Close()
				}
			}
		})

		// Only a foreground job waits for a stop: a background one carries
		// on waiting, the way its subshell would.
		status, stopped := sh.procHandler.Wait([]*proc.Process{p}, j.foreground)
		if stopped {
			return status // 148; it is the shell's job now, and keeps its watcher
		}
		stopWatching()
		sh.procHandler.Reap([]*proc.Process{p})
		if sh.interrupted() {
			return sh.status
		}
		return status
	})
}

func (sh *Shell) runBackground(cmd Cmd, fds *filesystem.FDTable) int {
	j := sh.newJob(sh.nextJobID(), false)
	child := sh.sub()
	child.job, child.ctx = j, context.Background()
	sh.jobs = append(sh.jobs, j)
	go func() { j.finish(child.runCmd(cmd, fds)) }()

	select {
	case <-j.started:
	case <-sh.ctx.Done(): // a job of builtins alone may never start a process
	}
	sh.lastBG = j.PGID()
	if sh.lastBG != 0 { // 0: it ended without starting a process, as "x=1 &" does
		fmt.Fprintf(fds.Stderr(), "[%d] %d\n", j.id, sh.lastBG) // as bash: [job] pid
	}
	return sh.setStatus(0)
}

// nextJobID is one past the highest job number in use, as in bash.
func (sh *Shell) nextJobID() int {
	id := 0
	for _, j := range sh.jobs {
		if j.ended.Err() == nil {
			id = max(id, j.id)
		}
	}
	return id + 1
}

func (sh *Shell) findJob(jobReference string) (*job, error) {
	if len(sh.jobs) == 0 {
		return nil, fmt.Errorf("%s: no such job", jobReference)
	}
	switch jobReference {
	case "", "%", "%%", "%+":
		return sh.jobs[len(sh.jobs)-1], nil
	case "%-":
		if len(sh.jobs) < 2 {
			return nil, fmt.Errorf("%s: no such job", jobReference)
		}
		return sh.jobs[len(sh.jobs)-2], nil
	}
	if number, ok := strings.CutPrefix(jobReference, "%"); ok {
		id, err := strconv.Atoi(number)
		if i := slices.IndexFunc(sh.jobs, func(j *job) bool { return j.id == id }); err == nil && i >= 0 {
			return sh.jobs[i], nil
		}
		return nil, fmt.Errorf("%s: no such job", jobReference)
	}
	pid, err := strconv.Atoi(jobReference)
	if i := slices.IndexFunc(sh.jobs, func(j *job) bool { return int(j.PGID()) == pid }); err == nil && i >= 0 {
		return sh.jobs[i], nil
	}
	return nil, fmt.Errorf("pid %s is not a child of this shell", jobReference)
}

// jobProcessGroup is what kill makes of %1: the job's process group, to
// signal all of it at once.
func (sh *Shell) jobProcessGroup(jobReference string) (proc.PID, error) {
	j, err := sh.findJob(jobReference)
	if err != nil {
		return 0, err
	}
	if j.PGID() == 0 {
		return 0, fmt.Errorf("%s: no such job", jobReference)
	}
	return j.PGID(), nil
}

func (sh *Shell) forget(j *job) {
	sh.jobs = slices.DeleteFunc(sh.jobs, func(other *job) bool { return other == j })
}

// state is the job as jobs reports it.
func (sh *Shell) state(j *job) string {
	switch {
	case j.ended.Err() != nil && j.status == 0:
		return "Done"
	case j.ended.Err() != nil && j.status > 128:
		return proc.Signal(j.status - 128).Describe()
	case j.ended.Err() != nil:
		return fmt.Sprintf("Exit %d", j.status)
	case sh.procHandler.Stopped(j.processes()):
		return "Stopped"
	}
	return "Running"
}

// printJob writes one line of jobs: "[1]+  Running    sleep 5 &".
func (sh *Shell) printJob(w io.Writer, j *job) {
	mark := ' '
	if n := len(sh.jobs); n > 0 && sh.jobs[n-1] == j {
		mark = '+'
	} else if n > 1 && sh.jobs[n-2] == j {
		mark = '-'
	}
	state := sh.state(j)
	text := j.text()
	if state == "Running" {
		text += " &"
	}
	fmt.Fprintf(w, "[%d]%c  %-24s%s\n", j.id, mark, state, text)
}

// jobsBuiltin is "jobs". A job reported as ended is forgotten, as in bash.
func (sh *Shell) jobsBuiltin(fds *filesystem.FDTable) int {
	for _, j := range sh.jobs {
		sh.printJob(fds.Stdout(), j)
	}
	sh.jobs = slices.DeleteFunc(sh.jobs, func(j *job) bool { return j.ended.Err() != nil })
	return 0
}

// ReportFinishedJobs tells of the background jobs that have ended since
// the last line, the way bash does before its prompt, and forgets them.
func (sh *Shell) ReportFinishedJobs(w io.Writer) {
	for _, j := range sh.jobs {
		if j.ended.Err() != nil {
			sh.printJob(w, j)
		}
	}
	sh.jobs = slices.DeleteFunc(sh.jobs, func(j *job) bool { return j.ended.Err() != nil })
}

// it continues a job and gives it the terminal, then
// waits for it the way it waits for any foreground job.
func (sh *Shell) fgBuiltin(args []string, fds *filesystem.FDTable) int {
	jobReference := "" // none given: the current job
	if len(args) > 0 {
		jobReference = args[0]
	}
	j, err := sh.findJob(jobReference)
	if err != nil {
		fmt.Fprintf(fds.Stderr(), "prt: fg: %v\n", err)
		return 1
	}
	fmt.Fprintln(fds.Stdout(), j.text())

	sh.procHandler.SetForeground(j.Group)
	stopForwarding := context.AfterFunc(sh.ctx, func() {
		sh.procHandler.SignalForeground(proc.SIGINT)
	})
	defer func() {
		stopForwarding()
		sh.procHandler.SetForeground(nil)
	}()

	j.Kill(nil, proc.SIGCONT)
	if j.WaitStopped(j.ended) {
		fmt.Fprintf(fds.Stderr(), "\n[%d]+  %-24s%s\n", j.id, "Stopped", j.text())
		sh.forget(j)
		sh.jobs = append(sh.jobs, j) // it is the current job again
		return 128 + int(proc.SIGTSTP)
	}
	sh.forget(j)
	return j.status
}

// it continues a stopped job where it is, in the background.
func (sh *Shell) bgBuiltin(args []string, fds *filesystem.FDTable) int {
	jobReference := "" // none given: the current job
	if len(args) > 0 {
		jobReference = args[0]
	}
	j, err := sh.findJob(jobReference)
	if err != nil {
		fmt.Fprintf(fds.Stderr(), "prt: bg: %v\n", err)
		return 1
	}
	if j.ended.Err() != nil {
		fmt.Fprintf(fds.Stderr(), "prt: bg: job %d has already completed\n", j.id)
		return 1
	}
	j.Kill(nil, proc.SIGCONT)
	fmt.Fprintf(fds.Stdout(), "[%d]+ %s &\n", j.id, j.text())
	return 0
}

// waitBuiltin is "wait": with no arguments it waits for every background job
func (sh *Shell) waitBuiltin(args []string, fds *filesystem.FDTable) int {
	if len(args) == 0 {
		for _, j := range sh.jobs {
			if !sh.waitJob(j) {
				return 130
			}
		}
		sh.jobs = nil
		return 0 // as in bash: a bare wait succeeds
	}
	status := 0
	for _, arg := range args {
		j, err := sh.findJob(arg)
		if err != nil {
			fmt.Fprintf(fds.Stderr(), "prt: wait: %v\n", err)
			return 127
		}
		if !sh.waitJob(j) {
			return 130
		}
		sh.forget(j)
		status = j.status
	}
	return status
}

// waitJob waits for j to end. It reports false if Ctrl-C came first.
func (sh *Shell) waitJob(j *job) bool {
	select {
	case <-j.ended.Done():
		return true
	case <-sh.ctx.Done():
		return false
	}
}
