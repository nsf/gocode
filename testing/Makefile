TOOLS=showcursor showsmap rename listidents

.PHONY: all clean $(TOOLS)

all: $(TOOLS)

$(TOOLS):
	make -C utils/$@
	cp utils/$@/$@ .

clean:
	for i in $(TOOLS); do make -C utils/$$i clean; done
	rm -rf $(TOOLS)

