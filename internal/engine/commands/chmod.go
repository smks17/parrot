package commands

import (
	"errors"
	"fmt"
	"strconv"

	"parrot/internal/engine/vfs"
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

	node, err := ctx.VFS.Resolve(path)
	if err != nil {
		fmt.Fprintf(ctx.Stderr, "%s: %s: %v\n", c.Name(), path, err)
		return 1
	}

	exit := 0
	var changeMod func(n *vfs.Node) error
	changeMod = func(n *vfs.Node) error {
		err := ctx.VFS.ChmodNode(n, mode)
		if err != nil {
			return errors.New(fmt.Sprintf("%s: %s: %v\n", c.Name(), n.Path(), err))
		}
		return nil
	}
	err = node.Walk(changeMod)
	if err != nil {
		fmt.Fprintf(ctx.Stderr, err.Error())
		exit = 1
	}
	return exit
}

func parseMode(s string) (vfs.FileMode, error) {
	v, err := strconv.ParseUint(s, 8, 32)
	if err != nil || v > 0777 {
		return 0, fmt.Errorf("invalid mode")
	}
	return vfs.FileMode(v), nil
}

func init() { Register(Chmod{}) }
