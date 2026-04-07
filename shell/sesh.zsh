# sesh - shell wrapper for the sesh session picker
# Source this file from your .zshrc:
#   source /path/to/sesh.zsh
#
# The wrapper is needed because the binary outputs a shell command (cd + exec)
# that must run in the current shell, not a subprocess.

sesh() {
  local cmd
  cmd=$(command sesh "$@") || return $?
  eval "$cmd"
}
