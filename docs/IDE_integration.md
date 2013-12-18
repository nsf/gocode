
# IDE Integration Guide #

This guide should help programmers to develop Go lang plugins for IDEs and editors. It documents command arguments and output formats, and also mentions common pitfails. Note that gocode is not intended to be used from command-line by human.

## Code Completion Assistance ##

Gocode proposes completion depending on current scope and context. Currently some obvious features are missed:
* No keywords completion (no context-sensitive neither absolute)
* No package names completion
* No completion proposal priority
* Information about context not passed to output, i.e. gocode does not report if you've typed `st.` or `fn(`

Also keep in mind following things:
* Editor probably keeps unsaved file copy in memory, so you should pass file content via stdin
* If coder started to type identifier like `Pr`, gocode will produce completions `Printf`, `Produce`, etc. In other words, completion contains identifier prefix and is already filtered using case-sensitive comparison

Use autocomplete command to produce completion assistance for particular position at file:
```bash
# Read source from file and show completions for character at offset 449 from beginning
gocode -f=json --in=server.go autocomplete 449
# Read source from stdin (more suitable for editor since it keeps unsaved file copy in memory)
gocode -f=json autocomplete 449
# You can also pass target filename along with position
gocode -f=json autocomplete server.go 889
```

[Output formats reference.](autocomplete_formats.md)

## Semantic Code Highlighting ##

Gocode does not handle lexical highlighting (which finds keywords, literals, etc). You should implement it yourself in order to make fast and responsive editor. The only area where lexical highlighter does not work are identifiers: you cannot easily determine which object is declarated or referenced in identifier, and gocode can help you.

Use highlight command to produce semantic highlight ranges for whole file:
```bash
# Read source from file
gocode -f=qtjson --in=server.go highlight
# Read source from stdin (more suitable for editor since it keeps unsaved file copy in memory)
gocode -f=qtjson highlight
# You can also pass target filename
gocode -f=qtjson highlight server.go
```

[Output formats reference.](highlighting_formats.md)
