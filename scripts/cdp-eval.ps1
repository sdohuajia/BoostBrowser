param(
  [Parameter(Mandatory=$true)][string]$WebSocketUrl,
  [Parameter(Mandatory=$true)][string]$Expression,
  [int]$Id = 1
)
$ErrorActionPreference = 'Stop'
$ws = [System.Net.WebSockets.ClientWebSocket]::new()
$uri = [Uri]$WebSocketUrl
$cts = [Threading.CancellationToken]::None
$ws.ConnectAsync($uri, $cts).GetAwaiter().GetResult()
try {
  $payload = @{ id = $Id; method = 'Runtime.evaluate'; params = @{ expression = $Expression; returnByValue = $true; awaitPromise = $true } } | ConvertTo-Json -Depth 8 -Compress
  $bytes = [Text.Encoding]::UTF8.GetBytes($payload)
  $seg = [ArraySegment[byte]]::new($bytes)
  $ws.SendAsync($seg, [System.Net.WebSockets.WebSocketMessageType]::Text, $true, $cts).GetAwaiter().GetResult()

  $buffer = New-Object byte[] 65536
  while ($true) {
    $ms = New-Object System.IO.MemoryStream
    do {
      $segIn = [ArraySegment[byte]]::new($buffer)
      $result = $ws.ReceiveAsync($segIn, $cts).GetAwaiter().GetResult()
      if ($result.Count -gt 0) { $ms.Write($buffer, 0, $result.Count) }
    } while (-not $result.EndOfMessage)
    $msg = [Text.Encoding]::UTF8.GetString($ms.ToArray())
    if ($msg -match '"id"\s*:\s*' + $Id) {
      Write-Host $msg
      break
    }
  }
}
finally {
  if ($ws.State -eq [System.Net.WebSockets.WebSocketState]::Open) {
    $ws.CloseAsync([System.Net.WebSockets.WebSocketCloseStatus]::NormalClosure, 'done', $cts).GetAwaiter().GetResult()
  }
  $ws.Dispose()
}
