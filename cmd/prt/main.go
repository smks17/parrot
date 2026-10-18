package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"net/netip"
	"os"
	"os/signal"
	"strconv"
	"strings"

	"parrot/internal/engine"
	"parrot/internal/engine/hostnet"
	"parrot/internal/engine/osfs"
)

func main() {
	onDisk := flag.Bool("real", false, "work on the real filesystem instead of the in-memory one")
	hostNetwork := flag.Bool("net", false, "give the machine the host's network as eth0")
	var loads loadList
	flag.Var(&loads, "load", "copy a host file into the machine before it starts, as `host:path` (repeatable)")
	var published publishFlags
	flag.Var(&published, "p", "publish a port: `[hostIP:]hostPort:port` (repeatable; hostIP defaults to 127.0.0.1)")
	flag.Usage = usage
	flag.Parse()

	app, err := start(*onDisk)
	if err == nil && *hostNetwork {
		err = attachHostNetwork(app)
	}
	for _, spec := range published {
		if err != nil {
			break
		}
		err = publish(app, spec)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", engine.ShellName, err)
		os.Exit(1)
	}
	if err := loads.into(app); err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", engine.ShellName, err)
		os.Exit(1)
	}
	app.SetBackgroundOutput(os.Stdout, os.Stderr)

	// A named file is run as a script, with the rest of the arguments as $1
	if args := flag.Args(); len(args) > 0 {
		os.Exit(runFile(app, args[0], args[1:]))
	}
	os.Exit(repl(app, *onDisk))
}

// loadList is the -load flags: host files to copy into the machine. Reading
// the host is this front end's job, never the engine's, so the engine only
// ever sees the bytes.
type loadList []string

func (l *loadList) String() string { return strings.Join(*l, ",") }

func (l *loadList) Set(v string) error {
	if host, path, ok := strings.Cut(v, ":"); !ok || host == "" || path == "" {
		return fmt.Errorf("want host:path, got %q", v)
	}
	*l = append(*l, v)
	return nil
}

func (l loadList) into(app *engine.App) error {
	for _, v := range l {
		host, path, _ := strings.Cut(v, ":")
		data, err := os.ReadFile(host)
		if err != nil {
			return err
		}
		if result := app.Upload(path, data); result.ExitCode != 0 {
			return fmt.Errorf("%s", strings.TrimSpace(result.Stderr))
		}
	}
	return nil
}

func usage() {
	fmt.Fprintf(os.Stderr, "usage: %s [-real] [-net] [-p [hostIP:]hostPort:port]... [-load host:path]... [script [args...]]\n\n", engine.ShellName)
	flag.PrintDefaults()
}

// attachHostNetwork plugs the host's own network into the machine as eth0, with
// the host's address on it, and sends everything lo does not cover through it
func attachHostNetwork(app *engine.App) error {
	nm := app.Network()
	nm.AddInterface("eth0", hostnet.Net{})
	// A host with no network yet still gets eth0, only without an address.
	if addr, err := hostnet.Addr(); err == nil {
		if err := nm.AddAddress("eth0", addr); err != nil {
			return err
		}
	}
	return nm.SetDefaultRoute("eth0")
}

// publishFlags collects every -p, so a port can be published more than once.
type publishFlags []string

func (p *publishFlags) String() string     { return strings.Join(*p, ",") }
func (p *publishFlags) Set(v string) error { *p = append(*p, v); return nil }

func publish(app *engine.App, spec string) error {
	parts := strings.Split(spec, ":")
	hostIP := "127.0.0.1"
	switch len(parts) {
	case 2:
	case 3:
		hostIP, parts = parts[0], parts[1:]
	default:
		return fmt.Errorf("-p %s: want [hostIP:]hostPort:port", spec)
	}
	ip, err := netip.ParseAddr(hostIP)
	if err != nil {
		return fmt.Errorf("-p %s: invalid host IP %q", spec, hostIP)
	}
	hostPort, err := strconv.ParseUint(parts[0], 10, 16)
	if err != nil {
		return fmt.Errorf("-p %s: invalid host port %q", spec, parts[0])
	}
	port, err := strconv.ParseUint(parts[1], 10, 16)
	if err != nil {
		return fmt.Errorf("-p %s: invalid port %q", spec, parts[1])
	}
	if err := app.Network().Publish(hostnet.Net{}, ip, uint16(hostPort), uint16(port)); err != nil {
		return fmt.Errorf("-p %s: %w", spec, err)
	}
	return nil
}

// start builds the shell over whichever filesystem was asked for.
func start(onDisk bool) (*engine.App, error) {
	if !onDisk {
		return engine.NewApp(), nil
	}

	filesystem, err := osfs.New()
	if err != nil {
		return nil, err
	}

	app := engine.NewAppOn(filesystem)
	return app, nil
}

func runFile(app *engine.App, path string, args []string) int {
	src, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", engine.ShellName, err)
		return 127
	}
	return report(interruptible(app, func(ctx context.Context) engine.Result {
		return app.RunScriptStream(ctx, string(src), args, os.Stdout, os.Stderr)
	}))
}

func repl(app *engine.App, onDisk bool) int {
	scanner := bufio.NewScanner(os.Stdin)
	status := 0

	// On the real filesystem there is nobody to log in as: the shell already
	// runs as the account that started it, and the machine's own accounts are
	// not the in-memory world's.
	if !onDisk && !login(app, scanner) {
		return 1
	}

	for {
		sigil := "$"
		if app.User() == "root" {
			sigil = "#"
		}
		fmt.Printf("%s%s ", app.User(), sigil)
		// Scan reports false for both the end of input and a read error; the
		// error itself is checked after the loop.
		if !scanner.Scan() {
			fmt.Println()
			break
		}

		line := scanner.Text()
		result := interruptible(app, func(ctx context.Context) engine.Result {
			return app.ExecuteStream(ctx, line, os.Stdout, os.Stderr)
		})
		status = report(result)
		if result.Exited {
			return status
		}
	}

	if err := scanner.Err(); err != nil {
		fmt.Fprintln(os.Stderr, engine.ShellName+": read error:", err)
		return 1
	}
	return status
}

// interruptible runs one piece of input with Ctrl+C wired to cancel it, and
// Ctrl+Z, where the host has it, to stop it.
func interruptible(app *engine.App, run func(ctx context.Context) engine.Result) engine.Result {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)
	defer signal.Stop(sig)

	suspend := make(chan os.Signal, 1)
	if len(suspendSignals) > 0 {
		signal.Notify(suspend, suspendSignals...)
		defer signal.Stop(suspend)
	}

	done := make(chan struct{})
	defer close(done)
	go func() {
		for {
			select {
			case <-sig:
				cancel()
				return
			case <-suspend:
				app.Suspend() // the command stops; the line goes on without it
			case <-done:
				return
			}
		}
	}()

	return run(ctx)
}

// report echoes what a terminal shows for an interrupted command
func report(result engine.Result) int {
	if result.Interrupted {
		fmt.Fprintln(os.Stdout, "^C")
	}
	return result.ExitCode
}

func login(app *engine.App, scanner *bufio.Scanner) bool {
	for {
		fmt.Printf("%s login: ", engine.ShellName)
		if !scanner.Scan() {
			fmt.Println()
			return false
		}
		name := strings.TrimSpace(scanner.Text())
		if name == "" {
			continue
		}
		fmt.Print("Password: ")
		if !scanner.Scan() {
			fmt.Println()
			return false
		}
		if err := app.Login(name, scanner.Text()); err != nil {
			fmt.Fprintln(os.Stderr, "Login incorrect")
			continue
		}
		return true
	}
}
