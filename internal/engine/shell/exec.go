package shell

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"strconv"
	"strings"

	"parrot/internal/engine/commands"
	"parrot/internal/engine/vfs"
)

// TODO: Configuring
const (
	maxLoops = 100000
	maxCalls = 100
)

type Streams struct {
	In  io.Reader
	Out io.Writer
	Err io.Writer
}

// Shell is one running shell: a filesystem, and the state kept between commands.
type Shell struct {
	fs      *vfs.VFS
	vars    map[string]string
	funcs   map[string]*List
	params  []string // the arguments a script was given, $1 onwards
	status  int      // the exit status of the last command, $?
	calls   int      // how deep we are in function calls
	History []string

	// control is set by break, continue, return and exit; the loop, function
	// or shell they were meant for clears it again.
	control control
	code    int
}

type control int

const (
	running control = iota
	breaking
	continuing
	returning
	exiting
)

func New(fs *vfs.VFS) *Shell {
	return &Shell{fs: fs, vars: map[string]string{}, funcs: map[string]*List{}}
}

func (sh *Shell) Vars() map[string]string { return sh.vars }

func (sh *Shell) SetVars(vars map[string]string) { sh.vars = vars }

func (sh *Shell) SetParams(params []string) { sh.params = params }

func (sh *Shell) Exiting() (int, bool) {
	if sh.control != exiting {
		return 0, false
	}
	sh.control = running
	return sh.code, true
}

// Run parses and runs source text, returning the status of the last command.
func (sh *Shell) Run(src string, io Streams) int {
	list, err := Parse(src)
	if err != nil {
		return sh.fail(io, err)
	}

	status := sh.runList(list, io)
	if sh.control != exiting {
		sh.control = running
	}
	return status
}

func (sh *Shell) runList(list *List, io Streams) int {
	status := 0
	for _, cmd := range list.Cmds {
		status = sh.runCmd(cmd, io)
		if sh.control != running {
			break
		}
	}
	return status
}

func (sh *Shell) runCmd(cmd Cmd, io Streams) int {
	switch c := cmd.(type) {
	case *List:
		return sh.runList(c, io)
	case *AndOr:
		return sh.runAndOr(c, io)
	case *Pipeline:
		return sh.runPipeline(c, io)
	case *If:
		return sh.runIf(c, io)
	case *Loop:
		return sh.runLoop(c, io)
	case *For:
		return sh.runFor(c, io)
	// TODO: Implement
	// case *FuncDef:
	// 	sh.funcs[c.Name] = c.Body
	// 	return sh.setStatus(0)
	case *Simple:
		return sh.runSimple(c, io)
	}
	return sh.fail(io, fmt.Errorf("cannot run %T", cmd))
}

func (sh *Shell) runAndOr(cmd *AndOr, io Streams) int {
	status := sh.runCmd(cmd.Left, io)
	if sh.control != running {
		return status
	}
	// && runs the right side after a success, || after a failure.
	if (cmd.Op == "&&") == (status == 0) {
		return sh.runCmd(cmd.Right, io)
	}
	return status
}

func (sh *Shell) runIf(cmd *If, io Streams) int {
	if sh.runList(cmd.Cond, io) == 0 {
		return sh.runList(cmd.Then, io)
	}
	if cmd.Else != nil {
		return sh.runList(cmd.Else, io)
	}
	return sh.setStatus(0)
}

func (sh *Shell) runLoop(loop *Loop, io Streams) int {
	status := 0
	for turn := 0; turn < maxLoops; turn++ {
		succeeded := sh.runList(loop.Cond, io) == 0
		if sh.control != running {
			return status
		}
		if succeeded == loop.Until {
			return status
		}
		status = sh.runList(loop.Body, io)
		if sh.stopLoop() {
			return status
		}
	}
	return sh.fail(io, fmt.Errorf("loop ran too long")) // TODO: create error object
}

