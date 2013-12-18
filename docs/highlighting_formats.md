
# Description of Highlighting Formats #

Use `-f` parameter for `highlight` command to set format. QtJSON format is default and fallback.

Following formats supported:
* qtjson

## qtjson ###
This JSON-based format designed for Qt-based editors like QtCreator. Example:
```json
[{ "format": "var", "line": 1, "column": 1, "length": 2 }]
```
Limitations:
* results are not sorted by position;
* both line and column are 1-based, so there never will be line #0;
* `format` should be one of:
    * `error` is expression, declaration or statement with syntax error
    * `package` is package name
    * `field` is struct field name or anything accessed by dot, e.g. `a.field`
    * `func` is function name in declaration or call expression
    * `label` is label name (jabels used for jumps)
    * `type` is type name in declaration or any other place
    * `var` is variable name in declaration or reference

