package shell

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"sync"

	"parrot/internal/engine/commands"
	"parrot/internal/engine/filesystem"
	"parrot/internal/engine/proc"
	"parrot/internal/engine/stream"
	"parrot/internal/engine/user"
)

// TODO: Configuring
const (
	maxLoops = 100000
	maxCalls = 100
)

// Shell is one running shell: a filesystem, and the state kept between commands.
type Shell struct {
	fs      filesystem.FS
	vars    map[string]string
	funcs   map[string]*List
	params  []string // the arguments a script was given, $1 onwards
	status  int      // the exit status of the last command, $?
	calls   int      // how deep we are in function calls
	History []commands.HistoryEntry

	// control is set by break, continue, return and exit; the loop, function
	// or shell they were meant for clears it again.
	control control
	code    int

	ctx  context.Context
	tick func() // The shell alls it where a loop could otherwise run forever without parking

	user       user.Identity
	SwitchUser func(name, password string) error

	proc        *proc.Process // the shell's process, every command's parent
	procHandler *proc.Table
	pid         proc.PID       // the shell's own PID, $$
	lastBG      proc.PID       // the last job started with &, $!
	job         *job           // the job commands run in now; nil between command lines
	jobs        []*job         // background and stopped jobs, until wait, fg or jobs collects them
	pipes       []*stream.File // a pipeline stage's pipe ends, closed if its command is killed
	lastProcess *proc.Process  // the last process this shell started

	// Fallback is asked for a command the shell does not have. It is how a
	// shell on the real filesystem reaches the programs installed on the
	// machine; the browser leaves it nil and has none.
	Fallback func(name string) (commands.Command, bool)
}

type control int

const (
	running control = iota
	breaking
	continuing
	returning
	exiting
	interrupting
)

func New(fs filesystem.FS, switchUser func(name, password string) error) *Shell {
	sh := &Shell{fs: fs, vars: map[string]string{}, funcs: map[string]*List{}, user: fs.Identity(), SwitchUser: switchUser, ctx: context.Background()}
	sh.procHandler = proc.NewTable()
	sh.proc = sh.procHandler.Attach(1, fs.Identity(), "prt")
	sh.pid = sh.proc.PID
	return sh
}

func (sh *Shell) Vars() map[string]string { return sh.vars }

func (sh *Shell) SetVars(vars map[string]string) { sh.vars = vars }

func (sh *Shell) SetParams(params []string) { sh.params = params }

func (sh *Shell) SetUser(id user.Identity) {
	sh.user = id
	sh.proc.SetUser(id)
}

func (sh *Shell) SetContext(ctx context.Context) {
	if ctx == nil {
		ctx = context.Background()
	}
	sh.ctx = ctx
}

func (sh *Shell) Interrupted() bool {
	if sh.control != interrupting {
		return false
	}
	sh.control = running
	return true
}

func (sh *Shell) SetYield(tick func()) { sh.tick = tick }

func (sh *Shell) interrupted() bool {
	if sh.tick != nil {
		sh.tick()
	}
	if sh.ctx.Err() == nil {
		return false
	}
	sh.control = interrupting
	sh.setStatus(130) // 128 + SIGINT, the way a real shell reports it
	return true
}

func (sh *Shell) Exiting() (int, bool) {
	if sh.control != exiting {
		return 0, false
	}
	sh.control = running
	return sh.code, true
}

// Run parses and runs source text, returning the status of the last command.
func (sh *Shell) Run(src string, fds *stream.FDTable) int {
	list, err := Parse(src)
	if err != nil {
		return sh.fail(fds, err)
	}

	status := sh.runList(list, fds)
	if sh.control != exiting && sh.control != interrupting {
		sh.control = running
	}
	return status
}

func (sh *Shell) runList(list *List, fds *stream.FDTable) int {
	status := 0
	for _, cmd := range list.Cmds {
		if sh.interrupted() {
			return sh.status
		}
		status = sh.runCmd(cmd, fds)
		if sh.control != running {
			break
		}
	}
	return status
}

