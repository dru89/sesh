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
    # Subcommands never emit __sesh_eval:, so run them directly to preserve
    # TTY (glamour rendering, colors, etc.).
    $passthrough = @('index','recap','ask','init','list','show','stats','version','update')
    if ($args.Count -gt 0 -and $passthrough -contains $args[0]) {
        & sesh.exe @args
        return
    }

    # Root picker: capture output so we can eval resume commands.
    $env:SESH_WRAPPER = '1'
    $output = & sesh.exe @args
    Remove-Item Env:\SESH_WRAPPER
    if ($LASTEXITCODE -ne 0) { return }
    if ($output -and $output.StartsWith('__sesh_eval:')) {
        Invoke-Expression $output.Substring(12)
    } elseif ($output) {
        Write-Output $output
    }
}
