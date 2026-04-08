# sesh - shell wrapper for the sesh session picker
# Source this file from your .bashrc or .bash_profile:
#   source /path/to/sesh.bash
#
# The wrapper is needed because the picker outputs a shell command (cd + exec)
# that must run in the current shell, not a subprocess. The binary prefixes
# eval-able output with __sesh_eval: so the wrapper knows when to eval vs.
# print directly.

sesh() {
  local out
  out=$(SESH_WRAPPER=1 command sesh "$@") || return $?
  if [[ "$out" == __sesh_eval:* ]]; then
    eval "${out#__sesh_eval:}"
  elif [[ -n "$out" ]]; then
    printf '%s\n' "$out"
  fi
}