func (sh *Shell) runCmd(cmd Cmd, fds *stream.FDTable) int {
	switch c := cmd.(type) {
	case *List:
		return sh.runList(c, fds)
	case *AndOr:
		return sh.runAndOr(c, fds)
	case *Pipeline:
		return sh.runPipeline(c, fds)
	case *If:
		return sh.runIf(c, fds)
	case *Loop:
		return sh.runLoop(c, fds)
	case *For:
		return sh.runFor(c, fds)
	case *Background:
		return sh.runBackground(c.Cmd, fds)
	case *Not:
		if sh.runCmd(c.Cmd, fds) == 0 {
			return sh.setStatus(1)
		}
		return sh.setStatus(0)
	case *FuncDef:
		sh.funcs[c.Name] = c.Body
		return sh.setStatus(0)
	case *Simple:
		return sh.runSimple(c, fds)
	}
	return sh.fail(fds, fmt.Errorf("cannot run %T", cmd))
}

func (sh *Shell) runAndOr(cmd *AndOr, fds *stream.FDTable) int {
	status := sh.runCmd(cmd.Left, fds)
	if sh.control != running {
		return status
	}
	// && runs the right side after a success, || after a failure.
	if (cmd.Op == "&&") == (status == 0) {
		return sh.runCmd(cmd.Right, fds)
	}
	return status
}

func (sh *Shell) runIf(cmd *If, fds *stream.FDTable) int {
	if sh.runList(cmd.Cond, fds) == 0 {
		return sh.runList(cmd.Then, fds)
	}
	if cmd.Else != nil {
		return sh.runList(cmd.Else, fds)
	}
	return sh.setStatus(0)
}

func (sh *Shell) runLoop(loop *Loop, fds *stream.FDTable) int {
	status := 0
	for turn := 0; turn < maxLoops; turn++ {
		if sh.interrupted() {
			return sh.status
		}
		succeeded := sh.runList(loop.Cond, fds) == 0
		if sh.control == interrupting {
			return sh.status // the condition was killed by Ctrl-C: 130, not the body's last
		}
		if sh.control != running {
			return status
		}
		if succeeded == loop.Until {
			return status
		}
		status = sh.runList(loop.Body, fds)
		if sh.stopLoop() {
			return status
		}
	}
	return sh.fail(fds, fmt.Errorf("loop ran too long")) // TODO: create error object
}

func (sh *Shell) runFor(cmd *For, fds *stream.FDTable) int {
	items, err := sh.expandWords(cmd.Items, fds)
	if err != nil {
		return sh.fail(fds, err)
	}

	status := 0
	for _, item := range items {
		if sh.interrupted() {
			return sh.status
		}
		sh.vars[cmd.Name] = item
		status = sh.runList(cmd.Body, fds)
		if sh.stopLoop() {
			break
		}
	}
	return status
}

func (sh *Shell) stopLoop() bool {
	switch sh.control {
	case running:
		return false
	case continuing:
		sh.control = running
		return false
	case breaking:
		sh.control = running
		return true
	}
	return true
}

func (sh *Shell) runSimple(cmd *Simple, fds *stream.FDTable) int {
	args, err := sh.expandWords(cmd.Words, fds)
	if err != nil {
		return sh.fail(fds, err)
	}

	for _, assign := range cmd.Assigns {
		value, err := sh.expandOne(assign.Value, fds)
		if err != nil {
			return sh.fail(fds, err)
		}
		sh.vars[assign.Name] = value
	}
	if len(args) == 0 {
		return sh.setStatus(0) // the command was only assignments
	}

	redirected, err := sh.redirect(cmd.Redirs, fds)
	if err != nil {
		return sh.fail(redirected, err)
	}
	return sh.setStatus(sh.runCommand(args, redirected))
}

