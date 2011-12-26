include $(GOROOT)/src/Make.inc

TARG=gocode
GOFILES=gocode.go\
	autocompletefile.go\
	package.go\
	autocompletecontext.go\
	server.go\
	rpc.go\
	decl.go\
	apropos.go\
	scope.go\
	ripper.go\
	config.go\
	declcache.go

ifeq ($(GOOS),windows)
GOFILES+=os_win32.go
else
GOFILES+=os_posix.go
endif

include $(GOROOT)/src/Make.cmd
