// Copyright 2011 The Go Authors.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//modify 2013-2014 visualfc

package command

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"text/template"
	"unicode"
	"unicode/utf8"
)

// A Command is an implementation of a go command
// like go build or go fix.
type Command struct {
	// Run runs the command.
	// The args are the arguments after the command name.
	Run func(cmd *Command, args []string) error

	// UsageLine is the one-line usage message.
	// The first word in the line is taken to be the command name.
	UsageLine string

	// Short is the short description shown in the 'go help' output.
	Short string

	// Long is the long message shown in the 'go help <this-command>' output.
	Long string

	// Flag is a set of flags specific to this command.
	Flag flag.FlagSet

	// CustomFlags indicates that the command will do its own
	// flag parsing.
	CustomFlags bool

	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
}

// Name returns the command's name: the first word in the usage line.
func (c *Command) Name() string {
	name := c.UsageLine
	i := strings.Index(name, " ")
	if i >= 0 {
		name = name[:i]
	}
	return name
}

func (c *Command) Usage() {
	fmt.Fprintf(c.Stderr, "usage: %s %s\n", AppName, c.UsageLine)
	c.Flag.SetOutput(c.Stderr)
	c.Flag.PrintDefaults()
	//fmt.Fprintf(os.Stderr, "%s\n", strings.TrimSpace(c.Long))
	//os.Exit(2)
}

func (c *Command) PrintUsage() {
	fmt.Fprintf(c.Stderr, "usage: %s %s\n", AppName, c.UsageLine)
	c.Flag.SetOutput(c.Stderr)
	c.Flag.PrintDefaults()
}

// Runnable reports whether the command can be run; otherwise
// it is a documentation pseudo-command such as importpath.
func (c *Command) Runnable() bool {
	return c.Run != nil
}

func (c *Command) Println(args ...interface{}) {
	fmt.Fprintln(c.Stdout, args...)
}

func (c *Command) Printf(format string, args ...interface{}) {
	fmt.Fprintf(c.Stdout, format, args...)
}

var commands []*Command

func Register(cmd *Command) {
	commands = append(commands, cmd)
}

func CommandList() (cmds []string) {
	for _, cmd := range commands {
		cmds = append(cmds, cmd.Name())
	}
	return
}

var (
	Stdout io.Writer = os.Stdout
	Stderr io.Writer = os.Stderr
	Stdin  io.Reader = os.Stdin
)

func RunArgs(arguments []string, stdin io.Reader, stdout io.Writer, stderr io.Writer) error {
	flag.CommandLine.VisitAll(func(f *flag.Flag) {
		f.Value.Set(f.DefValue)
	})
	flag.CommandLine.Parse(arguments)
	args := flag.Args()
	if len(args) < 1 {
		printUsage(os.Stderr)
		return os.ErrInvalid
	}

	if len(args) == 1 && strings.TrimSpace(args[0]) == "" {
		printUsage(os.Stderr)
		return os.ErrInvalid
	}

	if args[0] == "help" {
		if !help(args[1:]) {
			return os.ErrInvalid
		}
		return nil
	}

	for _, cmd := range commands {
		if cmd.Name() == args[0] && cmd.Run != nil {
			cmd.Flag.VisitAll(func(f *flag.Flag) {
				f.Value.Set(f.DefValue)
			})
			cmd.Flag.Usage = func() { cmd.Usage() }
			cmd.Stdin = stdin
			cmd.Stdout = stdout
			cmd.Stderr = stderr
			if cmd.CustomFlags {
				args = args[1:]
			} else {
				err := cmd.Flag.Parse(args[1:])
				if err != nil {
					return err
				}
				args = cmd.Flag.Args()
			}
			return cmd.Run(cmd, args)
		}
	}

	fmt.Fprintf(os.Stderr, "%s: unknown subcommand %q\nRun '%s help' for usage.\n",
		AppName, args[0], AppName)
	return os.ErrInvalid
}

