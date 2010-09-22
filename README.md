## An autocompletion/refactoring daemon for the Go programming language

Gocode is a helper tool which is intended to be integraded with your source code editor, like vim and emacs. It provides several advanced capabilities, which currently includes:

 - Context-sensitive autocompletion
 - Semantically-preserving renaming of an identifier

It is called *daemon*, because it uses client/server architecture for caching purposes. In particular, it makes autocompletions very fast. Typical autocompletion time with warm cache is 30ms, which is barely noticeable.

### Setup

 1. First of all, you need to get the latest git version of the gocode source code: 
    
    `git clone git://github.com/nsf/gocode.git`

 2. Change the directory:

    `cd gocode`

 3. At this step, please make sure that your **$GOBIN** is available in your **$PATH**. By default **$GOBIN** points to **$GOROOT/bin**. This is important, because editors assume that **gocode** executable is available in one of the directories, specified by your **$PATH** environment variable. Usually you've done that already while installing the Go compiler suite.

    Do these steps only if you know why do you need them:

    `export GOBIN=$HOME/bin`
    `export PATH=$PATH:$HOME/bin`

 4. Then you need to build the gocode and install it:

    `make install`

 5. Next steps are editor specific. See below.

### Vim setup

In order to install vim scripts, you need to fulfill the following steps:

 1. Install official Go vim scripts from **$GOROOT/misc/vim**. If you did that already, proceed to the step 2.

 2. Install gocode vim scripts. Usually it's enough to do the following:

 `cd vim && ./update.bash`

 **update.bash** script does the following:

		#!/usr/bin/env bash
		mkdir -p ~/.vim/{autoload,ftplugin,plugin}
		cp autoload/gocomplete.vim ~/.vim/autoload
		cp ftplugin/go.vim ~/.vim/ftplugin
		cp plugin/gocode.vim ~/.vim/plugin

 3. Make sure vim has filetype plugin enabled. Simply add that to your **.vimrc**:

 `filetype plugin on`

 4. Autocompletion and renaming should work now. Use `<C-x><C-o>` for autocompletion (omnifunc autocompletion). For identifier renaming simply type `:GocodeRename` when the cursor is on top of an identifier you want to rename. Vim will ask you for a new name and do the job.

 *NOTE*: Vim renames identifiers only in opened files. If you want to apply rename operation to multiple files of your package you have to open all the files at once (e.g. `vim *.go`).

### Options

You can change all available options using `gocode set` command. The config file uses .ini-like format and usually stored somewhere in **~/.config/gocode** directory.

`gocode set` lists all options and their values.

`gocode set <option>` shows the value of that *option*.

`gocode set <option> <value>` sets the new *value* for that *option*.

 - *propose-builtins*

 A boolean option. If **true**, gocode will add built-in types, functions and constants to an autocompletion proposals. Default: **false**.

 - *deny-package-renames*

 A boolean option. If **true**, gocode will deny renaming requests for package names. This option exists mainly for allowing simple implementation of the testing framework, which tests renaming facility. Default: **false**.

 - *lib-path*

 A string option. Allows you to override default location of the standard Go library. By default, it uses **$GOROOT/pkg/$GOOS_$GOARCH** in terms of previously existed environment variables.

### Debugging

If something went wrong, the first thing you may want to do is manually start the gocode daemon in a separate terminal window. It will show you all the stack traces and panics if any. Shutdown the daemon if it was already started and run a new one explicitly:

 `gocode close`
 `gocode -s`

Please, report bugs, feature suggestions and other rants to the [github issue tracker](http://github.com/nsf/gocode/issues) of this project.

### Developing

If you want to integrate gocode in your editor, please, contact me and I will tell you exactly what do you need. You can send me a message via github or simply contact me via email: no.smile.face@gmail.com.

### Misc

 - It's a good idea to use the latest git version always. I'm trying to keep it in a working state.
 - Gocode always requires the latest Go compiler suite version of the 'release' branch.
