
# IDE Integration Guide #

This guide should help programmers to develop Go lang plugins for IDEs and editors. It documents command arguments and output formats, and also mentions common pitfails. Note that gocode is not intended to be used from command-line by human.

## Code Completion Assistance ##

Gocode proposes completion depending on current scope and context. Currently some obvious features are missed:
* No keywords completion (no context-sensitive neither absolute)
* No package names completion
* No completion proposal priority
* Information about context not passed to output, i.e. gocode does not report if you've typed `st.` or `fn(`

Also keep in mind following things:
* Editor probably keeps unsaved file copy in memory, so you should pass file content via stdin, or mirror it to temporary file and use `-in=*` parameter. Gocode does not support more than one unsaved file.
* If you pass unsaved file via stdin, you should also pass full path to target file as parameter, otherwise completion will be incomplete because other files from the same package will be not resolved.
* If coder started to type identifier like `Pr`, gocode will produce completions `Printf`, `Produce`, etc. In other words, completion contains identifier prefix and is already filtered. Filtering uses case-sensitive comparison if possible, and fallbacks to case-insensitive comparison.
* If you want to see built-in identifiers like `uint32`, `error`, etc, you can call `gocode set propose-builtins yes` once.

Use autocomplete command to produce completion assistance for particular position at file:
```bash
# Read source from file and show completions for character at offset 449 from beginning
gocode -f=json --in=server.go autocomplete 449
# Read source from stdin (more suitable for editor since it keeps unsaved file copy in memory)
gocode -f=json autocomplete 449
# You can also pass target file path along with position, it's used to find other files from the same package
gocode -f=json autocomplete server.go 889
# By default gocode interprets offset as bytes offset, but 'c' or 'C' prefix means that offset is unicode points offset
gocode -f=json autocomplete server.go c619
```

[Output formats reference.](autocomplete_formats.md)
