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

fu! s:gocodeCommand(preargs, args)
	for i in range(0, len(a:args) - 1)
		let a:args[i] = shellescape(a:args[i])
	endfor
	return s:system(printf('gocode %s autocomplete %s', join(a:preargs), join(a:args)))
endf

fu! s:gocodeCurrentBufferOpt(filename)
	return '-in=' . a:filename
endf

fu! s:gocodeCursor()
	return printf('%d', line2byte(line('.')))
endf

fu! s:gocodeAutocomplete(apropos)
	let filename = s:gocodeCurrentBuffer()
	let result = s:gocodeCommand([s:gocodeCurrentBufferOpt(filename), '-f=vim'],
				   \ [a:apropos, s:gocodeCursor()])
	call delete(filename)
	return result
endf

fu! gocomplete#Complete(findstart, base)
    "findstart = 1 when we need to get the text length
    if a:findstart == 1
        let line = getline('.')
        let idx = col('.')
        while idx > 0
            let idx -= 1
            let c = line[idx]
            if c =~ '\w'
                continue
            elseif ! c =~ '\.'
                let idx = -1
                break
            else
                break
            endif
        endwhile

        return idx
    "findstart = 0 when we need to return the list of completions
    else
        "vim no longer moves the cursor upon completion... fix that
        let line = getline('.')
        let idx = col('.')
        let cword = ''
        while idx > 0
            let idx -= 1
            let c = line[idx]
            if c =~ '\w' || c =~ '\.'
                let cword = c . cword
                continue
            elseif strlen(cword) > 0 || idx == 0
                break
            endif
        endwhile
	execute "silent let g:gocomplete_completions = " . s:gocodeAutocomplete(cword)
        return g:gocomplete_completions
    endif
endf
