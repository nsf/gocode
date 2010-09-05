all: showcursor showsmap rename

showcursor:
	make -C utils/showcursor
	cp utils/showcursor/showcursor .

showsmap:
	make -C utils/showsmap
	cp utils/showsmap/showsmap .

rename:
	make -C utils/rename
	cp utils/rename/rename .

clean:
	make -C utils/showcursor clean
	rm -rf showcursor
	make -C utils/showsmap clean
	rm -rf showsmap
	make -C utils/rename clean
	rm -rf rename
