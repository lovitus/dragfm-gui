package webgui

import (
 "runtime"
 "strings"

 "github.com/lovitus/dragfm-gui/internal/endpoint"
)

func terminalCDForEndpoint(target endpoint.Endpoint, directory string) string {
 if _, local := target.(*endpoint.Local); local && runtime.GOOS == "windows" {
  return powershellCDCommand(directory)
 }
 return terminalCDCommand(directory)
}

func powershellCDCommand(directory string) string {
 // Single-quoted PowerShell literals do not expand $, backticks or subexpressions.
 // -LiteralPath also prevents wildcard characters in file names being interpreted.
 return "Set-Location -LiteralPath '" + strings.ReplaceAll(directory, "'", "''") + "'; Write-Host -NoNewline ([string][char]27 + '[2K' + [char]13)\r"
}
