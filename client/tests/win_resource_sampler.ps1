# Samples one process's CPU + memory on a fixed interval and writes CSV rows
# to -Out, one per sample: wall_ts,cpu_pct,working_set_mb,private_mb
#
# cpu_pct is core-seconds-consumed-per-wall-second * 100, i.e. 100 == one
# full logical core saturated (NOT normalized against total core count,
# so it can exceed 100 on a multi-threaded process using >1 core) -- this
# matches classic Task Manager's per-process % semantics, not the
# core-count-normalized view some newer Windows builds default to.
#
# Usage: powershell -File win_resource_sampler.ps1 -ProcName usbridge-streamer -DurationSec 180 -IntervalSec 5 -Out C:\path\out.csv
param(
    [Parameter(Mandatory=$true)][string]$ProcName,
    [int]$DurationSec = 180,
    [int]$IntervalSec = 5,
    [Parameter(Mandatory=$true)][string]$Out
)

"wall_ts,cpu_pct,working_set_mb,private_mb" | Out-File -FilePath $Out -Encoding ascii

$prevCpu = $null
$prevTime = $null
$deadline = (Get-Date).AddSeconds($DurationSec)

while ((Get-Date) -lt $deadline) {
    $p = Get-Process -Name $ProcName -ErrorAction SilentlyContinue | Select-Object -First 1
    $now = Get-Date
    if ($p -ne $null) {
        $cpuNow = $p.CPU
        $wsMb = [math]::Round($p.WorkingSet64 / 1MB, 1)
        $pmMb = [math]::Round($p.PrivateMemorySize64 / 1MB, 1)
        if ($prevCpu -ne $null) {
            $dCpu = $cpuNow - $prevCpu
            $dT = ($now - $prevTime).TotalSeconds
            $cpuPct = if ($dT -gt 0) { [math]::Round(($dCpu / $dT) * 100, 1) } else { 0 }
        } else {
            $cpuPct = 0
        }
        $prevCpu = $cpuNow
        $prevTime = $now
        "$($now.ToString('o')),$cpuPct,$wsMb,$pmMb" | Out-File -FilePath $Out -Encoding ascii -Append
    } else {
        "$($now.ToString('o')),,," | Out-File -FilePath $Out -Encoding ascii -Append
        $prevCpu = $null
        $prevTime = $null
    }
    Start-Sleep -Seconds $IntervalSec
}
