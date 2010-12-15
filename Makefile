include $(GOROOT)/src/Make.inc

PREREQ+=configfile.a
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

rpc.go: server.go goremote/goremote
	./goremote/goremote server.go | gofmt > rpc.go

goremote/goremote: goremote/goremote.go
	gomake -C goremote

configfile.a: goconfig/configfile.go
	gomake -C goconfig
	cp goconfig/_obj/configfile.a .

clean: cleandeps

cleandeps:
	gomake -C goremote clean
	gomake -C goconfig clean
