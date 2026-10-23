# Docker Desktop does not forward Windows bind-mount writes to Linux inotify.
# Relay native Windows filesystem events to Spaniel's reconciliation endpoints.
param(
    [Parameter(Mandatory)][string]$DashboardsDir,
    [Parameter(Mandatory)][string]$AlertsDir,
    [Parameter(Mandatory)][string]$BaseURL
)
$ErrorActionPreference = 'Stop'
$prefix = 'spaniel-definitions-' + [guid]::NewGuid().ToString('N')
$directories = @{
    dashboard = [IO.Path]::GetFullPath($DashboardsDir).TrimEnd([IO.Path]::DirectorySeparatorChar)
    alert = [IO.Path]::GetFullPath($AlertsDir).TrimEnd([IO.Path]::DirectorySeparatorChar)
}
$watchers = @()
$pending = [Collections.Generic.HashSet[string]]::new()
try {
    foreach ($kind in @('dashboard', 'alert')) {
        $directory = $directories[$kind]
        $watcher = [IO.FileSystemWatcher]::new([IO.Path]::GetDirectoryName($directory))
        $watcher.IncludeSubdirectories = $true
        $watcher.NotifyFilter = [IO.NotifyFilters]'FileName, DirectoryName, LastWrite, Size'
        foreach ($eventName in @('Created', 'Changed', 'Deleted', 'Renamed', 'Error')) {
            Register-ObjectEvent -InputObject $watcher -EventName $eventName -SourceIdentifier "$prefix|$kind|$eventName" | Out-Null
        }
        $watcher.EnableRaisingEvents = $true
        $watchers += $watcher
        [void]$pending.Add($kind) # Reconcile the registration gap, and reconnects.
    }
    while ($true) {
        if ($pending.Count -eq 0) { Wait-Event -Timeout 30 | Out-Null }
        Start-Sleep -Milliseconds 200 # Coalesce multi-write/atomic editor saves.
        foreach ($event in @(Get-Event | Where-Object { $_.SourceIdentifier.StartsWith($prefix) })) {
            $parts = $event.SourceIdentifier.Split('|')
            $kind = $parts[1]
            $directory = $directories[$kind]
            $paths = @($event.SourceEventArgs.FullPath, $event.SourceEventArgs.OldFullPath)
            $relevant = $parts[2] -eq 'Error'
            foreach ($path in $paths) {
                if (!$path) { continue }
                $isYAML = [IO.Path]::GetExtension($path) -cin @('.yaml', '.yml')
                if ($path -eq $directory -or ([IO.Path]::GetDirectoryName($path) -eq $directory -and $isYAML)) { $relevant = $true }
            }
            if ($relevant) { [void]$pending.Add($kind) }
            Remove-Event -EventIdentifier $event.EventIdentifier
        }
        foreach ($kind in @($pending)) {
            try {
                $plural = if ($kind -eq 'dashboard') { 'dashboards' } else { 'alerts' }
                $headers = @{}
                if ($env:SPANIEL_BEARER_TOKEN) { $headers.Authorization = "Bearer $env:SPANIEL_BEARER_TOKEN" }
                Invoke-RestMethod -Method Post -Uri "$($BaseURL.TrimEnd('/'))/api/$plural/reload" -Body '{}' -ContentType 'application/json' -Headers $headers -TimeoutSec 10 | Out-Null
                [void]$pending.Remove($kind)
                Write-Output "$(Get-Date -Format o) reconciled $kind definitions"
            } catch {
                Write-Warning "Could not reconcile $kind definitions: $($_.Exception.Message)"
                if ($_.Exception.Response -and [int]$_.Exception.Response.StatusCode -eq 400) {
                    [void]$pending.Remove($kind) # Invalid YAML retries on the next file event.
                    continue
                }
                # Keep the change pending while the collector restarts. Retry
                # requests, never poll directory contents.
                Wait-Event -Timeout 2 | Out-Null
            }
        }
    }
} finally {
    Get-EventSubscriber | Where-Object { $_.SourceIdentifier.StartsWith($prefix) } | Unregister-Event
    foreach ($watcher in $watchers) { $watcher.Dispose() }
}
