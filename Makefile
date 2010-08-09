include $(GOROOT)/src/Make.$(GOARCH)

TARG=gocode
GOFILES=gocode.go file.go module.go gocodelib.go gocodeserver.go gorpc.go decl.go gocodeapropos.go scope.go ripper.go

include $(GOROOT)/src/Make.cmd

gorpc.go: gocodeserver.go goremote/goremote
	./goremote/goremote gocodeserver.go > gorpc.go

goremote/goremote: goremote/goremote.go
	cd goremote && make
