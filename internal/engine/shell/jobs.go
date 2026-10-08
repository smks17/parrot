package shell

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"

	"parrot/internal/engine/commands"
	"parrot/internal/engine/filesystem"
	"parrot/internal/engine/proc"
)

// A job is what one command line, or one "&", runs.
type job struct {
	*proc.Group
	id         int  // its number, as in "[1] 304"; 0 for a foreground job
	foreground bool // it has the terminal: Ctrl-C and Ctrl-Z reach its group

	started chan struct{} // closed once the first process starts, or the job ends
	done    chan struct{} // closed once the job has ended
	status  int           // how it ended; read only after done is closed
}

func (sh *Shell) newJob(id int, foreground bool) *job {
	return &job{Group: sh.procHandler.NewGroup(), id: id, foreground: foreground,
		started: make(chan struct{}), done: make(chan struct{})}
}

func (j *job) finish(status int) {
	j.status = status
	if j.PGID() == 0 { // it never started a process, so nothing closed started
		close(j.started)
	}
	close(j.done)
}

// inJob runs fn as a foreground job of its own, unless the shell is already
// running inside a job
func (sh *Shell) inJob(fn func() int) int {
	if sh.job != nil {
		return fn()
	}
	sh.job = sh.newJob(0, true)

	stopForwarding := context.AfterFunc(sh.ctx, func() {
		sh.procHandler.SignalForeground(proc.SIGINT)
	})
	defer func() {
		stopForwarding()
		sh.job = nil
		sh.procHandler.SetForeground(nil)
	}()
	return fn()
}

// runProcess runs a command as a child process of the shell, in the current
// job, and waits for it.
func (sh *Shell) runProcess(cmd commands.Command, name string, args []string, fds *filesystem.FDTable) int {
	return sh.inJob(func() int {
		j := sh.job
		spec := proc.Spec{
			Parent: sh.proc, User: sh.fs.Identity(), Name: name, Args: args,
			Run: func(p *proc.Process) int {
				child := *sh // a copy, so the command sees p as its process
				child.proc, child.ctx = p, p.Context()
				return cmd.Run(child.context(fds), args)
			},
		}

		p := j.Spawn(spec)
		if j.PGID() == p.PID { // the first: it leads the job's group
			if j.foreground {
				sh.procHandler.SetForeground(j.Group)
			}
			close(j.started)
		}
		if j.foreground && sh.ctx.Err() != nil {
			j.Kill(nil, proc.SIGINT)
		}

		// Killed while it waits on a pipe: closing the pipe wakes it.
		stop := context.AfterFunc(p.Context(), func() {
			var sig proc.SignalError
			if errors.As(context.Cause(p.Context()), &sig) || sh.ctx.Err() != nil {
				for _, end := range sh.pipes {
					end.Close()
				}
			}
		})
		defer stop()

		status, _ := sh.procHandler.Wait([]*proc.Process{p}, false)
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
		select {
		case <-j.done:
		default:
			id = max(id, j.id)
		}
	}
	return id + 1
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
		n, err := strconv.Atoi(arg)
		i := slices.IndexFunc(sh.jobs, func(j *job) bool { return int(j.PGID()) == n })
		if err != nil || i < 0 {
			fmt.Fprintf(fds.Stderr(), "wait: pid %s is not a child of this shell\n", arg)
			return 127
		}
		j := sh.jobs[i]
		if !sh.waitJob(j) {
			return 130
		}
		sh.jobs = slices.Delete(sh.jobs, i, i+1)
		status = j.status
	}
	return status
}

// waitJob waits for j to end. It reports false if Ctrl-C came first.
func (sh *Shell) waitJob(j *job) bool {
	select {
	case <-j.done:
		return true
	case <-sh.ctx.Done():
		return false
	}
}
