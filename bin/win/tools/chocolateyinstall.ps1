$ErrorActionPreference = 'Stop';
$toolsPath = "$(Split-Path -parent $MyInvocation.MyCommand.Definition)"

$version = $env:chocolateyPackageVersion
$pp = Get-PackageParameters

if ($null -ne $pp['path']){
	$lpath = $pp['path']
}
elseif (Test-Path env:linkerdPath) {
	$lpath = $env:linkerdPath
}
else {
	$lpath = $toolsPath
}

if ($null -ne $pp['checksum']){
	$checksum = $pp['checksum']
}
else{
	$checksum = $env:linkerdCheckSum
}

$packageArgs = @{
  packageName    = 'linkerd'
  fileFullPath   = "$lpath\linkerd.exe"
  url64          = "https://github.com/linkerd/linkerd2/releases/download/stable-$version/linkerd2-cli-stable-$version-windows.exe"
  checksum       = $checksum
  checksumType   = 'sha256'
}

Get-ChocolateyWebFile @packageArgs
Install-ChocolateyPath $packageArgs.fileFullPath 'User'
