[CmdletBinding()]
param([switch] $Force)
Import-Module AU

$domain = "https://github.com"
$releases = "$domain/linkerd/linkerd2/releases"

function global:au_BeforeUpdate {

  Get-RemoteFiles -Purge -NoSuffix -FileNameBase "linkerd2-cli"
  $Latest.Checksum64 = Get-RemoteChecksum $Latest.Url

}

function global:au_SearchReplace {

  @{
      ".\tools\chocolateyInstall.ps1" = @{

	        "(?i)(^\s*packageName\s*=\s*)('.*')" = "`$1'$($Latest.PackageName)'"
	        "(?i)(^\s*url\s*=\s*)" = "`$1'$($Latest.URL)'"
	        "(?i)(^\s*checksum\s*=\s*)" = "`$1'$($Latest.Checksum64)'"
        
        }  
   }
}

function global:au_GetLatest {

  $download_page = Invoke-WebRequest -Uri $releases -UseBasicParsing
  $url = $download_page.links | ? href -match '/linkerd2-cli-stable-(.+)-windows\.exe$' | % href | select -First 1
  $url = $domain+$url
  $version = $Matches[1]
  return @{
    
    Version     = $version
    URL       = $url
    ReleaseURL  = "$domain/linkerd/linkerd2/releases/tag/v${version}"
  
  }

}
update -ChecksumFor none -Force:$Force