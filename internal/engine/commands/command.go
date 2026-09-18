package commands

import (
	"bytes"
	"fmt"
	"io"
	"maps"
	"parrot/internal/engine/user"
	"parrot/internal/engine/vfs"
	"slices"
	"strings"
	"time"
)

type Command interface {
	Name() string
	Usage() string
	Run(ctx *Context, args []string) int // exit code
}

type Hidden interface{ Hidden() bool }

type HistoryEntry struct {
	Line string
	At   time.Time
}

type Context struct {
	VFS     *vfs.VFS
	Stdin   io.Reader
	Stdout  io.Writer
	Stderr  io.Writer
	Env     map[string]string
	History []HistoryEntry

	User    user.Identity
	SetUser func(name string) error
}

// input is what a filter reads: stdin when no files are named, otherwise the
// files concatenated. A non-zero exit means an error was already printed.
func input(ctx *Context, name string, files []string) (io.Reader, int) {
	if len(files) == 0 {
		if ctx.Stdin == nil {
			return strings.NewReader(""), 0
		}
		return ctx.Stdin, 0
	}
	readers := make([]io.Reader, 0, len(files))
	for _, file := range files {
		content, err := ctx.VFS.Read(file)
		if err != nil {
			fmt.Fprintf(ctx.Stderr, "%s: %s: %v\n", name, file, err)
			return nil, 1
		}
		readers = append(readers, bytes.NewReader(content))
	}
	return io.MultiReader(readers...), 0
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
