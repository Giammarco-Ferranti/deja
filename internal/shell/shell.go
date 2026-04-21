package shell

import _ "embed"

//go:embed zsh.sh
var zshInit string

func ZshInit() string { return zshInit }
