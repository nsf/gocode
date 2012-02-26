package main

import (
	"bytes"
	"flag"
	"fmt"
	"io/ioutil"
	"net/rpc"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

var (
	g_is_server = flag.Bool("s", false, "run a server instead of a client")
	g_format    = flag.String("f", "nice", "output format (vim | emacs | nice | csv | json)")
	g_input     = flag.String("in", "", "use this file instead of stdin input")
	g_sock      = create_sock_flag("sock", "socket type (unix | tcp)")
	g_addr      = flag.String("addr", "localhost:37373", "address for tcp socket")
)

// TODO: find a better place for this function
// returns truncated 'data' and amount of bytes skipped (for cursor pos adjustment)
func filter_out_shebang(data []byte) ([]byte, int) {
	if len(data) > 2 && data[0] == '#' && data[1] == '!' {
		newline := bytes.Index(data, []byte("\n"))
		if newline != -1 && len(data) > newline+1 {
			return data[newline+1:], newline + 1
		}
	}
	return data, 0
}

//-------------------------------------------------------------------------
// formatter interfaces
//-------------------------------------------------------------------------

type formatter interface {
	write_candidates(candidates []candidate, num int)
}

//-------------------------------------------------------------------------
// nice_formatter (just for testing, simple textual output)
//-------------------------------------------------------------------------

type nice_formatter struct{}

func (*nice_formatter) write_candidates(candidates []candidate, num int) {
	if candidates == nil {
		fmt.Printf("Nothing to complete.\n")
		return
	}

	fmt.Printf("Found %d candidates:\n", len(candidates))
	for _, c := range candidates {
		abbr := fmt.Sprintf("%s %s %s", c.Class, c.Name, c.Type)
		if c.Class == decl_func {
			abbr = fmt.Sprintf("%s %s%s", c.Class, c.Name, c.Type[len("func"):])
		}
		fmt.Printf("  %s\n", abbr)
	}
}

//-------------------------------------------------------------------------
// vim_formatter
//-------------------------------------------------------------------------

type vim_formatter struct{}

func (*vim_formatter) write_candidates(candidates []candidate, num int) {
	if candidates == nil {
		fmt.Print("[0, []]")
		return
	}

	fmt.Printf("[%d, [", num)
	for i, c := range candidates {
		if i != 0 {
			fmt.Printf(", ")
		}

		word := c.Name
		if c.Class == decl_func {
			word += "("
			if strings.HasPrefix(c.Type, "func()") {
				word += ")"
			}
		}

		abbr := fmt.Sprintf("%s %s %s", c.Class, c.Name, c.Type)
		if c.Class == decl_func {
			abbr = fmt.Sprintf("%s %s%s", c.Class, c.Name, c.Type[len("func"):])
		}
		fmt.Printf("{'word': '%s', 'abbr': '%s'}", word, abbr)
	}
	fmt.Printf("]]")
}

//-------------------------------------------------------------------------
// emacs_formatter
//-------------------------------------------------------------------------

type emacs_formatter struct{}

func (*emacs_formatter) write_candidates(candidates []candidate, num int) {
	for _, c := range candidates {
		hint := c.Class.String() + " " + c.Type
		if c.Class == decl_func {
			hint = c.Type
		}
		fmt.Printf("%s,,%s\n", c.Name, hint)
	}
}

//-------------------------------------------------------------------------
// csv_formatter
//-------------------------------------------------------------------------

type csv_formatter struct{}

func (*csv_formatter) write_candidates(candidates []candidate, num int) {
	for _, c := range candidates {
		fmt.Printf("%s,,%s,,%s\n", c.Class, c.Name, c.Type)
	}
}

//-------------------------------------------------------------------------
// json_formatter
//-------------------------------------------------------------------------

type json_formatter struct{}

func (*json_formatter) write_candidates(candidates []candidate, num int) {
	if candidates == nil {
		fmt.Print("[]")
		return
	}

	fmt.Printf(`[%d, [`, num)
	for i, c := range candidates {
		if i != 0 {
			fmt.Printf(", ")
		}
		fmt.Printf(`{"class": "%s", "name": "%s", "type": "%s"}`,
			c.Class, c.Name, c.Type)
	}
	fmt.Print("]]")
}

//-------------------------------------------------------------------------

func get_formatter() formatter {
	switch *g_format {
	case "vim":
		return new(vim_formatter)
	case "emacs":
		return new(emacs_formatter)
	case "nice":
		return new(nice_formatter)
	case "csv":
		return new(csv_formatter)
	case "json":
		return new(json_formatter)
	}
	return new(vim_formatter)
}

func get_socket_filename() string {
	user := os.Getenv("USER")
	if user == "" {
		user = "all"
	}
	return fmt.Sprintf("%s/gocode-daemon.%s", os.TempDir(), user)
}

func file_exists(filename string) bool {
	_, err := os.Stat(filename)
	if err != nil {
		return false
	}
	return true
}

func do_server() int {
	g_config.read()

	addr := *g_addr
	if *g_sock == "unix" {
		addr = get_socket_filename()
		if file_exists(addr) {
			fmt.Printf("unix socket: '%s' already exists\n", addr)
			return 1
		}
	}
	g_daemon = new_daemon(*g_sock, addr)
	if *g_sock == "unix" {
		// cleanup unix socket file
		defer os.Remove(addr)
	}

	rpc.Register(new(RPC))

	g_daemon.loop()
	return 0
}

func cmd_status(c *rpc.Client) {
	fmt.Printf("%s\n", client_status(c, 0))
}

func cmd_auto_complete(c *rpc.Client) {
	var file []byte
	var err error

	if *g_input != "" {
		file, err = ioutil.ReadFile(*g_input)
	} else {
		file, err = ioutil.ReadAll(os.Stdin)
	}

	if err != nil {
		panic(err.Error())
	}

	var skipped int
	file, skipped = filter_out_shebang(file)

	filename := *g_input
	cursor := -1

	offset := ""
	switch flag.NArg() {
	case 2:
		offset = flag.Arg(1)
	case 3:
		filename = flag.Arg(1) // Override default filename
		offset = flag.Arg(2)
	}

	if offset != "" {
		if offset[0] == 'c' || offset[0] == 'C' {
			cursor, _ = strconv.Atoi(offset[1:])
			cursor = char_to_byte_offset(file, cursor)
		} else {
			cursor, _ = strconv.Atoi(offset)
		}
	}

	cursor -= skipped
	if filename != "" && !filepath.IsAbs(filename) {
		cwd, _ := os.Getwd()
		filename = filepath.Join(cwd, filename)
	}

	get_formatter().write_candidates(client_auto_complete(c, file, filename, cursor))
}

func cmd_close(c *rpc.Client) {
	client_close(c, 0)
}

func cmd_drop_cache(c *rpc.Client) {
	client_drop_cache(c, 0)
}

func cmd_set(c *rpc.Client) {
	switch flag.NArg() {
	case 1:
		fmt.Print(client_set(c, "\x00", "\x00"))
	case 2:
		fmt.Print(client_set(c, flag.Arg(1), "\x00"))
	case 3:
		fmt.Print(client_set(c, flag.Arg(1), flag.Arg(2)))
	}
}

func try_run_server() error {
	path := get_executable_filename()
	args := []string{os.Args[0], "-s", "-sock", *g_sock, "-addr", *g_addr}
	cwd, _ := os.Getwd()
	procattr := os.ProcAttr{Dir: cwd, Env: os.Environ(), Files: []*os.File{nil, nil, nil}}
	p, err := os.StartProcess(path, args, &procattr)

	if err != nil {
		return err
	}
	return p.Release()
}

func try_to_connect(network, address string) (client *rpc.Client, err error) {
	t := 0
	for {
		client, err = rpc.Dial(network, address)
		if err != nil && t < 1000 {
			time.Sleep(10 * time.Millisecond)
			t += 10
			continue
		}
		break
	}

	return
}

func do_client() int {
	addr := *g_addr
	if *g_sock == "unix" {
		addr = get_socket_filename()
	}

	// client
	client, err := rpc.Dial(*g_sock, addr)
	if err != nil {
		if *g_sock == "unix" && file_exists(addr) {
			os.Remove(addr)
		}

		err = try_run_server()
		if err != nil {
			fmt.Printf("%s\n", err.Error())
			return 1
		}
		client, err = try_to_connect(*g_sock, addr)
		if err != nil {
			fmt.Printf("%s\n", err.Error())
			return 1
		}
	}
	defer client.Close()

	if flag.NArg() > 0 {
		switch flag.Arg(0) {
		case "autocomplete":
			cmd_auto_complete(client)
		case "close":
			cmd_close(client)
		case "status":
			cmd_status(client)
		case "drop-cache":
			cmd_drop_cache(client)
		case "set":
			cmd_set(client)
		}
	}
	return 0
}

func char_to_byte_offset(s []byte, offset_c int) (offset_b int) {
	for offset_b = 0; offset_c > 0 && offset_b < len(s); offset_b++ {
		if utf8.RuneStart(s[offset_b]) {
			offset_c--
		}
	}
	return offset_b
}

func main() {
	flag.Parse()

	var retval int
	if *g_is_server {
		retval = do_server()
	} else {
		retval = do_client()
	}
	os.Exit(retval)
}
