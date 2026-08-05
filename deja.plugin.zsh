# deja: Oh My Zsh plugin
# Predictive inline autosuggestions for zsh. Requires the `deja` binary on $PATH;
# install it via Homebrew or the curl script: https://github.com/Giammarco-Ferranti/deja
#
# Enable by adding `deja` to plugins=(...) in ~/.zshrc. This sources deja's zsh
# integration. The integration is already framework-aware and re-binds its
# widgets on each precmd, so it coexists with Oh My Zsh's own keybinding setup.

if (( ! $+commands[deja] )); then
  print -ru2 -- "[oh-my-zsh] deja plugin: 'deja' not found on \$PATH. Install it (https://github.com/Giammarco-Ferranti/deja), then restart your shell."
  return
fi

# `deja init zsh` does not print the integration script — it writes it to
# ~/.local/share/deja/init.zsh and prints a source line for that file. Sourcing
# the file directly keeps a full binary launch (~25-36ms) off every shell
# startup; the eval is only the first-run bootstrap, for before the file exists.
# deja refreshes the file itself, in the background, when the binary changes.
_deja_init_file="$HOME/.local/share/deja/init.zsh"
if [[ -r "$_deja_init_file" ]]; then
  source "$_deja_init_file"
else
  eval "$(deja init zsh)"
fi
unset _deja_init_file