// runPipeline runs the stages at the same time, each reading what the one
// before it writes through an stream.Pipe. A pipe holds no buffer, so a fast
// producer waits for its reader instead of growing memory, and "cat file |
// wc" starts counting before cat is done.
func (sh *Shell) runPipeline(pipeline *Pipeline, fds *stream.FDTable) int {
	if len(pipeline.Cmds) == 1 {
		return sh.runCmd(pipeline.Cmds[0], fds)
	}
	return sh.inJob(fds, func() int { // the stages are one job, so Ctrl-C reaches all of them
		last := len(pipeline.Cmds) - 1
		var stages sync.WaitGroup
		statuses := make([]int, len(pipeline.Cmds))

		// read is the previous stage
		var read *stream.File

		for i, cmd := range pipeline.Cmds {
			stage := fds.Clone()

			var localFdTable stream.FDTable
			var ends []*stream.File // this stage's pipe ends
			if read != nil {
				stage.Set(stream.Stdin, read)
				localFdTable.Alloc(read)
				ends = append(ends, read)
			}
			if i < last {
				var write *stream.File
				read, write = stream.Pipe()
				stage.Set(stream.Stdout, write)
				localFdTable.Alloc(write)
				ends = append(ends, write)
			}

			child := sh.sub()
			child.pipes = ends
			child.fs = newDirView(sh.fs) // a stage is a subshell: its cd stays in it
			stages.Add(1)
			go func() {
				defer stages.Done()
				statuses[i] = child.runCmd(cmd, stage)
				if child.interrupted() {
					statuses[i] = child.status
				}
				// A stage Ctrl-Z stopped still reads and writes its pipe once
				// fg or bg continues it, so its ends stay open until its process
				// ends; then the next stage sees EOF, as it would have.
				if p := child.lastProcess; p != nil && sh.procHandler.Stopped([]*proc.Process{p}) {
					go func() {
						sh.procHandler.Wait([]*proc.Process{p}, false)
						localFdTable.Destroy()
					}()
				} else {
					localFdTable.Destroy()
				}
			}()
		}
		stages.Wait()

		if sh.ctx.Err() != nil {
			sh.control = interrupting
			return sh.setStatus(130) // 128 + SIGINT, the way a real shell reports it
		}
		return sh.setStatus(statuses[last])
	})
}

func (sh *Shell) sub() *Shell {
	child := *sh
	child.vars = maps.Clone(sh.vars)
	child.funcs = maps.Clone(sh.funcs)
	child.params = slices.Clone(sh.params)
	child.control = running
	child.jobs = nil
	child.lastProcess = nil
	return &child
}

func (sh *Shell) runCommand(args []string, fds *stream.FDTable) int {
	name, rest := args[0], args[1:]

	if body, ok := sh.funcs[name]; ok {
		return sh.callFunc(body, rest, fds)
	}
	if status, ok := sh.builtin(name, rest, fds); ok {
		return status
	}
	if cmd, ok := commands.Lookup(name); ok {
		return sh.runProcess(cmd, name, rest, fds)
	}
	if sh.Fallback != nil {
		if cmd, ok := sh.Fallback(name); ok {
			return sh.runProcess(cmd, name, rest, fds)
		}
	}

	fmt.Fprintf(fds.Stderr(), "prt: %s: command not found\n", name)
	return 127
}

// context is what the commands package expects to be handed.
func (sh *Shell) context(fds *stream.FDTable) *commands.Context {
	guard := proc.StreamGuard{Context: sh.ctx, YieldToHost: sh.tick}
	if !sh.proc.Attached() {
		guard.Process = sh.proc
	}
	return &commands.Context{
		Ctx:             sh.ctx,
		VFS:             viewFor(sh.fs, fds),
		Fds:             fds,
		Stdin:           guard.WrapReader(fds.Stdin()),
		Stdout:          guard.WrapWriter(fds.Stdout()),
		Stderr:          guard.WrapWriter(fds.Stderr()),
		Env:             sh.vars,
		History:         sh.History,
		User:            sh.fs.Identity(),
		SwitchUser:      sh.SwitchUser,
		Proc:            sh.procHandler,
		Self:            sh.proc,
		JobProcessGroup: sh.jobProcessGroup,
	}
}

