package commands

import "fmt"

// homeDir is where a bare "cd" lands, matching the seed tree in vfs.New.
const homeDir = "/home/friend"

type Cd struct{}

var _ Command = Cd{}

func (cd Cd) Name() string {
	return "cd"
}

func (cd Cd) Usage() string { return "cd [path]  — change the working directory" }

func (cd Cd) Run(ctx *Context, args []string) int {
	path := homeDir
	if len(args) > 0 {
		path = args[0]
	}

	if err := ctx.VFS.Chdir(path); err != nil {
		fmt.Fprintf(ctx.Stderr, "%s: %s: %v\n", cd.Name(), path, err)
		return 1
	}
	return 0
}

func init() { Register(Cd{}) }
