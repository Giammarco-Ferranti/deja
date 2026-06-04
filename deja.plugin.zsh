# deja: Oh My Zsh plugin
# Predictive inline autosuggestions for zsh. Requires the `deja` binary on $PATH;
# install it via Homebrew or the curl script: https://github.com/Giammarco-Ferranti/deja
#
# Enable by adding `deja` to plugins=(...) in ~/.zshrc. This sources deja's zsh
# integration (the same script as `eval "$(deja init zsh)"`). The integration is
# already framework-aware and re-binds its widgets on each precmd, so it coexists
# with Oh My Zsh's own keybinding setup.

if (( ! $+commands[deja] )); then
  print -ru2 -- "[oh-my-zsh] deja plugin: 'deja' not found on \$PATH. Install it (https://github.com/Giammarco-Ferranti/deja), then restart your shell."
  return
fi

eval "$(deja init zsh)"
