## Fast parse Go modules

	go get github.com/visualfc/fastmod
	
Usages:

	pkg, err := fastmod.LoadPackage(dir, &build.Default)
	if err != nil {
		return
	}
	path, dir, typ := pkg.Lookup(pkg)