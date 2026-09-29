package commands

const (
	EnvUser = "USER"
	EnvHome = "HOME"
)

func (ctx *Context) Home() string {
	if home := ctx.Env[EnvHome]; home != "" {
		return home
	}
	return ctx.User.Home
}
