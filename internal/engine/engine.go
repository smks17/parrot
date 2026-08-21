package engine

import (
	"bytes"
	"fmt"
	"parrot/internal/engine/commands"
	"parrot/internal/engine/vfs"
	"slices"
	"strings"
)

const ShellName = "prt"

const exitNotFound = 127

type Session struct {
	vfs     *vfs.VFS
	env     map[string]string
	history []string
}

func NewSession() *Session {
	return &Session{
		vfs: vfs.New(),
		env: map[string]string{},
	}
}

type Result struct {
	Stdout   string
	Stderr   string
	ExitCode int    // Unix convention: 0 is success, non-zero is failure
	Cwd      string // lets the UI render the prompt without a second call
}

type Redirect struct {
	File *vfs.Node
}

func (s *Session) Execute(line string) Result {

	fields := strings.Fields(line)
	if len(fields) == 0 {
		// A bare Enter is not an error, and it is not a command either.
		return Result{Cwd: s.vfs.Cwd()}
	}

	name, args := fields[0], fields[1:]

	var redirect *Redirect
	for i, arg := range args {
		if arg != ">" {
			continue
		}
		if i != len(args)-2 {
			return Result{
				Stderr:   fmt.Sprintf("%s: %s: syntax error near unexpected token `>'\n", ShellName, name),
				ExitCode: 2,
				Cwd:      s.vfs.Cwd(),
			}
		}
		file := args[i+1]
		node, err := s.vfs.Resolve(file)
		if err != nil {
			if err = s.vfs.Create(file); err == nil {
				node, err = s.vfs.Resolve(file)
			}
		}
		if err != nil {
			return Result{
				Stderr:   fmt.Sprintf("%s: %s: %v\n", ShellName, file, err),
				ExitCode: 1,
				Cwd:      s.vfs.Cwd(),
			}
		}
		redirect = &Redirect{node}
		args = args[:i]
		break
	}

	s.history = append(s.history, line)

	cmd, exist := commands.Lookup(name)
	if !exist {
		return Result{
			Stderr:   fmt.Sprintf("%s: %s: command not found\n", ShellName, name),
			ExitCode: exitNotFound,
			Cwd:      s.vfs.Cwd(),
		}
	}

	var stdout, stderr bytes.Buffer
	code := cmd.Run(&commands.Context{
		VFS:     s.vfs,
		Stdout:  &stdout,
		Stderr:  &stderr,
		Env:     s.env,
		History: slices.Clone(s.history),
	}, args)

	if redirect != nil {
		stdout := stdout.String()
		stderr := stderr.String()
		redirect.File.Override([]byte(stdout + stderr))
		return Result{
			ExitCode: 0,
			Cwd:      s.vfs.Cwd(),
		}
	}
	return Result{
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		ExitCode: code,
		Cwd:      s.vfs.Cwd(),
	}
}
