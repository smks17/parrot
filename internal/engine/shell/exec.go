package shell

import (
	"context"
	"fmt"
	"log"
	"maps"
	"slices"
	"strconv"
	"strings"
	"sync"

	"parrot/internal/engine/commands"
	"parrot/internal/engine/user"
	"parrot/internal/engine/vfs"
)

// TODO: Configuring
const (
	maxLoops = 100000
	maxCalls = 100
)

// Shell is one running shell: a filesystem, and the state kept between commands.
type Shell struct {
	fs      *vfs.VFS
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

func New(fs *vfs.VFS, switchUser func(name, password string) error) *Shell {
	return &Shell{fs: fs, vars: map[string]string{}, funcs: map[string]*List{}, user: fs.Identity(), SwitchUser: switchUser, ctx: context.Background()}
}

func (sh *Shell) Vars() map[string]string { return sh.vars }

func (sh *Shell) SetVars(vars map[string]string) { sh.vars = vars }

func (sh *Shell) SetParams(params []string) { sh.params = params }

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
func (sh *Shell) Run(src string, fds *vfs.FDTable) int {
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

func (sh *Shell) runList(list *List, fds *vfs.FDTable) int {
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

func (sh *Shell) runCmd(cmd Cmd, fds *vfs.FDTable) int {
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
	// TODO: Implement
	// case *FuncDef:
	// 	sh.funcs[c.Name] = c.Body
	// 	return sh.setStatus(0)
	case *Simple:
		return sh.runSimple(c, fds)
	}
	return sh.fail(fds, fmt.Errorf("cannot run %T", cmd))
}

func (sh *Shell) runAndOr(cmd *AndOr, fds *vfs.FDTable) int {
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

func (sh *Shell) runIf(cmd *If, fds *vfs.FDTable) int {
	if sh.runList(cmd.Cond, fds) == 0 {
		return sh.runList(cmd.Then, fds)
	}
	if cmd.Else != nil {
		return sh.runList(cmd.Else, fds)
	}
	return sh.setStatus(0)
}

func (sh *Shell) runLoop(loop *Loop, fds *vfs.FDTable) int {
	status := 0
	for turn := 0; turn < maxLoops; turn++ {
		if sh.interrupted() {
			return sh.status
		}
		succeeded := sh.runList(loop.Cond, fds) == 0
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

func (sh *Shell) runFor(cmd *For, fds *vfs.FDTable) int {
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

func (sh *Shell) runSimple(cmd *Simple, fds *vfs.FDTable) int {
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
// before it writes through an io.Pipe. A pipe holds no buffer, so a fast
// producer waits for its reader instead of growing memory, and "cat file |
// wc" starts counting before cat is done.
func (sh *Shell) runPipeline(pipeline *Pipeline, fds *vfs.FDTable) int {
	if len(pipeline.Cmds) == 1 {
		return sh.runCmd(pipeline.Cmds[0], fds)
	}

	last := len(pipeline.Cmds) - 1
	var stages sync.WaitGroup
	statuses := make([]int, len(pipeline.Cmds))

	// read is the previous stage
	var read *vfs.File

	for i, cmd := range pipeline.Cmds {
		stage := fds.Clone()

		var localFdTable vfs.FDTable
		if read != nil {
			stage.Set(vfs.Stdin, read)
			localFdTable.Alloc(read)
		}
		if i < last {
			var write *vfs.File
			read, write = vfs.Pipe()
			stage.Set(vfs.Stdout, write)
			localFdTable.Alloc(write)
		}

		child := sh.sub()
		stages.Add(1)
		go func() {
			defer stages.Done()
			statuses[i] = child.runCmd(cmd, stage)
			localFdTable.Destroy()
			if child.interrupted() {
				statuses[i] = child.status
			}
		}()
	}
	stages.Wait()

	if sh.ctx.Err() != nil {
		sh.control = interrupting
		return sh.setStatus(130) // 128 + SIGINT, the way a real shell reports it
	}
	return sh.setStatus(statuses[len(statuses)-1])
}

func (sh *Shell) sub() *Shell {
	child := *sh
	child.vars = maps.Clone(sh.vars)
	child.funcs = maps.Clone(sh.funcs)
	child.params = slices.Clone(sh.params)
	child.control = running
	return &child
}

func (sh *Shell) runCommand(args []string, fds *vfs.FDTable) int {
	name, rest := args[0], args[1:]

	if body, ok := sh.funcs[name]; ok {
		return sh.callFunc(body, rest, fds)
	}
	if status, ok := sh.builtin(name, rest, fds); ok {
		return status
	}
	if cmd, ok := commands.Lookup(name); ok {
		return cmd.Run(sh.context(fds), rest)
	}

	fmt.Fprintf(fds.Stderr(), "prt: %s: command not found\n", name)
	return 127
}

// context is what the commands package expects to be handed.
func (sh *Shell) context(fds *vfs.FDTable) *commands.Context {
	return &commands.Context{
		Ctx:        sh.ctx,
		VFS:        sh.fs,
		Fds:        fds,
		Stdin:      GuardReader(sh.ctx, sh.tick, fds.Stdin()),
		Stdout:     GuardWriter(sh.ctx, sh.tick, fds.Stdout()),
		Stderr:     GuardWriter(sh.ctx, sh.tick, fds.Stderr()),
		Env:        sh.vars,
		History:    sh.History,
		User:       sh.fs.Identity(),
		SwitchUser: sh.SwitchUser,
	}
}

func (sh *Shell) callFunc(body *List, args []string, fds *vfs.FDTable) int {
	log.Fatal("Not implemented") // TODO
	return 0
}

func (sh *Shell) redirect(redirs []Redirect, fds *vfs.FDTable) (*vfs.FDTable, error) {
	if len(redirs) == 0 {
		return fds, nil
	}
	fds = fds.Clone()

	for _, redirect := range redirs {
		if redirect.Op == "2>&1" {
			err := fds.Dup(vfs.Stdout, vfs.Stderr)
			if err != nil {
				return fds, err
			}
			continue
		}
		if redirect.Op == ">&2" {
			err := fds.Dup(vfs.Stderr, vfs.Stdout)
			if err != nil {
				return fds, err
			}
			continue
		}

		name, err := sh.expandOne(redirect.Target, fds)
		if err != nil {
			return fds, err
		}

		if redirect.Op == "<" {
			file, err := sh.fs.Open(name, vfs.O_RDONLY)
			if err != nil {
				return fds, fmt.Errorf("%s: %v", name, err)
			}
			fds.Set(vfs.Stdin, file)
			continue
		}

		flags := vfs.O_WRONLY | vfs.O_CREATE | vfs.O_TRUNC
		if strings.HasSuffix(redirect.Op, ">>") {
			flags = vfs.O_WRONLY | vfs.O_CREATE | vfs.O_APPEND
		}
		file, err := sh.fs.Open(name, flags)
		if err != nil {
			return fds, fmt.Errorf("%s: %v", name, err)
		}
		fd := vfs.Stdout
		if strings.HasPrefix(redirect.Op, "2") {
			fd = vfs.Stderr
		}
		fds.Set(fd, file)
	}
	return fds, nil
}

func (sh *Shell) setStatus(status int) int {
	sh.status = status
	return status
}

func (sh *Shell) builtin(name string, args []string, fds *vfs.FDTable) (int, bool) {
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

func (sh *Shell) source(args []string, fds *vfs.FDTable) int {
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
func (sh *Shell) test(args []string, fds *vfs.FDTable) int {
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

	node, err := sh.fs.Resolve(operand)
	exists := err == nil
	switch op {
	case "-e":
		return exists, nil
	case "-f":
		return exists && !node.IsDir(), nil
	case "-d":
		return exists && node.IsDir(), nil
	case "-s":
		return exists && len(node.Bytes()) > 0, nil
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

func (sh *Shell) fail(fds *vfs.FDTable, err error) int {
	fmt.Fprintf(fds.Stderr(), "prt: %v\n", err)
	return sh.setStatus(2)
}
