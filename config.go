package main

import (
	cfg "./configfile"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
)

//-------------------------------------------------------------------------
// Config
//
// Structure represents persistent config storage of the gocode daemon. Usually
// the config is located somewhere in ~/.config/gocode directory.
//-------------------------------------------------------------------------

var Config = struct {
	ProposeBuiltins bool   "propose-builtins"
	LibPath         string "lib-path"
}{
	false,
	"",
}

func setValue(v reflect.Value, name, value string) {
	switch t := v; t.Kind() {
	case reflect.Bool:
		v, ok := cfg.BoolStrings[value]
		if ok {
			t.SetBool(v)
		}
	case reflect.String:
		t.SetString(value)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		v, err := strconv.Atoi64(value)
		if err == nil {
			t.SetInt(v)
		}
	case reflect.Float32, reflect.Float64:
		v, err := strconv.Atof64(value)
		if err == nil {
			t.SetFloat(v)
		}
	}
}

func listValue(v reflect.Value, name string, w io.Writer) {
	switch t := v; t.Kind() {
	case reflect.Bool:
		fmt.Fprintf(w, "%s = %v\n", name, t.Bool())
	case reflect.String:
		fmt.Fprintf(w, "%s = \"%v\"\n", name, t.String())
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		fmt.Fprintf(w, "%s = %v\n", name, t.Int())
	case reflect.Float32, reflect.Float64:
		fmt.Fprintf(w, "%s = %v\n", name, t.Float())
	}
}

func listConfig(v interface{}) string {
	str, typ, ok := interfaceIsPtrStruct(v)
	if !ok {
		return ""
	}

	buf := bytes.NewBuffer(make([]byte, 0, 256))
	for i := 0; i < str.NumField(); i++ {
		v := str.Field(i)
		name := typ.Field(i).Tag
		listValue(v, string(name), buf)
	}
	return buf.String()
}

func listOption(v interface{}, name string) string {
	str, typ, ok := interfaceIsPtrStruct(v)
	if !ok {
		return ""
	}

	buf := bytes.NewBuffer(make([]byte, 0, 256))
	for i := 0; i < str.NumField(); i++ {
		v := str.Field(i)
		nm := typ.Field(i).Tag
		if string(nm) == name {
			listValue(v, name, buf)
		}
	}
	return buf.String()
}

func setOption(v interface{}, name, value string) string {
	str, typ, ok := interfaceIsPtrStruct(v)
	if !ok {
		return ""
	}

	buf := bytes.NewBuffer(make([]byte, 0, 256))
	for i := 0; i < str.NumField(); i++ {
		v := str.Field(i)
		nm := typ.Field(i).Tag
		if string(nm) == name {
			setValue(v, name, value)
			listValue(v, name, buf)
		}
	}
	writeConfig(v)
	return buf.String()
}

func interfaceIsPtrStruct(v interface{}) (reflect.Value, reflect.Type, bool) {
	ptr := reflect.ValueOf(v)
	ok := ptr.Kind() == reflect.Ptr
	if !ok {
		return reflect.Value{}, nil, false
	}

	str := ptr.Elem()
	if str.Kind() != reflect.Struct {
		return reflect.Value{}, nil, false
	}
	typ := str.Type()
	return str, typ, true
}

func writeValue(v reflect.Value, name string, c *cfg.ConfigFile) {
	switch v.Kind() {
	case reflect.Bool, reflect.String,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Float32, reflect.Float64:
		c.AddOption(cfg.DefaultSection, name, fmt.Sprint(v.Interface()))
	}
}

func readValue(v reflect.Value, name string, c *cfg.ConfigFile) {
	if !c.HasOption(cfg.DefaultSection, name) {
		return
	}
	switch t := v; t.Kind() {
	case reflect.Bool:
		v, err := c.GetBool(cfg.DefaultSection, name)
		if err == nil {
			t.SetBool(v)
		}
	case reflect.String:
		v, err := c.GetString(cfg.DefaultSection, name)
		if err == nil {
			t.SetString(v)
		}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		v, err := c.GetInt(cfg.DefaultSection, name)
		if err == nil {
			t.SetInt(int64(v))
		}
	case reflect.Float32, reflect.Float64:
		v, err := c.GetFloat(cfg.DefaultSection, name)
		if err == nil {
			t.SetFloat(float64(v))
		}
	}
}

func writeConfig(v interface{}) error {
	const errstr = "WriteConfig expects a pointer to a struct value as an argument"

	str, typ, ok := interfaceIsPtrStruct(v)
	if !ok {
		return errors.New(errstr)
	}

	c := cfg.NewConfigFile()
	for i := 0; i < str.NumField(); i++ {
		v := str.Field(i)
		name := typ.Field(i).Tag
		writeValue(v, string(name), c)
	}

	makeSureConfigDirExists()
	err := c.WriteConfigFile(configFile(), 0644, "gocode config file")
	if err != nil {
		return err
	}

	return nil
}

func readConfig(v interface{}) error {
	c, err := cfg.ReadConfigFile(configFile())
	if err != nil {
		return err
	}

	const errstr = "ReadConfig expects a pointer to a struct value as an argument"

	str, typ, ok := interfaceIsPtrStruct(v)
	if !ok {
		return errors.New(errstr)
	}

	for i := 0; i < str.NumField(); i++ {
		v := str.Field(i)
		name := typ.Field(i).Tag
		readValue(v, string(name), c)
	}

	return nil
}

func xdgHomeDir() string {
	xdghome := os.Getenv("XDG_CONFIG_HOME")
	if xdghome == "" {
		xdghome = filepath.Join(os.Getenv("HOME"), ".config")
	}
	return xdghome
}

func makeSureConfigDirExists() {
	dir := filepath.Join(xdgHomeDir(), "gocode")
	if !fileExists(dir) {
		os.MkdirAll(dir, 0755)
	}
}

func configFile() string {
	return filepath.Join(xdgHomeDir(), "gocode", "config.ini")
}
