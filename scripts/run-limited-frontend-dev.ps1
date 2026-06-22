param(
    [Parameter(Mandatory = $true)]
    [string]$WorkingDirectory,
    [int]$MemoryLimitMB = 512,
    [int]$MaxOldSpaceMB = 256,
    [int]$MaxSemiSpaceMB = 16,
    [string]$PidFile = "",
    [string[]]$NodeArgs = @("frontend/scripts/dev-watcher.mjs")
)

$ErrorActionPreference = "Stop"

function Remove-PidFile {
    if ($PidFile -and (Test-Path -LiteralPath $PidFile)) {
        Remove-Item -LiteralPath $PidFile -Force -ErrorAction SilentlyContinue
    }
}

if (-not ("AntChrome.JobObjectNative" -as [type])) {
    Add-Type -TypeDefinition @"
using System;
using System.Runtime.InteropServices;

namespace AntChrome {
    public static class JobObjectNative {
        [StructLayout(LayoutKind.Sequential)]
        public struct JOBOBJECT_BASIC_LIMIT_INFORMATION {
            public long PerProcessUserTimeLimit;
            public long PerJobUserTimeLimit;
            public uint LimitFlags;
            public UIntPtr MinimumWorkingSetSize;
            public UIntPtr MaximumWorkingSetSize;
            public uint ActiveProcessLimit;
            public UIntPtr Affinity;
            public uint PriorityClass;
            public uint SchedulingClass;
        }

        [StructLayout(LayoutKind.Sequential)]
        public struct IO_COUNTERS {
            public ulong ReadOperationCount;
            public ulong WriteOperationCount;
            public ulong OtherOperationCount;
            public ulong ReadTransferCount;
            public ulong WriteTransferCount;
            public ulong OtherTransferCount;
        }

        [StructLayout(LayoutKind.Sequential)]
        public struct JOBOBJECT_EXTENDED_LIMIT_INFORMATION {
            public JOBOBJECT_BASIC_LIMIT_INFORMATION BasicLimitInformation;
            public IO_COUNTERS IoInfo;
            public UIntPtr ProcessMemoryLimit;
            public UIntPtr JobMemoryLimit;
            public UIntPtr PeakProcessMemoryUsed;
            public UIntPtr PeakJobMemoryUsed;
        }

        public const int JobObjectExtendedLimitInformation = 9;
        public const uint JOB_OBJECT_LIMIT_PROCESS_MEMORY = 0x00000100;
        public const uint JOB_OBJECT_LIMIT_JOB_MEMORY = 0x00000200;
        public const uint JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE = 0x00002000;

        [DllImport("kernel32.dll", CharSet = CharSet.Unicode, SetLastError = true)]
        public static extern IntPtr CreateJobObject(IntPtr lpJobAttributes, string lpName);

        [DllImport("kernel32.dll", SetLastError = true)]
        public static extern bool SetInformationJobObject(
            IntPtr hJob,
            int JobObjectInfoClass,
            ref JOBOBJECT_EXTENDED_LIMIT_INFORMATION lpJobObjectInfo,
            int cbJobObjectInfoLength
        );

        [DllImport("kernel32.dll", SetLastError = true)]
        public static extern bool AssignProcessToJobObject(IntPtr hJob, IntPtr hProcess);
    }
}
"@
}

$jobName = "ant-chrome-node-$PID"
$jobHandle = [AntChrome.JobObjectNative]::CreateJobObject([IntPtr]::Zero, $jobName)
if ($jobHandle -eq [IntPtr]::Zero) {
    throw "CreateJobObject failed: $([Runtime.InteropServices.Marshal]::GetLastWin32Error())"
}

$memoryLimitBytes = [UInt64]$MemoryLimitMB * 1MB
$limits = New-Object AntChrome.JobObjectNative+JOBOBJECT_EXTENDED_LIMIT_INFORMATION
$limits.BasicLimitInformation.LimitFlags = `
    [AntChrome.JobObjectNative]::JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE -bor `
    [AntChrome.JobObjectNative]::JOB_OBJECT_LIMIT_JOB_MEMORY -bor `
    [AntChrome.JobObjectNative]::JOB_OBJECT_LIMIT_PROCESS_MEMORY
$limits.ProcessMemoryLimit = [UIntPtr]::new($memoryLimitBytes)
$limits.JobMemoryLimit = [UIntPtr]::new($memoryLimitBytes)

$limitStructSize = [Runtime.InteropServices.Marshal]::SizeOf($limits)
if (-not [AntChrome.JobObjectNative]::SetInformationJobObject(
    $jobHandle,
    [AntChrome.JobObjectNative]::JobObjectExtendedLimitInformation,
    [ref]$limits,
    $limitStructSize
)) {
    throw "SetInformationJobObject failed: $([Runtime.InteropServices.Marshal]::GetLastWin32Error())"
}

$currentProcessHandle = [System.Diagnostics.Process]::GetCurrentProcess().Handle
if (-not [AntChrome.JobObjectNative]::AssignProcessToJobObject($jobHandle, $currentProcessHandle)) {
    throw "AssignProcessToJobObject failed: $([Runtime.InteropServices.Marshal]::GetLastWin32Error())"
}

$nodeOptions = "--max-old-space-size=$MaxOldSpaceMB --max-semi-space-size=$MaxSemiSpaceMB"
if ($env:NODE_OPTIONS) {
    $env:NODE_OPTIONS = "$($env:NODE_OPTIONS) $nodeOptions"
} else {
    $env:NODE_OPTIONS = $nodeOptions
}
$env:npm_config_node_options = $env:NODE_OPTIONS

if (-not (Test-Path -LiteralPath $WorkingDirectory)) {
    throw "Working directory not found: $WorkingDirectory"
}

if ($PidFile) {
    $pidDirectory = Split-Path -Parent $PidFile
    if ($pidDirectory) {
        New-Item -ItemType Directory -Force -Path $pidDirectory | Out-Null
    }
    Set-Content -LiteralPath $PidFile -Value $PID -Encoding ascii -NoNewline
}

Write-Host "[node-limit] job-memory=$MemoryLimitMB MB"
Write-Host "[node-limit] node-options=$env:NODE_OPTIONS"
Write-Host "[node-limit] working-directory=$WorkingDirectory"
Write-Host "[node-limit] node-args=$($NodeArgs -join ' ')"

try {
    $nodeProcess = Start-Process -FilePath "node" -ArgumentList $NodeArgs -WorkingDirectory $WorkingDirectory -PassThru -NoNewWindow
    $nodeProcess.WaitForExit()
    exit $nodeProcess.ExitCode
} finally {
    Remove-PidFile
}