func Main() {
	flag.Usage = func() {
		printUsage(os.Stderr)
	}
	flag.Parse()
	log.SetFlags(0)

	args := flag.Args()
	if len(args) < 1 {
		flag.Usage()
		Exit(2)
	}

	if len(args) == 1 && strings.TrimSpace(args[0]) == "" {
		flag.Usage()
		Exit(2)
	}

	if args[0] == "help" {
		if !help(args[1:]) {
			os.Exit(2)
		}
		return
	}

	for _, cmd := range commands {
		if cmd.Name() == args[0] && cmd.Run != nil {
			cmd.Stdin = Stdin
			cmd.Stdout = Stdout
			cmd.Stderr = Stderr
			if cmd.CustomFlags {
				args = args[1:]
			} else {
				err := cmd.Flag.Parse(args[1:])
				if err != nil {
					Exit(2)
				}
				args = cmd.Flag.Args()
			}
			cmd.Flag.Usage = func() { cmd.Usage() }
			err := cmd.Run(cmd, args)
			if err != nil {
				fmt.Fprintln(cmd.Stderr, err)
				Exit(2)
			}
			Exit(0)
			return
		}
	}

	fmt.Fprintf(os.Stderr, "%s: unknown subcommand %q\nRun '%s help' for usage.\n",
		AppName, args[0], AppName)
	Exit(2)
}

var AppInfo string = "LiteIDE golang tool."
var AppName string = "tools"

var usageTemplate = `
Usage:

	{{AppName}} command [arguments]

The commands are:
{{range .}}{{if .Runnable}}
    {{.Name | printf "%-11s"}} {{.Short}}{{end}}{{end}}

Use "{{AppName}} help [command]" for more information about a command.

Additional help topics:
{{range .}}{{if not .Runnable}}
    {{.Name | printf "%-11s"}} {{.Short}}{{end}}{{end}}

Use "{{AppName}} help [topic]" for more information about that topic.

`

var helpTemplate = `{{if .Runnable}}usage: {{AppName}} {{.UsageLine}}

{{end}}{{.Long | trim}}
`

var documentationTemplate = `//
/*
{{range .}}{{if .Short}}{{.Short | capitalize}}

{{end}}{{if .Runnable}}Usage:

	{{AppName}} {{.UsageLine}}

{{end}}{{.Long | trim}}


{{end}}*/
package main
`

// tmpl executes the given template text on data, writing the result to w.
func tmpl(w io.Writer, text string, data interface{}) {
	t := template.New("top")
	t.Funcs(template.FuncMap{"trim": strings.TrimSpace, "capitalize": capitalize})
	template.Must(t.Parse(text))
	if err := t.Execute(w, data); err != nil {
		panic(err)
	}
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	r, n := utf8.DecodeRuneInString(s)
	return string(unicode.ToTitle(r)) + s[n:]
}

func printUsage(w io.Writer) {
	if len(AppInfo) > 0 {
		fmt.Fprintln(w, AppInfo)
	}
	tmpl(w, strings.Replace(usageTemplate, "{{AppName}}", AppName, -1), commands)
}

// help implements the 'help' command.
func help(args []string) bool {
	if len(args) == 0 {
		printUsage(os.Stdout)
		// not exit 2: succeeded at 'go help'.
		return true
	}
	if len(args) != 1 {
		fmt.Fprintf(os.Stderr, "usage: %s help command\n\nToo many arguments given.\n", AppName)
		return false
	}

	arg := args[0]

	// 'go help documentation' generates doc.go.
	if arg == "documentation" {
		buf := new(bytes.Buffer)
		printUsage(buf)
		usage := &Command{Long: buf.String()}
		tmpl(os.Stdout, strings.Replace(documentationTemplate, "{{AppName}}", AppName, -1), append([]*Command{usage}, commands...))
		return false
	}

	for _, cmd := range commands {
		if cmd.Name() == arg {
			tmpl(os.Stdout, strings.Replace(helpTemplate, "{{AppName}}", AppName, -1), cmd)
			// not exit 2: succeeded at 'go help cmd'.
			return true
		}
	}

	fmt.Fprintf(os.Stderr, "Unknown help topic %#q.  Run '%s help'.\n", arg, AppName)
	return false
}

func Exit(code int) {
	os.Exit(code)
}
