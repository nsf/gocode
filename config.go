package main

import (
	cfg "./configfile"
	"reflect"
	"strconv"
	"bytes"
	"path"
	"fmt"
	"os"
	"io"
)

var Config = struct {
	ProposeBuiltins bool "propose-builtins"
	DenyModuleRenames bool "deny-module-renames"
	LibPath string "lib-path" // currently unused
}{
	false,
	false,
	"",
}

func setValue(v reflect.Value, name, value string) {
	switch t := v.(type) {
	case *reflect.BoolValue:
		v, ok := cfg.BoolStrings[value]
		if ok {
			t.Set(v)
		}
	case *reflect.StringValue:
		t.Set(value)
	case *reflect.IntValue:
		v, err := strconv.Atoi64(value)
		if err == nil {
			t.Set(v)
		}
	case *reflect.FloatValue:
		v, err := strconv.Atof64(value)
		if err == nil {
			t.Set(v)
		}
	}
}

func listValue(v reflect.Value, name string, w io.Writer) {
	switch t := v.(type) {
	case *reflect.BoolValue:
		fmt.Fprintf(w, "boolean '%s': %v\n", name, t.Get())
	case *reflect.StringValue:
		fmt.Fprintf(w, "string  '%s': %v\n", name, t.Get())
	case *reflect.IntValue:
		fmt.Fprintf(w, "int     '%s': %v\n", name, t.Get())
	case *reflect.FloatValue:
		fmt.Fprintf(w, "float   '%s': %v\n", name, t.Get())
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
		listValue(v, name, buf)
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
		if nm == name {
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
		if nm == name {
			setValue(v, name, value)
			listValue(v, name, buf)
		}
	}
	writeConfig(v)
	return buf.String()
}

func interfaceIsPtrStruct(v interface{}) (*reflect.StructValue, *reflect.StructType, bool) {
	ptr, ok := reflect.NewValue(v).(*reflect.PtrValue)
	if !ok {
		return nil, nil, false
	}

	str, ok := ptr.Elem().(*reflect.StructValue)
	if !ok {
		return nil, nil, false
	}
	typ := str.Type().(*reflect.StructType)
	return str, typ, true
}

func writeValue(v reflect.Value, name string, c *cfg.ConfigFile) {
	switch v.(type) {
	case *reflect.BoolValue, *reflect.StringValue,
		*reflect.IntValue, *reflect.FloatValue:
		c.AddOption(cfg.DefaultSection, name, fmt.Sprint(v.Interface()))
	}
}

func readValue(v reflect.Value, name string, c *cfg.ConfigFile) {
	if !c.HasOption(cfg.DefaultSection, name) {
		return
	}
	switch t := v.(type) {
	case *reflect.BoolValue:
		v, err := c.GetBool(cfg.DefaultSection, name)
		if err == nil {
			t.Set(v)
		}
	case *reflect.StringValue:
		v, err := c.GetString(cfg.DefaultSection, name)
		if err == nil {
			t.Set(v)
		}
	case *reflect.IntValue:
		v, err := c.GetInt(cfg.DefaultSection, name)
		if err == nil {
			t.Set(int64(v))
		}
	case *reflect.FloatValue:
		v, err := c.GetFloat(cfg.DefaultSection, name)
		if err == nil {
			t.Set(float64(v))
		}
	}
}

func writeConfig(v interface{}) os.Error {
	const errstr = "WriteConfig expects a pointer to a struct value as an argument"

	str, typ, ok := interfaceIsPtrStruct(v)
	if !ok {
		return os.NewError(errstr)
	}

	c := cfg.NewConfigFile()
	for i := 0; i < str.NumField(); i++ {
		v := str.Field(i)
		name := typ.Field(i).Tag
		writeValue(v, name, c)
	}

	makeSureConfigDirExists()
	err := c.WriteConfigFile(configFile(), 0644, "gocode config file")
	if err != nil {
		return err
	}

	return nil
}

func readConfig(v interface{}) os.Error {
	c, err := cfg.ReadConfigFile(configFile())
	if err != nil {
		return err
	}

	const errstr = "ReadConfig expects a pointer to a struct value as an argument"

	str, typ, ok := interfaceIsPtrStruct(v)
	if !ok {
		return os.NewError(errstr)
	}

	for i := 0; i < str.NumField(); i++ {
		v := str.Field(i)
		name := typ.Field(i).Tag
		readValue(v, name, c)
	}

	return nil
}

func XDGHomeDir() string {
	xdghome := os.Getenv("XDG_CONFIG_HOME")
	if xdghome == "" {
		xdghome = path.Join(os.Getenv("HOME"), ".config")
	}
	return xdghome
}

func makeSureConfigDirExists() {
	dir := path.Join(XDGHomeDir(), "gocode")
	if !fileExists(dir) {
		os.MkdirAll(dir, 0755)
	}
}

func configFile() string {
	return path.Join(XDGHomeDir(), "gocode", "config.ini")
}
