if !has('python')
    echo "Error: Required vim compiled with +python"
    finish
endif

function! gocomplete#Complete(findstart, base)
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
        execute "python gocomplete('" . cword . "', '" . a:base . "', '" . line2byte(line('.')) . "')"
        return g:gocomplete_completions
    endif
endfunction

function! s:DefPython()
python << PYTHONEOF


def gocomplete(context, match, cursor=-1):
	import vim, subprocess
	buf = "\n".join(vim.current.buffer)

	if not context:
		context = "_"
	gocode = subprocess.Popen("gocode autocomplete %s %s" % (context, cursor), shell=True, stdin=subprocess.PIPE, stdout=subprocess.PIPE)
	output = gocode.communicate(buf)[0]
	if gocode.returncode != 0:
		vim.command('silent let g:gocomplete_completions = []')
	else:
		vim.command('silent let g:gocomplete_completions = %s' % output)

PYTHONEOF
endfunction

call s:DefPython()
