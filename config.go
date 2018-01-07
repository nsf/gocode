package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"reflect"
	"regexp"
	"strconv"
)

//-------------------------------------------------------------------------
// config
//
// Structure represents persistent config storage of the gocode daemon. Usually
// the config is located somewhere in ~/.config/gocode directory.
//-------------------------------------------------------------------------

type config struct {
	ProposeBuiltins    bool   `json:"propose-builtins"`
	LibPath            string `json:"lib-path"`
	CustomPkgPrefix    string `json:"custom-pkg-prefix"`
	CustomVendorDir    string `json:"custom-vendor-dir"`
	Autobuild          bool   `json:"autobuild"`
	ForceDebugOutput   string `json:"force-debug-output"`
	PackageLookupMode  string `json:"package-lookup-mode"`
	CloseTimeout       int    `json:"close-timeout"`
	UnimportedPackages bool   `json:"unimported-packages"`
	Partials           bool   `json:"partials"`
	IgnoreCase         bool   `json:"ignore-case"`
	ClassFiltering     bool   `json:"class-filtering"`
}

var g_config_desc = map[string]string{
	"propose-builtins":    "If set to {true}, gocode will add built-in types, functions and constants to autocompletion proposals.",
	"lib-path":            "A string option. Allows you to add search paths for packages. By default, gocode only searches {$GOPATH/pkg/$GOOS_$GOARCH} and {$GOROOT/pkg/$GOOS_$GOARCH} in terms of previously existed environment variables. Also you can specify multiple paths using ':' (colon) as a separator (on Windows use semicolon ';'). The paths specified by {lib-path} are prepended to the default ones.",
	"custom-pkg-prefix":   "",
	"custom-vendor-dir":   "",
	"autobuild":           "If set to {true}, gocode will try to automatically build out-of-date packages when their source files are modified, in order to obtain the freshest autocomplete results for them. This feature is experimental.",
	"force-debug-output":  "If is not empty, gocode will forcefully redirect the logging into that file. Also forces enabling of the debug mode on the server side.",
	"package-lookup-mode": "If set to {go}, use standard Go package lookup rules. If set to {gb}, use gb-specific lookup rules. See {https://github.com/constabulary/gb} for details.",
	"close-timeout":       "If there have been no completion requests after this number of seconds, the gocode process will terminate. Default is 30 minutes.",
	"unimported-packages": "If set to {true}, gocode will try to import certain known packages automatically for identifiers which cannot be resolved otherwise. Currently only a limited set of standard library packages is supported.",
	"partials":            "If set to {false}, gocode will not filter autocompletion results based on entered prefix before the cursor. Instead it will return all available autocompletion results viable for a given context. Whether this option is set to {true} or {false}, gocode will return a valid prefix length for output formats which support it. Setting this option to a non-default value may result in editor misbehaviour.",
	"ignore-case":         "If set to {true}, gocode will perform case-insensitive matching when doing prefix-based filtering.",
	"class-filtering":     "Enables or disables gocode's feature where it performs class-based filtering if partial input matches corresponding class keyword: const, var, type, func, package.",
}

var g_default_config = config{
	ProposeBuiltins:    false,
	LibPath:            "",
	CustomPkgPrefix:    "",
	Autobuild:          false,
	ForceDebugOutput:   "",
	PackageLookupMode:  "go",
	CloseTimeout:       1800,
	UnimportedPackages: false,
	Partials:           true,
	IgnoreCase:         false,
	ClassFiltering:     true,
}
var g_config = g_default_config

var g_string_to_bool = map[string]bool{
	"t":     true,
	"true":  true,
	"y":     true,
	"yes":   true,
	"on":    true,
	"1":     true,
	"f":     false,
	"false": false,
	"n":     false,
	"no":    false,
	"off":   false,
	"0":     false,
}

func set_value(v reflect.Value, value string) {
	switch t := v; t.Kind() {
	case reflect.Bool:
		v, ok := g_string_to_bool[value]
		if ok {
			t.SetBool(v)
		}
	case reflect.String:
		t.SetString(value)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		v, err := strconv.ParseInt(value, 10, 64)
		if err == nil {
			t.SetInt(v)
		}
	case reflect.Float32, reflect.Float64:
		v, err := strconv.ParseFloat(value, 64)
		if err == nil {
			t.SetFloat(v)
		}
	}
}

