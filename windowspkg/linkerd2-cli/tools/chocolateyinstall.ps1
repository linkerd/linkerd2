$ErrorActionPreference = 'Stop';
$toolsPath = "$(Split-Path -parent $MyInvocation.MyCommand.Definition)"

$packageArgs = @{

  packageName    = 'linkerd'
  fileFullPath   = "$toolsPath\linkerd.exe"
  url            = 'https://github.com/linkerd/linkerd2/releases/download/stable-2.6.1/linkerd2-cli-stable-2.6.1-windows.exe'
  checksum       = '4ed915b10c2a76070d40bf3223f09e742777105914acaeadfc83d99ef6cf1798'
  checksumType   = 'sha256'
}

Get-ChocolateyWebFile @packageArgs
Install-ChocolateyPath $packageArgs.fileFullPath 'Machine'