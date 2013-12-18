
# Description of Completion Assistance Formats #

Use `-f` parameter for `autocomplete` command to set format. VIM format is default and fallback.

Following formats supported:
* nice
* vim
* godit
* emacs
* csv
* json

## nice ##
You can use it to test from command-line.

## vim ##
Format designed to be used in VIM scripts.

## godit ##

## emacs ##
Format designed to be used in Emacs scripts.

## csv ##
Comma-separated values format which has small size.

## json ###
Generic JSON format. Example (manually formatted):
```json
[6, [{
         "class": "func",
         "name": "client_auto_complete",
         "type": "func(cli *rpc.Client, Arg0 []byte, Arg1 string, Arg2 int, Arg3 gocode_env) (c []candidate, d int)"
     }, {
         "class": "func",
         "name": "client_close",
         "type": "func(cli *rpc.Client, Arg0 int) int"
     }, {
         "class": "func",
         "name": "client_cursor_type_pkg",
         "type": "func(cli *rpc.Client, Arg0 []byte, Arg1 string, Arg2 int) (typ, pkg string)"
     }, {
         "class": "func",
         "name": "client_drop_cache",
         "type": "func(cli *rpc.Client, Arg0 int) int"
     }, {
         "class": "func",
         "name": "client_highlight",
         "type": "func(cli *rpc.Client, Arg0 []byte, Arg1 string, Arg2 gocode_env) (c []highlight_range, d int)"
     }, {
         "class": "func",
         "name": "client_set",
         "type": "func(cli *rpc.Client, Arg0, Arg1 string) string"
     }, {
         "class": "func",
         "name": "client_status",
         "type": "func(cli *rpc.Client, Arg0 int) string"
     }
 ]]
```

Limitations:
* `class` can be one of: `func`, `package`, `var`, `type`, `const`, `PANIC`
* `PANIC` means suspicious error inside gocode
* `name` is text which can be inserted
* `type` can be used to create code assistance hint
* You can re-format type by using following approach: if `class` is prefix of `type`, delete this prefix and add another prefix `class` + " " + `name`.
