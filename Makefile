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
	declcache.go\
	semanticcontext.go

include $(GOROOT)/src/Make.cmd

rpc.go: server.go goremote/goremote
	./goremote/goremote server.go | gofmt > rpc.go

goremote/goremote: goremote/goremote.go
	gomake -C goremote

_go_.$(O): configfile.a

configfile.a: goconfig/configfile.go
	gomake -C goconfig
	cp goconfig/_obj/configfile.a .