func (sh *Shell) runFor(cmd *For, io Streams) int {
	items, err := sh.expandWords(cmd.Items, io)
	if err != nil {
		return sh.fail(io, err)
	}

	status := 0
	for _, item := range items {
		sh.vars[cmd.Name] = item
		status = sh.runList(cmd.Body, io)
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

func (sh *Shell) runSimple(cmd *Simple, io Streams) int {
	args, err := sh.expandWords(cmd.Words, io)
	if err != nil {
		return sh.fail(io, err)
	}

	for _, assign := range cmd.Assigns {
		value, err := sh.expandOne(assign.Value, io)
		if err != nil {
			return sh.fail(io, err)
		}
		sh.vars[assign.Name] = value
	}
	if len(args) == 0 {
		return sh.setStatus(0) // the command was only assignments
	}

	streams, err := sh.redirect(cmd.Redirs, io)
	if err != nil {
		return sh.fail(io, err)
	}
	return sh.setStatus(sh.runCommand(args, streams))
}

func (sh *Shell) runPipeline(pipeline *Pipeline, io Streams) int {
	status := 0
	in := io.In

	for i, cmd := range pipeline.Cmds {
		var collected bytes.Buffer
		out := io.Out
		if i < len(pipeline.Cmds)-1 {
			out = &collected // not the last: its output feeds the next one
		}

		status = sh.runCmd(cmd, Streams{In: in, Out: out, Err: io.Err})
		if sh.control != running {
			break
		}
		in = bytes.NewReader(collected.Bytes())
	}
	return status
}

func (sh *Shell) runCommand(args []string, io Streams) int {
	name, rest := args[0], args[1:]

	if body, ok := sh.funcs[name]; ok {
		return sh.callFunc(body, rest, io)
	}
	if status, ok := sh.builtin(name, rest, io); ok {
		return status
	}
	if cmd, ok := commands.Lookup(name); ok {
		return cmd.Run(sh.context(io), rest)
	}

	fmt.Fprintf(io.Err, "prt: %s: command not found\n", name)
	return 127
}

// context is what the commands package expects to be handed.
func (sh *Shell) context(io Streams) *commands.Context {
	return &commands.Context{
		VFS:     sh.fs,
		Stdin:   io.In,
		Stdout:  io.Out,
		Stderr:  io.Err,
		Env:     sh.vars,
		History: sh.History,
	}
}

func (sh *Shell) callFunc(body *List, args []string, io Streams) int {
	log.Fatal("Not implemented") // TODO
	return 0
}

func (sh *Shell) redirect(redirs []Redirect, streams Streams) (Streams, error) {
	for _, redirect := range redirs {
		if redirect.Op == "2>&1" {
			streams.Err = streams.Out // send errors wherever output goes
			continue
		}

		name, err := sh.expandOne(redirect.Target, streams)
		if err != nil {
			return streams, err
		}

		if redirect.Op == "<" {
			content, err := sh.fs.Read(name)
			if err != nil {
				return streams, fmt.Errorf("%s: %v", name, err)
			}
			streams.In = bytes.NewReader(content)
			continue
		}

		file, err := sh.openWrite(name, strings.HasSuffix(redirect.Op, ">>"))
		if err != nil {
			return streams, err
		}
		if strings.HasPrefix(redirect.Op, "2") {
			streams.Err = file
			continue
		}
		streams.Out = file
	}
	return streams, nil
}

// TODO: Later use a unified writer reader, especially when devices are implemented
type fileWriter struct{ node *vfs.Node }

func (w fileWriter) Write(p []byte) (int, error) {
	w.node.Append(p)
	return len(p), nil
}

func (sh *Shell) openWrite(name string, appending bool) (io.Writer, error) {
	node, err := sh.fs.Resolve(name)
	if err != nil {
		if err := sh.fs.Create(name); err != nil {
			return nil, fmt.Errorf("%s: %v", name, err)
		}
		if node, err = sh.fs.Resolve(name); err != nil {
			return nil, fmt.Errorf("%s: %v", name, err)
		}
	}
	if node.IsDir() {
		return nil, fmt.Errorf("%s: is a directory", name)
	}
	if !appending {
		node.Override(nil)
	}
	return fileWriter{node}, nil
}

func (sh *Shell) setStatus(status int) int {
	sh.status = status
	return status
}

func (sh *Shell) builtin(name string, args []string, io Streams) (int, bool) {
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
		return sh.source(args, io), true
	case "test", "[":
		return sh.test(args, io), true
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

func (sh *Shell) source(args []string, io Streams) int {
	if len(args) == 0 {
		fmt.Fprintln(io.Err, "prt: source: no file given")
		return 2
	}

	content, err := sh.fs.Read(args[0])
	if err != nil {
		fmt.Fprintf(io.Err, "prt: source: %s: %v\n", args[0], err)
		return 1
	}

	saved := sh.params
	if len(args) > 1 {
		sh.params = args[1:]
	}
	status := sh.Run(string(content), io)
	sh.params = saved
	return status
}

// test is the "test" builtin, which is also written "[ ... ]". It is what
// gives "if" and "while" something to ask about.
func (sh *Shell) test(args []string, io Streams) int {
	if len(args) > 0 && args[len(args)-1] == "]" {
		args = args[:len(args)-1] // the closing bracket is not an argument
	}

	result, err := sh.testExpr(args)
	if err != nil {
		fmt.Fprintf(io.Err, "prt: test: %v\n", err)
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
		return exists && len(node.Content) > 0, nil
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

func (sh *Shell) fail(streams Streams, err error) int {
	fmt.Fprintf(streams.Err, "prt: %v\n", err)
	return sh.setStatus(2)
}
