package commands

type True struct{}

var _ Command = True{}

func (true True) Name() string {
	return "true"
}

func (true True) Usage() string { return "true  — return 0" }

func (true True) Run(ctx *Context, args []string) int {
	return 0
}

func init() { Register(True{}) }
