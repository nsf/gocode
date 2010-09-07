if exists('loaded_gocode_plugin')
	finish
endif

let loaded_gocode_plugin = 1

command! GocodeRename :call gocomplete#Rename()
