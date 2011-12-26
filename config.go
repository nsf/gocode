package main

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"encoding/json"
)

//-------------------------------------------------------------------------
// Config
//
// Structure represents persistent config storage of the gocode daemon. Usually
// the config is located somewhere in ~/.config/gocode directory.
//-------------------------------------------------------------------------

var Config = struct {
	ProposeBuiltins bool   `json:"propose-builtins"`
	LibPath         string `json:"lib-path"`
}{
	false,
	"",
}

var boolStrings = map[string]bool{
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

func setValue(v reflect.Value, name, value string) {
	switch t := v; t.Kind() {
	case reflect.Bool:
		v, ok := boolStrings[value]
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
		name := typ.Field(i).Tag.Get("json")
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
		nm := typ.Field(i).Tag.Get("json")
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
		nm := typ.Field(i).Tag.Get("json")
		if nm == name {
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

func writeConfig(v interface{}) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}

	makeSureConfigDirExists()
	f, err := os.Create(configFile())
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

func readConfig(v interface{}) error {
	data, err := ioutil.ReadFile(configFile())
	if err != nil {
		return err
	}

	err = json.Unmarshal(data, v)
	if err != nil {
		return err
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
	return filepath.Join(xdgHomeDir(), "gocode", "config.json")
}
