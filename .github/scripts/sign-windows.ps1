$ErrorActionPreference = 'Stop'
$sdk = Join-Path ${env:ProgramFiles(x86)} 'Windows Kits\10\bin'
$signtool = Get-ChildItem "$sdk\*\x64\signtool.exe" |
    Sort-Object { [version] $_.Directory.Parent.Name } -Descending |
    Select-Object -First 1 -ExpandProperty FullName
if (!$signtool) { throw 'SignTool not found' }

$pfx = Join-Path $env:RUNNER_TEMP 'vlessia-signing.pfx'
[IO.File]::WriteAllBytes($pfx, [Convert]::FromBase64String($env:CERTIFICATE))
try {
    function Sign-ReleaseFile($path) {
        & $signtool sign /f $pfx /p $env:CERTIFICATE_PASSWORD /fd SHA256 /tr http://timestamp.digicert.com /td SHA256 $path
        if ($LASTEXITCODE) { throw "SignTool signing failed for $path" }
        & $signtool verify /pa $path
        if ($LASTEXITCODE) { throw "SignTool verification failed for $path" }
    }
    $binary = (Resolve-Path build/bin/Vlessia.exe).Path
    Sign-ReleaseFile $binary
    Push-Location build/windows/installer
    try {
        makensis "-DARG_WAILS_AMD64_BINARY=$binary" project.nsi
        if ($LASTEXITCODE) { throw 'NSIS packaging failed' }
    } finally {
        Pop-Location
    }
    Sign-ReleaseFile build/bin/Vlessia-amd64-installer.exe
} finally {
    Remove-Item -LiteralPath $pfx -Force
}
