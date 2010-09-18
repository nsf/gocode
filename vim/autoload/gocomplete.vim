if exists('g:loaded_gocode')
	finish
endif
let g:loaded_gocode = 1

fu! s:gocodeCurrentBuffer()
	let buf = getline(1, '$')
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

fu! s:gocodeRename()
	return s:gocodeCommand('rename',
			     \ ['-f=vim'],
			     \ [bufname('%'), s:gocodeCursor()])
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

fu! s:gocodeDoForBuf(expr, funcref, argslist)
	let [cur_bufnr, expr_bufnr] = [bufnr('%'), bufnr(a:expr)]
	let [cur_bufhidden, expr_bufhidden] = [getbufvar('%', '&bufhidden'), getbufvar(a:expr, '&bufhidden')]
	call setbufvar('%', '&bufhidden', 'hide')
	call setbufvar(a:expr, '&bufhidden', 'hide')
	try
		if cur_bufnr != expr_bufnr
			execute expr_bufnr . 'buffer'
		endif
		call call(a:funcref, a:argslist)
	finally
		execute cur_bufnr . 'buffer'
		call setbufvar('%', '&bufhidden', cur_bufhidden)
		call setbufvar(a:expr, '&bufhidden', expr_bufhidden)
	endtry
endf

fu! s:gocodeRenameBuf(newname, length, rename_data)
	" rename_data is: [[line,col],[line,col],...]
	for renamer in a:rename_data
		let line = getline(renamer[0])
		let break = byteidx(line, renamer[1]-1)
		call setline(renamer[0],
			   \ strpart(line, 0, break) .
			   \ a:newname .
			   \ strpart(line, break + a:length))
	endfor
	write
endf

fu! gocomplete#Rename()
	" Rename format is:
	" [status, [{'filename':...,'length':...,'decls':[[line,col],...]},...]]
	execute "silent let rename_data = " . s:gocodeRename()
	let status = rename_data[0]
	let renames = rename_data[1]
	if status != "OK"
		echo status
		return
	endif
	if &modified
		echo "Current buffer was modified, you should :w before using GocodeRename"
		return
	endif
	let newname = input("New identifier name: ")
	for fileinfo in renames
		" Skip those files that are not in the buffer list
		if !bufexists(fileinfo["filename"])
			con
		endif
		" If there is nothing to do, don't run DoForBuf, because
		" it does screen flickering and we can avoid that in a lot of
		" cases.
		if empty(fileinfo['decls'])
			con
		endif
		call s:gocodeDoForBuf(fileinfo["filename"],
				    \ function("s:gocodeRenameBuf"),
				    \ [newname, fileinfo["length"], fileinfo["decls"]])
	endfor
endf
