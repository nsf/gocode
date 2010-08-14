include $(GOROOT)/src/Make.$(GOARCH)

TARG=gocode
GOFILES=gocode.go file.go module.go autocompletion.go gocodeserver.go gorpc.go decl.go gocodeapropos.go scope.go ripper.go config.go

include $(GOROOT)/src/Make.cmd

gorpc.go: gocodeserver.go goremote/goremote
	./goremote/goremote gocodeserver.go > gorpc.go

goremote/goremote: goremote/goremote.go
	cd goremote && make

_go_.$(O): configfile.a

configfile.a: goconfig/configfile.go
	cd goconfig && make
	cp goconfig/_obj/configfile.a .
