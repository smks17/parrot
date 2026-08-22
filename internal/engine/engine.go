package engine

import (
	"bytes"
	"fmt"
	"parrot/internal/engine/commands"
	"parrot/internal/engine/parser"
	"parrot/internal/engine/vfs"
	"slices"
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

	fields, err := parser.Parse(line)
	if err != nil {
		return Result{
			Stderr:   fmt.Sprintf("%s: %v\n", ShellName, err),
			ExitCode: 2,
			Cwd:      s.vfs.Cwd(),
		}
	}
	if len(fields) == 0 {
		// A bare Enter is not an error, and it is not a command either.
		return Result{Cwd: s.vfs.Cwd()}
	}

	name, fields := fields[0], fields[1:]

	var redirect *Redirect
	for i, arg := range fields {
		if arg.Kind != parser.TokenKind(parser.Redirect) {
			continue
		}
		if i != len(fields)-2 {
			return Result{
				Stderr:   fmt.Sprintf("%s: %s: syntax error near unexpected token `>'\n", ShellName, name.Value),
				ExitCode: 2,
				Cwd:      s.vfs.Cwd(),
			}
		}
		file := fields[i+1]
		node, err := s.vfs.Resolve(file.Value)
		if err != nil {
			if err = s.vfs.Create(file.Value); err == nil {
				node, err = s.vfs.Resolve(file.Value)
			}
		}
		if err != nil {
			return Result{
				Stderr:   fmt.Sprintf("%s: %s: %v\n", ShellName, file.Value, err),
				ExitCode: 1,
				Cwd:      s.vfs.Cwd(),
			}
		}
		redirect = &Redirect{node}
		fields = fields[:i]
		break
	}

	s.history = append(s.history, line)

	cmd, exist := commands.Lookup(name.Value)
	if !exist {
		return Result{
			Stderr:   fmt.Sprintf("%s: %s: command not found\n", ShellName, name.Value),
			ExitCode: exitNotFound,
			Cwd:      s.vfs.Cwd(),
		}
	}

	var stdout, stderr bytes.Buffer
	args := make([]string, len(fields))
	for i, arg := range fields {
		args[i] = arg.Value
	}
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
