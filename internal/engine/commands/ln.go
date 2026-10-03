package commands

import "fmt"

type Ln struct{}

var _ Command = Ln{}

func (ln Ln) Name() string { return "ln" }

func (ln Ln) Usage() string {
	return "ln <target> <name>  — give an existing file a second name"
}

// Run is link(2): a second entry pointing at one inode. Nothing is copied, so
// the two names are not two files — "ls -i" shows one inode number and a link
// count of 2, and writing through either is writing the same bytes.
func (ln Ln) Run(ctx *Context, args []string) int {
	var operands []string
	for _, arg := range args {
		// -s is the obvious next flag, but a symlink is a different kind of
		// inode and there is no such kind yet. Refusing beats pretending.
		if arg == "-s" || arg == "--symbolic" {
			fmt.Fprintf(ctx.Stderr, "%s: symbolic links are not supported yet\n", ln.Name())
			return 1
		}
		operands = append(operands, arg)
	}

	if len(operands) < 2 {
		fmt.Fprintf(ctx.Stderr, "%s: missing operand\n", ln.Name())
		return 1
	}
	target, name := operands[0], operands[1]

	if err := ctx.VFS.Link(target, name); err != nil {
		fmt.Fprintf(ctx.Stderr, "%s: %s: %v\n", ln.Name(), name, err)
		return 1
	}
	return 0
}

func init() { Register(Ln{}) }
