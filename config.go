package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"reflect"
	"strconv"
)

//-------------------------------------------------------------------------
// config
//
// Structure represents persistent config storage of the gocode daemon. Usually
// the config is located somewhere in ~/.config/gocode directory.
//-------------------------------------------------------------------------

type config struct {
	ProposeBuiltins bool   `json:"propose-builtins"`
	LibPath         string `json:"lib-path"`
}

var gConfig = config{
	false,
	"",
}

var gStringToBool = map[string]bool{
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

func setValue(v reflect.Value, value string) {
	switch t := v; t.Kind() {
	case reflect.Bool:
		v, ok := gStringToBool[value]
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
	str, typ := this.valueAndType()
	buf := bytes.NewBuffer(make([]byte, 0, 256))
	for i := 0; i < str.NumField(); i++ {
		v := str.Field(i)
		name := typ.Field(i).Tag.Get("json")
		listValue(v, name, buf)
	}
	return buf.String()
}

func (this *config) listOption(name string) string {
	str, typ := this.valueAndType()
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

func (this *config) setOption(name, value string) string {
	str, typ := this.valueAndType()
	buf := bytes.NewBuffer(make([]byte, 0, 256))
	for i := 0; i < str.NumField(); i++ {
		v := str.Field(i)
		nm := typ.Field(i).Tag.Get("json")
		if nm == name {
			setValue(v, value)
			listValue(v, name, buf)
		}
	}
	this.write()
	return buf.String()

}

func (this *config) valueAndType() (reflect.Value, reflect.Type) {
	v := reflect.ValueOf(this).Elem()
	return v, v.Type()
}

func (this *config) write() error {
	data, err := json.Marshal(this)
	if err != nil {
		return err
	}

	// make sure config dir exists
	dir := configDir()
	if !fileExists(dir) {
		os.MkdirAll(dir, 0755)
	}

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

func (this *config) read() error {
	data, err := ioutil.ReadFile(configFile())
	if err != nil {
		return err
	}

	err = json.Unmarshal(data, this)
	if err != nil {
		return err
	}

	return nil
}
