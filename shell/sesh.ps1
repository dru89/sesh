# sesh - PowerShell wrapper for the sesh session picker
# Add this to your PowerShell profile ($PROFILE):
#   . /path/to/sesh.ps1
#
# Or copy the function directly into your profile.
#
# The wrapper is needed because the picker outputs a shell command
# (Set-Location + exec) that must run in the current session. The binary
# prefixes eval-able output with __sesh_eval: so the wrapper knows when
# to eval vs. print directly.

function sesh {
    $env:SESH_WRAPPER = '1'
    $output = & sesh.exe @args
    Remove-Item Env:\SESH_WRAPPER
    if ($LASTEXITCODE -ne 0) { return }
    if ($output -and $output.StartsWith('__sesh_eval:')) {
        Invoke-Expression $output.Substring(13)
    } elseif ($output) {
        Write-Output $output
    }
}
