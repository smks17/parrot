package commands

import (
	"io"
	"maps"
	"parrot/internal/engine/vfs"
	"slices"
	"strings"
)

type Command interface {
	Name() string
	Usage() string
	Run(ctx *Context, args []string) int // exit code
}

type Hidden interface{ Hidden() bool }

type Context struct {
	VFS    *vfs.VFS
	Stdout io.Writer
	Stderr io.Writer
	Env    map[string]string
}

var registry = map[string]Command{}

func Register(c Command) { registry[c.Name()] = c }
func Lookup(name string) (Command, bool) {
	command, exist := registry[name]
	return command, exist
}

func All() []Command {
	cmds := slices.Collect(maps.Values(registry))
	slices.SortFunc(cmds, func(a, b Command) int {
		return strings.Compare(a.Name(), b.Name())
	})
	return cmds
}
