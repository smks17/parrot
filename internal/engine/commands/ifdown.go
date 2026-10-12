package commands

type Ifdown struct{}

var _ Command = Ifdown{}

func (Ifdown) Name() string  { return "ifdown" }
func (Ifdown) Usage() string { return "ifdown IFACE  — take a network interface down" }

func (c Ifdown) Run(ctx *Context, args []string) int {
	return setLink(ctx, c, args, false)
}

func init() { Register(Ifdown{}) }
