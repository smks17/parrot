package commands

type False struct{}

var _ Command = False{}

func (false False) Name() string {
	return "false"
}

func (false False) Usage() string { return "false  — return 1" }

func (false False) Run(ctx *Context, args []string) int {
	return 1
}

func init() { Register(False{}) }
