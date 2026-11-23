# Smoke test on a real Windows machine: start Orbit, drive it through its own settings API (full
# screen, both kinds, backgrounds, lyrics styles, widgets, arrange mode), take screenshots, and
# check it never crashed, then quit it cleanly.
$ErrorActionPreference = "Stop"
$env:APPDATA = "$env:RUNNER_TEMP\appdata"
New-Item -ItemType Directory -Force $env:APPDATA, shots | Out-Null
$dir = "$env:APPDATA\OrbitLyrics"
Add-Type -AssemblyName System.Drawing, System.Windows.Forms

$p = Start-Process -FilePath .\Orbit.exe -PassThru
function Alive($what) {
  if ($p.HasExited) { Write-Host "::error::Orbit exited ($($p.ExitCode)) during: $what"; Dump; exit 1 }
  $c = Get-Content "$dir\crash.log" -Raw -ErrorAction SilentlyContinue
  if ($c -match "panic:|fatal error:|goroutine \d+") { Write-Host "::error::crash during: $what"; Dump; exit 1 }
}
function Dump {
  Write-Host "---- orbit.log ----"; Get-Content "$dir\orbit.log" -ErrorAction SilentlyContinue | Write-Host
  Write-Host "---- crash.log ----"; Get-Content "$dir\crash.log" -ErrorAction SilentlyContinue | Write-Host
}
function Shot($name) {
  try {
    $b = [System.Windows.Forms.Screen]::PrimaryScreen.Bounds
    $bmp = New-Object System.Drawing.Bitmap $b.Width, $b.Height
    $g = [System.Drawing.Graphics]::FromImage($bmp)
    $g.CopyFromScreen($b.Location, [System.Drawing.Point]::Empty, $b.Size)
    $bmp.Save("shots\$name.png"); $g.Dispose(); $bmp.Dispose()
  } catch { Write-Host "screenshot $name failed: $_" }
}

Start-Sleep -Seconds 8
Alive "start"
$tok = (Get-Content "$dir\settings.key" -Raw).Trim()
$base = "http://127.0.0.1:47330"
$state = Invoke-RestMethod "$base/api/state?t=$tok"
Write-Host "settings API answers: version $($state.version)"
function Set-Opt($k, $v) {
  Invoke-RestMethod -Method Post -Uri "$base/api/set?t=$tok" -ContentType "application/json" -Body (@{ key = $k; value = $v } | ConvertTo-Json -Compress) | Out-Null
}
function Act($n, $v) {
  Invoke-RestMethod -Method Post -Uri "$base/api/action?t=$tok" -ContentType "application/json" -Body (@{ name = $n; value = $v } | ConvertTo-Json -Compress) | Out-Null
}
Shot "01-desktop"

# widgets on, every lyrics style
foreach ($k in "ShowLyrics", "ShowVinyl", "ShowHalo", "ShowClock", "ShowWeather", "ShowGlow") { Set-Opt $k $true }
Start-Sleep 2; Alive "widgets on"; Shot "02-widgets"
for ($s = 0; $s -lt $state.styles.Count; $s++) { Set-Opt "LyricsStyle" $s; Start-Sleep -Milliseconds 250; Alive "lyrics style $s" }
Act "editMode" $true; Start-Sleep 2; Alive "arrange mode"; Shot "03-arrange"; Act "editMode" $false

# full screen: the music display, then the desktop skin with each background and middle
Set-Opt "FsKind" 0
Act "fullscreen" $true; Start-Sleep 5; Alive "music display"; Shot "04-music-display"
Act "fullscreen" $false; Start-Sleep 2; Alive "leave full screen"
Set-Opt "FsKind" 1
Act "fullscreen" $true; Start-Sleep 5; Alive "desktop skin"; Shot "05-desktop-skin"
$bgs = $state.lists.skinBgs.Count
for ($b = 0; $b -lt $bgs; $b++) { Set-Opt "SkinBg" $b; Start-Sleep -Milliseconds 1200; Alive "skin background $b" }
Shot "06-skin-last-bg"
for ($m = 0; $m -lt 3; $m++) { Set-Opt "SkinCentre" $m; foreach ($n in 1, 2, 3) { Set-Opt "SkinFlowCount" $n; Start-Sleep -Milliseconds 500; Alive "middle $m / $n" } }
foreach ($k in "SkinList", "SkinBar", "SkinClock", "SkinLyrics", "SkinFrame") { Set-Opt $k $false; Start-Sleep -Milliseconds 400; Set-Opt $k $true; Alive "toggle $k" }
Shot "07-skin-flow"
Act "fullscreen" $false; Start-Sleep 2
# open and close it quickly many times
for ($i = 0; $i -lt 8; $i++) { Act "fullscreen" $true; Start-Sleep -Milliseconds 300; Act "fullscreen" $false; Start-Sleep -Milliseconds 300 }
Start-Sleep 2; Alive "open/close 8 times"
$ws = (Get-Process -Id $p.Id).WorkingSet64 / 1MB
Write-Host ("memory after all that: {0:N0} MB" -f $ws)

Act "quit" $true
if (-not $p.WaitForExit(10000)) { Write-Host "::error::Orbit didn't quit within 10 s"; Dump; exit 1 }
Alive "quit"
Dump
Write-Host "Orbit went through everything without crashing"
