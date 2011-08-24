if exists('g:loaded_gocode')
	finish
endif
let g:loaded_gocode = 1

fu! s:gocodeCurrentBuffer()
	let buf = getline(1, '$')
	if &l:fileformat == 'dos'
		" XXX: line2byte() depend on 'fileformat' option.
		" so if fileformat is 'dos', 'buf' must include '\r'.
		let buf = map(buf, 'v:val."\r"')
	endif
	let file = tempname()
	call writefile(buf, file)
	return file
endf

fu! s:system(str, ...)
	return (a:0 == 0 ? system(a:str) : system(a:str, join(a:000)))
endf

fu! s:gocodeCommand(cmd, preargs, args)
	for i in range(0, len(a:args) - 1)
		let a:args[i] = shellescape(a:args[i])
	endfor
	for i in range(0, len(a:preargs) - 1)
		let a:preargs[i] = shellescape(a:preargs[i])
	endfor
	let result = s:system(printf('gocode %s %s %s', join(a:preargs), a:cmd, join(a:args)))
	if v:shell_error != 0
		return "[\"0\", []]"
	else
		return result
	endif
endf

fu! s:gocodeCurrentBufferOpt(filename)
	return '-in=' . a:filename
endf

fu! s:gocodeCursor()
	return printf('%d', line2byte(line('.')) + (col('.')-2))
endf

fu! s:gocodeAutocomplete()
	let filename = s:gocodeCurrentBuffer()
	let result = s:gocodeCommand('autocomplete',
				   \ [s:gocodeCurrentBufferOpt(filename), '-f=vim'],
				   \ [bufname('%'), s:gocodeCursor()])
	call delete(filename)
	return result
endf

fu! gocomplete#Complete(findstart, base)
	"findstart = 1 when we need to get the text length
	if a:findstart == 1
		execute "silent let g:gocomplete_completions = " . s:gocodeAutocomplete()
		return col('.') - g:gocomplete_completions[0] - 1
	"findstart = 0 when we need to return the list of completions
	else
		return g:gocomplete_completions[1]
	endif
endf
