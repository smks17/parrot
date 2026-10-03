// Package external runs the programs already installed on the machine, so
// that a shell working on the real filesystem can call git or python as well
// as its own commands.
//
// Nothing else imports it: the browser has no processes to start, and keeping
// os/exec out of that build keeps a runtime surprise from replacing a
// compile-time one.
package external

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sort"

	"parrot/internal/engine/commands"
)

// Lookup finds a program on PATH. It is handed to the engine as the last
// resort, after the built-in commands have had their say — which is the order
// a real shell uses for its own builtins.
func Lookup(name string) (commands.Command, bool) {
	path, err := exec.LookPath(name)
	if err != nil {
		return nil, false
	}
	return Program{name: name, path: path}, true
}

// Program is one installed executable, wrapped so that the shell can run it
// like any other command.
type Program struct {
	name string
	path string
}

var _ commands.Command = Program{}

func (p Program) Name() string { return p.name }

func (p Program) Usage() string { return p.name + "  — " + p.path }

// Hidden keeps installed programs out of "help", which lists what this shell
// provides rather than what the machine happens to have.
func (p Program) Hidden() bool { return true }

func (p Program) Run(ctx *commands.Context, args []string) int {
	cmd := exec.Command(p.path, args...)

	// The shell's own working directory, not the process's: they are
	// deliberately different.
	cmd.Dir = ctx.VFS.Cwd()
	cmd.Stdin = ctx.Stdin
	cmd.Stdout = ctx.Stdout
	cmd.Stderr = ctx.Stderr
	cmd.Env = environ(ctx.Env)

	if err := cmd.Run(); err != nil {
		var exited *exec.ExitError
		if errors.As(err, &exited) {
			return exited.ExitCode() // the program ran and failed, which is its own news
		}
		fmt.Fprintf(ctx.Stderr, "%s: %v\n", p.name, err)
		return 126
	}
	return 0
}

// environ hands the program the machine's environment with the shell's own
// variables laid over it, so that PATH and HOME survive.
func environ(vars map[string]string) []string {
	env := os.Environ()
	for name, value := range vars {
		env = append(env, name+"="+value)
	}
	sort.Strings(env)
	return env
}
