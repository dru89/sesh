# sesh - shell wrapper for the sesh session picker
# Source this file from your .bashrc or .bash_profile:
#   source /path/to/sesh.bash
#
# The wrapper is needed because the binary outputs a shell command (cd + exec)
# that must run in the current shell, not a subprocess.

sesh() {
  local cmd
  cmd=$(command sesh "$@") || return $?
  eval "$cmd"
}
