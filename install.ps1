<#
.SYNOPSIS
    TimeWeaver root installer (Windows).

.DESCRIPTION
    Thin pass-through wrapper around scripts\setup-windows.ps1 so the project can
    be installed directly from the repository root. Every argument is forwarded
    to the setup script unchanged.

    Run with no arguments for an interactive install (it asks which component to
    install and writes ready-to-run config for you - nothing to copy or edit).

.EXAMPLE
    .\install.ps1
    Interactive install: pick a component, answer a few prompts (Enter = default).

.EXAMPLE
    .\install.ps1 -Component agent
    Install only the scheduler agent (no Node/UI build required).

.EXAMPLE
    .\install.ps1 -Component server -NonInteractive
    Unattended server install with all defaults (sqlite3, generated SECRET_KEY).
#>
$ErrorActionPreference = "Stop"
$setup = Join-Path $PSScriptRoot "scripts\setup-windows.ps1"
& $setup @args