func (sh *Shell) callFunc(body *List, args []string, fds *stream.FDTable) int {
	if sh.calls >= maxCalls {
		fmt.Fprintf(fds.Stderr(), "prt: maximum function nesting level exceeded (%d)\n", maxCalls)
		return 1
	}

	saved := sh.params
	sh.params = args
	sh.calls++
	status := sh.runList(body, fds)
	sh.calls--
	sh.params = saved

	if sh.control == returning { // "return" ends this function and nothing more
		sh.control = running
		status = sh.code
	}
	return sh.setStatus(status)
}

func (sh *Shell) redirect(redirs []Redirect, fds *stream.FDTable) (*stream.FDTable, error) {
	if len(redirs) == 0 {
		return fds, nil
	}
	fds = fds.Clone()

	for _, redirect := range redirs {
		if redirect.Op == "2>&1" {
			err := fds.Dup(stream.Stdout, stream.Stderr)
			if err != nil {
				return fds, err
			}
			continue
		}
		if redirect.Op == ">&2" {
			err := fds.Dup(stream.Stderr, stream.Stdout)
			if err != nil {
				return fds, err
			}
			continue
		}

		name, err := sh.expandOne(redirect.Target, fds)
		if err != nil {
			return fds, err
		}

		// Opened on behalf of the table being built, so "> /dev/stderr" after
		// "2>&1" sees what the redirects before it left, left to right as in sh.
		if redirect.Op == "<" {
			file, err := sh.fs.OpenFor(name, stream.O_RDONLY, fds)
			if err != nil {
				return fds, fmt.Errorf("%s: %v", name, err)
			}
			fds.Set(stream.Stdin, file)
			continue
		}

		flags := stream.O_WRONLY | stream.O_CREATE | stream.O_TRUNC
		if strings.HasSuffix(redirect.Op, ">>") {
			flags = stream.O_WRONLY | stream.O_CREATE | stream.O_APPEND
		}
		file, err := sh.fs.OpenFor(name, flags, fds)
		if err != nil {
			return fds, fmt.Errorf("%s: %v", name, err)
		}
		fd := stream.Stdout
		if strings.HasPrefix(redirect.Op, "2") {
			fd = stream.Stderr
		}
		fds.Set(fd, file)
	}
	return fds, nil
}

func (sh *Shell) setStatus(status int) int {
	sh.status = status
	return status
}

func (sh *Shell) builtin(name string, args []string, fds *stream.FDTable) (int, bool) {
	switch name {
	case ":":
		return 0, true
	case "exit":
		sh.control, sh.code = exiting, exitStatus(args, 0)
		return sh.code, true
	case "return":
		sh.control, sh.code = returning, exitStatus(args, sh.status)
		return sh.code, true
	case "break":
		sh.control = breaking
		return 0, true
	case "continue":
		sh.control = continuing
		return 0, true
	case "shift":
		if len(sh.params) == 0 {
			return 1, true
		}
		sh.params = sh.params[1:]
		return 0, true
	case "export":
		for _, arg := range args {
			if name, value, ok := strings.Cut(arg, "="); ok {
				sh.vars[name] = value
			}
		}
		return 0, true
	case "unset":
		for _, name := range args {
			delete(sh.vars, name)
			delete(sh.funcs, name)
		}
		return 0, true
	case "source", ".":
		return sh.source(args, fds), true
	case "test", "[":
		return sh.test(args, fds), true
	case "wait":
		return sh.waitBuiltin(args, fds), true
	case "jobs":
		return sh.jobsBuiltin(fds), true
	case "fg":
		return sh.fgBuiltin(args, fds), true
	case "bg":
		return sh.bgBuiltin(args, fds), true
	}
	return 0, false
}

