package main

import (
	cfg "./configfile"
	"reflect"
	"path"
	"fmt"
	"os"
)

var Config = struct {
	ProposeBuiltins bool "propose-builtins"
}{
	false,
}

func WriteValue(v reflect.Value, name string, c *cfg.ConfigFile) {
	switch t := v.(type) {
	case *reflect.BoolValue:
		c.AddOption(cfg.DefaultSection, name, fmt.Sprint(t.Get()))
	case *reflect.StringValue:
		c.AddOption(cfg.DefaultSection, name, fmt.Sprint(t.Get()))
	case *reflect.IntValue:
		c.AddOption(cfg.DefaultSection, name, fmt.Sprint(t.Get()))
	case *reflect.FloatValue:
		c.AddOption(cfg.DefaultSection, name, fmt.Sprint(t.Get()))
	default:
		panic("Unknown value type")
	}
}

func ReadValue(v reflect.Value, name string, c *cfg.ConfigFile) {
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
	default:
		panic("Unknown value type")
	}
}

func WriteConfig(v interface{}) os.Error {
	const errstr = "WriteConfig expects a pointer to a struct value as an argument"

	ptr, ok := reflect.NewValue(v).(*reflect.PtrValue)
	if !ok {
		return os.NewError(errstr)
	}

	str, ok := ptr.Elem().(*reflect.StructValue)
	if !ok {
		return os.NewError(errstr)
	}

	typ := str.Type().(*reflect.StructType)

	c := cfg.NewConfigFile()
	for i := 0; i < str.NumField(); i++ {
		v := str.Field(i)
		name := typ.Field(i).Tag
		WriteValue(v, name, c)
	}

	MakeSureConfigDirExists()
	err := c.WriteConfigFile(ConfigFile(), 0644, "gocode config file")
	if err != nil {
		return err
	}

	return nil
}

func ReadConfig(v interface{}) os.Error {
	c, err := cfg.ReadConfigFile(ConfigFile())
	if err != nil {
		return err
	}

	const errstr = "ReadConfig expects a pointer to a struct value as an argument"

	ptr, ok := reflect.NewValue(v).(*reflect.PtrValue)
	if !ok {
		return os.NewError(errstr)
	}

	str, ok := ptr.Elem().(*reflect.StructValue)
	if !ok {
		return os.NewError(errstr)
	}

	typ := str.Type().(*reflect.StructType)
	for i := 0; i < str.NumField(); i++ {
		v := str.Field(i)
		name := typ.Field(i).Tag
		ReadValue(v, name, c)
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

func MakeSureConfigDirExists() {
	dir := path.Join(XDGHomeDir(), "gocode")
	if !fileExists(dir) {
		os.MkdirAll(dir, 0755)
	}
}

func ConfigFile() string {
	return path.Join(XDGHomeDir(), "gocode", "config.ini")
}