func list_value(v reflect.Value, name string, w io.Writer) {
	switch t := v; t.Kind() {
	case reflect.Bool:
		fmt.Fprintf(w, "%s %v\n", name, t.Bool())
	case reflect.String:
		fmt.Fprintf(w, "%s \"%v\"\n", name, t.String())
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		fmt.Fprintf(w, "%s %v\n", name, t.Int())
	case reflect.Float32, reflect.Float64:
		fmt.Fprintf(w, "%s %v\n", name, t.Float())
	}
}

func (this *config) list() string {
	str, typ := this.value_and_type()
	buf := bytes.NewBuffer(make([]byte, 0, 256))
	for i := 0; i < str.NumField(); i++ {
		v := str.Field(i)
		name := typ.Field(i).Tag.Get("json")
		list_value(v, name, buf)
	}
	return buf.String()
}

func (this *config) list_option(name string) string {
	str, typ := this.value_and_type()
	buf := bytes.NewBuffer(make([]byte, 0, 256))
	for i := 0; i < str.NumField(); i++ {
		v := str.Field(i)
		nm := typ.Field(i).Tag.Get("json")
		if nm == name {
			list_value(v, name, buf)
		}
	}
	return buf.String()
}

func (this *config) set_option(name, value string) string {
	str, typ := this.value_and_type()
	buf := bytes.NewBuffer(make([]byte, 0, 256))
	for i := 0; i < str.NumField(); i++ {
		v := str.Field(i)
		nm := typ.Field(i).Tag.Get("json")
		if nm == name {
			set_value(v, value)
			list_value(v, name, buf)
		}
	}
	this.write()
	return buf.String()

}

func (this *config) value_and_type() (reflect.Value, reflect.Type) {
	v := reflect.ValueOf(this).Elem()
	return v, v.Type()
}

func (this *config) write() error {
	data, err := json.Marshal(this)
	if err != nil {
		return err
	}

	// make sure config dir exists
	dir := config_dir()
	if !file_exists(dir) {
		os.MkdirAll(dir, 0755)
	}

	f, err := os.Create(config_file())
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = f.Write(data)
	if err != nil {
		return err
	}

	return nil
}

func (this *config) read() error {
	data, err := ioutil.ReadFile(config_file())
	if err != nil {
		return err
	}

	err = json.Unmarshal(data, this)
	if err != nil {
		return err
	}

	return nil
}

func quoted(v interface{}) string {
	switch v.(type) {
	case string:
		return fmt.Sprintf("%q", v)
	case int:
		return fmt.Sprint(v)
	case bool:
		return fmt.Sprint(v)
	default:
		panic("unreachable")
	}
}

var descRE = regexp.MustCompile(`{[^}]+}`)

func preprocess_desc(v string) string {
	return descRE.ReplaceAllStringFunc(v, func(v string) string {
		return color_cyan + v[1:len(v)-1] + color_none
	})
}

func (this *config) options() string {
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "%sConfig file location%s: %s\n", color_white_bold, color_none, config_file())
	dv := reflect.ValueOf(g_default_config)
	v, t := this.value_and_type()
	for i, n := 0, t.NumField(); i < n; i++ {
		f := t.Field(i)
		index := f.Index
		tag := f.Tag.Get("json")
		fmt.Fprintf(&buf, "\n%s%s%s\n", color_yellow_bold, tag, color_none)
		fmt.Fprintf(&buf, "%stype%s: %s\n", color_yellow, color_none, f.Type)
		fmt.Fprintf(&buf, "%svalue%s: %s\n", color_yellow, color_none, quoted(v.FieldByIndex(index).Interface()))
		fmt.Fprintf(&buf, "%sdefault%s: %s\n", color_yellow, color_none, quoted(dv.FieldByIndex(index).Interface()))
		fmt.Fprintf(&buf, "%sdescription%s: %s\n", color_yellow, color_none, preprocess_desc(g_config_desc[tag]))
	}

	return buf.String()
}