func exitStatus(args []string, fallback int) int {
	if len(args) == 0 {
		return fallback
	}
	status, err := strconv.Atoi(args[0])
	if err != nil {
		return 2
	}
	return status
}

func (sh *Shell) source(args []string, fds *stream.FDTable) int {
	if len(args) == 0 {
		fmt.Fprintln(fds.Stderr(), "prt: source: no file given")
		return 2
	}

	content, err := sh.fs.Read(args[0])
	if err != nil {
		fmt.Fprintf(fds.Stderr(), "prt: source: %s: %v\n", args[0], err)
		return 1
	}
	if sh.calls >= maxCalls {
		fmt.Fprintf(fds.Stderr(), "prt: source: %s: nested too deeply\n", args[0])
		return 1
	}

	saved := sh.params
	if len(args) > 1 {
		sh.params = args[1:]
	}
	sh.calls++
	status := sh.Run(string(content), fds)
	sh.calls--
	sh.params = saved
	return status
}

// test is the "test" builtin, which is also written "[ ... ]". It is what
// gives "if" and "while" something to ask about.
func (sh *Shell) test(args []string, fds *stream.FDTable) int {
	if len(args) > 0 && args[len(args)-1] == "]" {
		args = args[:len(args)-1] // the closing bracket is not an argument
	}

	result, err := sh.testExpr(args)
	if err != nil {
		fmt.Fprintf(fds.Stderr(), "prt: test: %v\n", err)
		return 2
	}
	if !result {
		return 1 // false, in the shell's way of saying it
	}
	return 0
}

func (sh *Shell) testExpr(args []string) (bool, error) {
	switch {
	case len(args) == 0:
		return false, nil
	case args[0] == "!":
		result, err := sh.testExpr(args[1:])
		return !result, err
	case len(args) == 1:
		return args[0] != "", nil // a string on its own: true when not empty
	case len(args) == 2:
		return sh.testOne(args[0], args[1])
	case len(args) == 3:
		return sh.testTwo(args[0], args[1], args[2])
	}
	return false, fmt.Errorf("too many arguments")
}

func (sh *Shell) testOne(op, operand string) (bool, error) {
	switch op {
	case "-z":
		return operand == "", nil
	case "-n":
		return operand != "", nil
	}

	entry, err := sh.fs.Stat(operand)
	exists := err == nil
	switch op {
	case "-e":
		return exists, nil
	case "-f":
		return exists && !entry.IsDir(), nil
	case "-d":
		return exists && entry.IsDir(), nil
	case "-s":
		return exists && entry.Size > 0, nil
	}
	return false, fmt.Errorf("unknown check %q", op)
}

func (sh *Shell) testTwo(left, op, right string) (bool, error) {
	switch op {
	case "=", "==":
		return left == right, nil
	case "!=":
		return left != right, nil
	}

	first, err := strconv.Atoi(left)
	if err != nil {
		return false, fmt.Errorf("%q is not a number", left)
	}
	second, err := strconv.Atoi(right)
	if err != nil {
		return false, fmt.Errorf("%q is not a number", right)
	}

	switch op {
	case "-eq":
		return first == second, nil
	case "-ne":
		return first != second, nil
	case "-lt":
		return first < second, nil
	case "-le":
		return first <= second, nil
	case "-gt":
		return first > second, nil
	case "-ge":
		return first >= second, nil
	}
	return false, fmt.Errorf("unknown comparison %q", op)
}

func (sh *Shell) fail(fds *stream.FDTable, err error) int {
	fmt.Fprintf(fds.Stderr(), "prt: %v\n", err)
	return sh.setStatus(2)
}
