all: showcursor showsmap

showcursor:
	make -C utils/showcursor
	cp utils/showcursor/showcursor .

showsmap:
	make -C utils/showsmap
	cp utils/showsmap/showsmap .

clean:
	make -C utils/showcursor clean
	rm -rf showcursor
	make -C utils/showsmap clean
	rm -rf showsmap
