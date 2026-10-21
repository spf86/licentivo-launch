param(
    [string]$MySQLBin = 'C:\Program Files\MySQL\MySQL Server 8.0\bin',
    [switch]$Preview,
	[int]$PreviewPort = 18080,
	[string]$PreviewRestore = ''
)
$ErrorActionPreference = 'Stop'
function Stop-TaskProcess([Diagnostics.Process]$Process) {
    if ($Process -and -not $Process.HasExited) {
        # Kill(Boolean) is unavailable in Windows PowerShell 5.1. These are
        # direct project-owned processes, so the compatible overload is enough.
        $Process.Kill()
        $Process.WaitForExit()
    }
}
function Stop-TaskMySQLProcess([string]$DataPath) {
	if ([string]::IsNullOrWhiteSpace($DataPath)) { return }
	$taskResolvedData = [IO.Path]::GetFullPath($DataPath)
	$taskOwnedRoot = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..\.cache\mysql-test-'))
	if (-not $taskResolvedData.StartsWith($taskOwnedRoot, [StringComparison]::OrdinalIgnoreCase)) {
		throw ('Refusing to stop MySQL outside the project preview directory: ' + $taskResolvedData)
	}
	Get-CimInstance Win32_Process -Filter "Name = 'mysqld.exe'" -ErrorAction SilentlyContinue |
		Where-Object { $_.CommandLine -and $_.CommandLine.IndexOf($taskResolvedData, [StringComparison]::OrdinalIgnoreCase) -ge 0 } |
		ForEach-Object { Stop-Process -Id $_.ProcessId -Force -ErrorAction SilentlyContinue }
}
function Invoke-TaskJson {
	param(
		[string]$Method,
		[string]$Uri,
		[hashtable]$Headers,
		[string]$Body,
		[string]$BodyPath,
		[string]$CookiePath
	)
	[IO.File]::WriteAllText($BodyPath, $Body, [Text.UTF8Encoding]::new($false))
	$taskCurlArgs = @(
		'--fail-with-body', '--silent', '--show-error', '--max-time', '15',
		'--request', $Method.ToUpperInvariant(), '--url', $Uri,
		'--header', 'Content-Type: application/json',
		'--data-binary', ('@' + $BodyPath)
	)
	foreach ($taskHeader in $Headers.GetEnumerator()) {
		$taskCurlArgs += @('--header', ($taskHeader.Key + ': ' + $taskHeader.Value))
	}
	if ($CookiePath) {
		if (Test-Path -LiteralPath $CookiePath) { $taskCurlArgs += @('--cookie', $CookiePath) }
		$taskCurlArgs += @('--cookie-jar', $CookiePath)
	}
	$taskResponse = & curl.exe @taskCurlArgs
	if ($LASTEXITCODE -ne 0) { throw ("Preview request failed: $Method $Uri`n" + ($taskResponse -join "`n")) }
	$taskContent = $taskResponse -join "`n"
	if ([string]::IsNullOrWhiteSpace($taskContent)) { return $null }
	return $taskContent | ConvertFrom-Json
}
$taskRoot = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))
$taskMySQLD = Join-Path $MySQLBin 'mysqld.exe'
if (-not (Test-Path -LiteralPath $taskMySQLD)) { throw 'Specify -MySQLBin containing mysqld.exe.' }
$taskRun = Join-Path $taskRoot ('.cache\mysql-test-' + [Guid]::NewGuid().ToString('N'))
$taskData = Join-Path $taskRun 'data'
New-Item -ItemType Directory -Path $taskData -Force | Out-Null
$taskBase = [IO.Path]::GetFullPath((Join-Path $MySQLBin '..'))
$taskInitArgs = @('--no-defaults', '--initialize-insecure', ('--basedir="' + $taskBase + '"'), ('--datadir="' + $taskData + '"'))
$taskInit = Start-Process -FilePath $taskMySQLD -ArgumentList $taskInitArgs -WindowStyle Hidden -PassThru -Wait -RedirectStandardOutput (Join-Path $taskRun 'initialize.out') -RedirectStandardError (Join-Path $taskRun 'initialize.err')
if ($taskInit.ExitCode -ne 0) { throw ('MySQL test initialization failed. Logs: ' + $taskRun) }
$taskPortProbe = [Net.Sockets.TcpListener]::new([Net.IPAddress]::Loopback, 0)
$taskPortProbe.Start()
$taskPort = $taskPortProbe.LocalEndpoint.Port
$taskPortProbe.Stop()
$taskPassword = [Guid]::NewGuid().ToString('N') + [Guid]::NewGuid().ToString('N')
$taskDatabase = 'launch_test_' + [Guid]::NewGuid().ToString('N')
$taskAppDatabase = $taskDatabase + '_app'
$taskRestoreDatabase = $taskDatabase + '_restore'
$taskBootstrapDatabase = $taskDatabase + '_bootstrap'
$taskPreviewDatabase = $taskDatabase + '_preview'
$taskInitFile = Join-Path $taskRun 'initialize-test.sql'
$taskInitSQL = "ALTER USER 'root'@'localhost' IDENTIFIED BY '$taskPassword';`nCREATE DATABASE $taskDatabase CHARACTER SET utf8mb4 COLLATE utf8mb4_bin;`nCREATE DATABASE $taskAppDatabase CHARACTER SET utf8mb4 COLLATE utf8mb4_bin;`nCREATE USER 'launch_test'@'127.0.0.1' IDENTIFIED BY '$taskPassword';`nGRANT ALL ON $taskDatabase.* TO 'launch_test'@'127.0.0.1';`nGRANT ALL ON $taskAppDatabase.* TO 'launch_test'@'127.0.0.1';`n"
[IO.File]::WriteAllText($taskInitFile, $taskInitSQL, [Text.UTF8Encoding]::new($false))
[IO.File]::AppendAllText($taskInitFile, "CREATE DATABASE $taskRestoreDatabase CHARACTER SET utf8mb4 COLLATE utf8mb4_bin;`nGRANT ALL ON $taskRestoreDatabase.* TO 'launch_test'@'127.0.0.1';`n", [Text.UTF8Encoding]::new($false))
[IO.File]::AppendAllText($taskInitFile, "CREATE DATABASE $taskPreviewDatabase CHARACTER SET utf8mb4 COLLATE utf8mb4_bin;`nGRANT ALL ON $taskPreviewDatabase.* TO 'launch_test'@'127.0.0.1';`n", [Text.UTF8Encoding]::new($false))
[IO.File]::AppendAllText($taskInitFile, "CREATE DATABASE $taskBootstrapDatabase CHARACTER SET utf8mb4 COLLATE utf8mb4_bin;`nGRANT ALL ON $taskBootstrapDatabase.* TO 'launch_test'@'127.0.0.1';`n", [Text.UTF8Encoding]::new($false))
$taskServerArgs = @('--no-defaults', ('--basedir="' + $taskBase + '"'), ('--datadir="' + $taskData + '"'), '--bind-address=127.0.0.1', "--port=$taskPort", '--mysqlx=OFF', '--skip-name-resolve', ('--init-file="' + $taskInitFile + '"'), '--console')
$taskServer = Start-Process -FilePath $taskMySQLD -ArgumentList $taskServerArgs -WindowStyle Hidden -PassThru -RedirectStandardOutput (Join-Path $taskRun 'server.out') -RedirectStandardError (Join-Path $taskRun 'server.err')
$taskPreviousDSN = $env:LAUNCH_TEST_DATABASE_DSN
$taskPreviousAppDSN = $env:LAUNCH_TEST_APP_DATABASE_DSN
$taskPreviousRestoreDSN = $env:LAUNCH_TEST_RESTORE_DATABASE_DSN
$taskPreviousBootstrapDSN = $env:LAUNCH_TEST_BOOTSTRAP_DATABASE_DSN
$taskTestExit = 1
$taskPreviewProcess = $null
try {
    $taskReady = $false
    for ($taskAttempt = 0; $taskAttempt -lt 100; $taskAttempt++) {
        if ($taskServer.HasExited) { throw ('Test MySQL exited. Logs: ' + $taskRun) }
        # Read only this process's own startup log; network access is tested by Go.
        $taskLog = Join-Path $taskRun 'server.err'
        if (Test-Path -LiteralPath $taskLog) {
            $taskReady = [bool](Select-String -LiteralPath $taskLog -Pattern 'ready for connections' -Quiet)
        }
        if ($taskReady) { break }
        Start-Sleep -Milliseconds 200
    }
    if (-not $taskReady) { throw ('Test MySQL did not become ready. Logs: ' + $taskRun) }
    $env:LAUNCH_TEST_DATABASE_DSN = "launch_test:$taskPassword@tcp(127.0.0.1:$taskPort)/$taskDatabase"
    $env:LAUNCH_TEST_APP_DATABASE_DSN = "launch_test:$taskPassword@tcp(127.0.0.1:$taskPort)/$taskAppDatabase"
    $env:LAUNCH_TEST_RESTORE_DATABASE_DSN = "launch_test:$taskPassword@tcp(127.0.0.1:$taskPort)/$taskRestoreDatabase"
    $env:LAUNCH_TEST_BOOTSTRAP_DATABASE_DSN = "launch_test:$taskPassword@tcp(127.0.0.1:$taskPort)/$taskBootstrapDatabase"
    Push-Location -LiteralPath $taskRoot
    try {
        go test ./... -count=1
        $taskTestExit = $LASTEXITCODE
        if ($Preview -and $taskTestExit -eq 0) {
			go build -o (Join-Path $taskRoot 'bin\launch.exe') ./cmd/launch
			if ($LASTEXITCODE -ne 0) { throw 'Preview build failed.' }
			if ($PreviewRestore) {
				$taskRestorePath = [IO.Path]::GetFullPath($PreviewRestore)
				if (-not (Test-Path -LiteralPath $taskRestorePath -PathType Leaf)) { throw 'Preview restore file does not exist.' }
				$taskMySQLClient = Join-Path $MySQLBin 'mysql.exe'
				$taskPreviousMySQLPwd = $env:MYSQL_PWD
				try {
					$env:MYSQL_PWD = $taskPassword
					# Let the MySQL client read the dump as bytes. PowerShell 5.1 text
					# pipelines alter binary literals and escaping in otherwise valid dumps.
					$taskRestoreInfo = [Diagnostics.ProcessStartInfo]::new($taskMySQLClient)
					$taskRestoreInfo.UseShellExecute = $false
					$taskRestoreInfo.CreateNoWindow = $true
					$taskRestoreInfo.RedirectStandardInput = $true
					$taskRestoreInfo.RedirectStandardError = $true
					$taskRestoreInfo.Arguments = "--no-defaults --binary-mode --default-character-set=utf8mb4 --host=127.0.0.1 --port=$taskPort --user=launch_test $taskPreviewDatabase"
					$taskRestoreProcess = [Diagnostics.Process]::Start($taskRestoreInfo)
					$taskRestoreStream = [IO.File]::OpenRead($taskRestorePath)
					try {
						$taskRestoreStream.CopyTo($taskRestoreProcess.StandardInput.BaseStream)
						$taskRestoreProcess.StandardInput.Close()
					} finally { $taskRestoreStream.Dispose() }
					$taskRestoreError = $taskRestoreProcess.StandardError.ReadToEnd()
					$taskRestoreProcess.WaitForExit()
					if ($taskRestoreProcess.ExitCode -ne 0) { throw ("Preview database restore failed.`n" + $taskRestoreError) }
				} finally { $env:MYSQL_PWD = $taskPreviousMySQLPwd }
			}
			$taskPreviewURL = "http://localhost:$PreviewPort"
			# Windows can prefer ::1 for localhost while the preview intentionally
			# listens on IPv4. Use the literal address for script-owned requests.
			$taskPreviewAPIURL = "http://127.0.0.1:$PreviewPort"
            $taskPreviewInfo = [Diagnostics.ProcessStartInfo]::new((Join-Path $taskRoot 'bin\launch.exe'))
            $taskPreviewInfo.UseShellExecute = $false
            $taskPreviewInfo.CreateNoWindow = $true
            $taskPreviewInfo.WorkingDirectory = $taskRoot
            $taskPreviewInfo.Environment['LAUNCH_ENV'] = 'development'
            # The preview must never reuse integration-test rows. A dedicated
            # database keeps fake providers, UUIDs and transactions out of the UI.
            $taskPreviewInfo.Environment['LAUNCH_DATABASE_DSN'] = "launch_test:$taskPassword@tcp(127.0.0.1:$taskPort)/$taskPreviewDatabase"
            $taskPreviewInfo.Environment['LAUNCH_MASTER_KEY'] = [Convert]::ToBase64String([byte[]](1..32 | ForEach-Object { 9 }))
            $taskPreviewInfo.Environment['LAUNCH_PUBLIC_URL'] = $taskPreviewURL
            $taskPreviewInfo.Environment['LAUNCH_HTTP_ADDR'] = "127.0.0.1:$PreviewPort"
            $taskPreviewInfo.Environment['LAUNCH_ADMIN_EMAIL'] = 'owner@example.invalid'
            $taskPreviewInfo.Environment['LAUNCH_ADMIN_PASSWORD'] = 'owner-password-for-tests'
            $taskPreviewProcess = [Diagnostics.Process]::Start($taskPreviewInfo)

			$taskPreviewReady = $false
			for ($taskAttempt = 0; $taskAttempt -lt 100; $taskAttempt++) {
				if ($taskPreviewProcess.HasExited) { throw 'Preview process exited during startup.' }
				try {
					$taskHealth = Invoke-WebRequest -UseBasicParsing -Uri "$taskPreviewAPIURL/healthz" -TimeoutSec 2
					$taskPreviewReady = $taskHealth.StatusCode -eq 200
				} catch { $taskPreviewReady = $false }
				if ($taskPreviewReady) { break }
				Start-Sleep -Milliseconds 100
			}
			if (-not $taskPreviewReady) { throw 'Preview HTTP server did not become ready.' }

			$taskHeaders = @{ Origin = $taskPreviewURL }
			$taskRequestBodyPath = Join-Path $taskRun 'preview-request.json'
			$taskCookiePath = Join-Path $taskRun 'preview-cookies.txt'
			$taskSite = (& curl.exe --fail-with-body --silent --show-error --max-time 10 --url "$taskPreviewAPIURL/api/v1/site?environment=sandbox") | ConvertFrom-Json
			if (-not $PreviewRestore) {
				$taskLoginBody = @{ email = 'owner@example.invalid'; password = 'owner-password-for-tests'; kind = 'website' } | ConvertTo-Json
				Invoke-TaskJson -Method Post -Uri "$taskPreviewAPIURL/api/v1/auth/login" -Headers $taskHeaders -Body $taskLoginBody -BodyPath $taskRequestBodyPath -CookiePath $taskCookiePath | Out-Null
				$taskCSRFCookie = Get-Content -LiteralPath $taskCookiePath | ForEach-Object {
					$taskCookieFields = $_ -split "`t"
					if ($taskCookieFields.Count -ge 7 -and $taskCookieFields[5] -eq 'launch_csrf') { $taskCookieFields[6] }
				} | Select-Object -First 1
				if (-not $taskCSRFCookie) { throw 'Preview login did not return a CSRF cookie.' }
				$taskAdminHeaders = @{ Origin = $taskPreviewURL; 'X-CSRF-Token' = $taskCSRFCookie }
				$taskChangeBody = @{ current_password = 'owner-password-for-tests'; new_password = 'owner-password-changed-for-tests' } | ConvertTo-Json
				Invoke-TaskJson -Method Post -Uri "$taskPreviewAPIURL/api/v1/me/password" -Headers $taskAdminHeaders -Body $taskChangeBody -BodyPath $taskRequestBodyPath -CookiePath $taskCookiePath | Out-Null
				$taskLoginBody = @{ email = 'owner@example.invalid'; password = 'owner-password-changed-for-tests'; kind = 'website' } | ConvertTo-Json
				Invoke-TaskJson -Method Post -Uri "$taskPreviewAPIURL/api/v1/auth/login" -Headers $taskHeaders -Body $taskLoginBody -BodyPath $taskRequestBodyPath -CookiePath $taskCookiePath | Out-Null
				$taskCSRFCookie = Get-Content -LiteralPath $taskCookiePath | ForEach-Object { $taskFields = $_ -split "`t"; if ($taskFields.Count -ge 7 -and $taskFields[5] -eq 'launch_csrf') { $taskFields[6] } } | Select-Object -First 1
				$taskAdminHeaders['X-CSRF-Token'] = $taskCSRFCookie

				$taskBrandBody = @{ name = 'Updated Brand'; tagline = ''; description = ''; support_email = 'support@example.invalid'; logo_data_url = ''; theme = 'forest' } | ConvertTo-Json
				Invoke-TaskJson -Method Put -Uri "$taskPreviewAPIURL/api/v1/admin/brand" -Headers $taskAdminHeaders -Body $taskBrandBody -BodyPath $taskRequestBodyPath -CookiePath $taskCookiePath | Out-Null
				$taskProductBody = @{
					name = 'Test Editor'; slug = 'test-editor'; status = 'published'
					content = @{ summary = 'A product'; description = ''; features = @('Export'); requirements = ''; download_url = 'https://example.invalid/download'; version = '1.0' }
				} | ConvertTo-Json -Depth 5
				Invoke-TaskJson -Method Put -Uri "$taskPreviewAPIURL/api/v1/admin/product" -Headers $taskAdminHeaders -Body $taskProductBody -BodyPath $taskRequestBodyPath -CookiePath $taskCookiePath | Out-Null
				$taskPlanBody = @{
					name = 'Pro'; currency = 'USD'; amount_minor = 1299; purchase_type = 'fixed_term'; term_unit = 'month'; term_count = 1
					snapshot = @{ features = @('Export'); max_devices = 2; license_model = 'node_locked'; offline_summary = 'Periodic online verification'; offline_mode = ''; heartbeat_seconds = 0; offline_seconds = 0; session_seconds = 0; self_release_limit = 2 }
					allow_purchase = $true; allow_renewal = $true
				} | ConvertTo-Json -Depth 5
				$taskPlan = Invoke-TaskJson -Method Post -Uri "$taskPreviewAPIURL/api/v1/admin/plans" -Headers $taskAdminHeaders -Body $taskPlanBody -BodyPath $taskRequestBodyPath -CookiePath $taskCookiePath
				$taskPublishBody = @{ version_id = $taskPlan.data.version_id; environment = 'sandbox' } | ConvertTo-Json
				Invoke-TaskJson -Method Post -Uri "$taskPreviewAPIURL/api/v1/admin/plans/$($taskPlan.data.id)/publish" -Headers $taskAdminHeaders -Body $taskPublishBody -BodyPath $taskRequestBodyPath -CookiePath $taskCookiePath | Out-Null
			}

			Write-Output "Clean preview: $taskPreviewURL"
			Write-Output 'Preview login: owner@example.invalid / owner-password-changed-for-tests'
            $taskPreviewProcess.WaitForExit()
        }
    } finally { Pop-Location }
} finally {
    $env:LAUNCH_TEST_DATABASE_DSN = $taskPreviousDSN
    $env:LAUNCH_TEST_APP_DATABASE_DSN = $taskPreviousAppDSN
    $env:LAUNCH_TEST_RESTORE_DATABASE_DSN = $taskPreviousRestoreDSN
    $env:LAUNCH_TEST_BOOTSTRAP_DATABASE_DSN = $taskPreviousBootstrapDSN
    Stop-TaskProcess $taskPreviewProcess
    Stop-TaskProcess $taskServer
	Stop-TaskMySQLProcess $taskData
}
exit $taskTestExit
