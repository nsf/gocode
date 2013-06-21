## An autocompletion daemon for the Go programming language

Gocode is a helper tool which is intended to be integrated with your source code editor, like vim and emacs. It provides several advanced capabilities, which currently includes:

 - Context-sensitive autocompletion

It is called *daemon*, because it uses client/server architecture for caching purposes. In particular, it makes autocompletions very fast. Typical autocompletion time with warm cache is 30ms, which is barely noticeable.

Also watch the [demo screencast](http://nosmileface.ru/images/gocode-demo.swf).

![Gocode in vim](http://nosmileface.ru/images/gocode-screenshot.png)

![Gocode in emacs](http://nosmileface.ru/images/emacs-gocode.png)

### Setup

 1. You should have a correctly installed Go compiler environment and your personal workspace ($GOPATH). If you have no idea what **$GOPATH** is, take a look [here](http://golang.org/doc/code.html). Please make sure that your **$GOPATH/bin** is available in your **$PATH**. This is important, because most editors assume that **gocode** binary is available in one of the directories, specified by your **$PATH** environment variable. Otherwise manually copy the **gocode** binary from **$GOPATH/bin** to a location which is part of your **$PATH** after getting it in step 2.

    Do these steps only if you know why do you need them:

    `export GOPATH=$HOME/goprojects`

    `export PATH=$PATH:$GOPATH/bin`

 2. Then you need to get the appropriate version of the gocode, for 6g/8g/5g compiler you can do this:

    `go get -u github.com/nsf/gocode` (-u flag for "update")

    Windows users should consider doing this instead:

    `go get -u -ldflags -H=windowsgui github.com/nsf/gocode`

    That way on the Windows OS gocode will be built as a GUI application and doing so solves hanging window issues with some of the editors.

 3. Next steps are editor specific. See below.

### Vim setup

In order to install vim scripts, you need to fulfill the following steps:

 1. Install official Go vim scripts from **$GOROOT/misc/vim**. If you did that already, proceed to the step 2.

 2. Install gocode vim scripts. Usually it's enough to do the following:

    `vim/update.sh`

    **update.sh** script does the following:

		#!/bin/sh
		mkdir -p "$HOME/.vim/autoload"
		mkdir -p "$HOME/.vim/ftplugin"
		cp "${0%/*}/autoload/gocomplete.vim" "$HOME/.vim/autoload"
		cp "${0%/*}/ftplugin/go.vim" "$HOME/.vim/ftplugin"

 3. Make sure vim has filetype plugin enabled. Simply add that to your **.vimrc**:

    `filetype plugin on`

 4. Autocompletion should work now. Use `<C-x><C-o>` for autocompletion (omnifunc autocompletion).

Alternatively take a look at the vundle/pathogen friendly repo: https://github.com/Blackrush/vim-gocode.

### Emacs setup

In order to install emacs script, you need to fulfill the following steps:

 1. Install [auto-complete-mode](http://www.emacswiki.org/emacs/AutoComplete)

 2. Copy **emacs/go-autocomplete.el** file from the gocode source distribution to a directory which is in your 'load-path' in emacs.

 3. Add these lines to your **.emacs**:

 		(require 'go-autocomplete)
		(require 'auto-complete-config)

Also, there is an alternative plugin for emacs using company-mode. See `emacs-company/README` for installation instructions.

### Options

You can change all available options using `gocode set` command. The config file uses .ini-like format and usually stored somewhere in **~/.config/gocode** directory.

`gocode set` lists all options and their values.

`gocode set <option>` shows the value of that *option*.

`gocode set <option> <value>` sets the new *value* for that *option*.

 - *propose-builtins*

   A boolean option. If **true**, gocode will add built-in types, functions and constants to an autocompletion proposals. Default: **false**.

 - *lib-path*

   A string option. Allows you to add search paths for packages. By default, gocode only searches **$GOPATH/pkg/$GOOS_$GOARCH** and **$GOROOT/pkg/$GOOS_$GOARCH** in terms of previously existed environment variables. Also you can specify multiple paths using ':' (colon) as a separator (on Windows use semicolon ';').

### Debugging

If something went wrong, the first thing you may want to do is manually start the gocode daemon in a separate terminal window. It will show you all the stack traces and panics if any. Shutdown the daemon if it was already started and run a new one explicitly:

`gocode close`

`gocode -s`

Please, report bugs, feature suggestions and other rants to the [github issue tracker](http://github.com/nsf/gocode/issues) of this project.

### Developing

If you want to integrate gocode in your editor, please, contact me and I will tell you exactly what do you need. You can send me a message via github or simply contact me via <a href="mailto:no.smile.face@gmail.com">email</a>.

### Misc

 - It's a good idea to use the latest git version always. I'm trying to keep it in a working state.
