[CmdletBinding()]
param([string] $InstallerPath = (Join-Path $PSScriptRoot '..\install\install.ps1'))
Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
$tokens = $null
$errors = $null
$ast = [System.Management.Automation.Language.Parser]::ParseFile(
    (Resolve-Path -LiteralPath $InstallerPath), [ref] $tokens, [ref] $errors)
if ($errors.Count -ne 0) { throw 'Installer syntax errors prevent ownership verification.' }
# Replay only the real rollback stop branch with in-memory command doubles.
# Never execute the installer, touch a real scheduled task, or require elevation.
$stops = @($ast.FindAll({
    param($node)
    $node -is [System.Management.Automation.Language.CommandAst] -and
        $node.GetCommandName() -eq 'Stop-ScheduledTask'
}, $true))
if ($stops.Count -ne 1) { throw 'Review every scheduled-task stop when this invariant changes.' }
$branch = $stops[0].Parent
while ($null -ne $branch -and $branch -isnot [System.Management.Automation.Language.IfStatementAst]) {
    $branch = $branch.Parent
}
if ($null -eq $branch) { throw 'The scheduled-task stop must be guarded.' }
$probe = [scriptblock]::Create($branch.Extent.Text)
function Stop-ScheduledTask {
    [CmdletBinding()]
    param([string] $TaskName, [string] $TaskPath)
    if ($TaskName -ne 'AgentDock' -or $TaskPath -ne '\') { throw 'Unexpected task identity.' }
    $script:stopCount++
}
function Start-Sleep { param([int] $Milliseconds) }
$cases = @(
    @{ Name = 'other-root-preflight-conflict'; Mode = 'elevated'; Started = $false; Stops = 0 },
    @{ Name = 'credential-preflight-rejection'; Mode = 'elevated'; Started = $false; Stops = 0 },
    @{ Name = 'uac-never-started'; Mode = 'elevated'; Started = $false; Stops = 0 },
    @{ Name = 'standard-failure'; Mode = 'standard'; Started = $true; Stops = 0 },
    @{ Name = 'owned-elevated-transaction-rollback'; Mode = 'elevated'; Started = $true; Stops = 1 }
)
foreach ($case in $cases) {
    $effectivePrivilegeMode = $case.Mode
    $taskTransactionStarted = $case.Started
    $script:stopCount = 0
    & $probe
    if ($script:stopCount -ne $case.Stops) {
        throw "Task ownership regression '$($case.Name)': expected $($case.Stops) stops, got $script:stopCount."
    }
}
Write-Host "Windows installer task rollback ownership: $($cases.Count) cases passed (mock task adapter)."
