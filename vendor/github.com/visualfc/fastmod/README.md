## Fast parse Go modules

	go get github.com/visualfc/fastmod
	
Usages:

	modList := fastmod.NewModuleList(&build.Default)
	mod, err := modList.LoadModule(dir)
	if err != nil {
		return
	}
	path, dir, typ := mod.Lookup(pkg)