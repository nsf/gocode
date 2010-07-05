include $(GOROOT)/src/Make.$(GOARCH)

TARG=gocode
GOFILES=gocode.go gocodelib.go gocodeserver.go

include $(GOROOT)/src/Make.cmd
