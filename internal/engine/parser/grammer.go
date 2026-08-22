package parser

import (
	"parrot/internal/engine/commands"
	"parrot/internal/engine/vfs"
)

type RedirectFile struct {
	File   *vfs.Node
	Append bool // true for >>, false for >
}

type SimpleCommand struct {
	Cmd      commands.Command
	Args     []string
	Redirect *RedirectFile // nil if none
}

func NewSimpleCommand(cmd commands.Command, args []string, redirect *RedirectFile) *SimpleCommand {
	return &SimpleCommand{cmd, args, redirect}
}

type Pipeline struct {
	Commands []SimpleCommand
}

func NewPipeline(commands []SimpleCommand) *Pipeline {
	return &Pipeline{Commands: commands}
}

func (p *Pipeline) AddCommand(command SimpleCommand) {
	p.Commands = append(p.Commands, command)
}

type AndList struct {
	Pipelines []Pipeline
}

func NewAndList(pipeline []Pipeline) *AndList {
	return &AndList{pipeline}
}

func (a *AndList) AddPipeline(pipeline Pipeline) {
	a.Pipelines = append(a.Pipelines, pipeline)
}
