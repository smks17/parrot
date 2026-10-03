package commands

import (
	"fmt"
	"strconv"

	"parrot/internal/engine/filesystem"
)

type Chmod struct{}

var _ Command = Chmod{}

func (c Chmod) Name() string { return "chmod" }

func (c Chmod) Usage() string { return "chmod [-R] <octal-mode> <path>  — change file permissions" }

func (c Chmod) Run(ctx *Context, args []string) int {
	var recursive bool
	var operands []string
	for _, arg := range args {
		if arg == "-R" {
			recursive = true
			continue
		}
		operands = append(operands, arg)
	}

	if len(operands) < 2 {
		fmt.Fprintf(ctx.Stderr, "%s: missing operand\n", c.Name())
		return 1
	}
	modeStr, path := operands[0], operands[1]

	mode, err := parseMode(modeStr)
	if err != nil {
		fmt.Fprintf(ctx.Stderr, "%s: invalid mode: '%s'\n", c.Name(), modeStr)
		return 1
	}

	if !recursive {
		if err := ctx.VFS.Chmod(path, mode); err != nil {
			fmt.Fprintf(ctx.Stderr, "%s: %s: %v\n", c.Name(), path, err)
			return 1
		}
		return 0
	}

	// -R walks the tree and changes each entry by path, which is all a
	// filesystem has to offer in common — the in-memory one has nodes, the
	// real one does not.
	err = ctx.VFS.Walk(path, func(entry filesystem.Info) error {
		if err := ctx.VFS.Chmod(entry.Path, mode); err != nil {
			return fmt.Errorf("%s: %s: %v", c.Name(), entry.Path, err)
		}
		return nil
	})
	if err != nil {
		fmt.Fprintln(ctx.Stderr, err)
		return 1
	}
	return 0
}

func parseMode(s string) (filesystem.FileMode, error) {
	v, err := strconv.ParseUint(s, 8, 32)
	if err != nil || v > 0777 {
		return 0, fmt.Errorf("invalid mode")
	}
	return filesystem.FileMode(v), nil
}

func init() { Register(Chmod{}) }
