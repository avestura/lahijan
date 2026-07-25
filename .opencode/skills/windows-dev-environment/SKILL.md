# Skill: Windows Dev Environment

> Use when running or debugging the Lahijan dev stack on a Windows host.
> Covers Docker Desktop quirks, CRLF→LF issues, process management, and
> port conflicts that don't exist on Linux/macOS.

## When to load this skill

- The user is on Windows (`platform: win32`) and trying to run `make dev-up`,
  `scripts/Run-Dev.ps1`, or any docker-compose dev workflow.
- A bind-mounted shell script fails inside a Linux container with
  `env: can't execute 'bash\r'` or similar.
- `Start-Process "npm"` fails with "cannot find all the information required."
- A dev server (Vite, Docusaurus) appears to start but the health probe
  never succeeds.
- A process is killed but the port stays occupied.

## CRLF → LF on bind-mounted scripts

**Problem:** Git on Windows checks out shell scripts with CRLF line
endings. When these files are bind-mounted into a Linux container
(`./postgres/init.sh:/docker-entrypoint-initdb.d/01-init.sh:ro`), the
shebang `#!/usr/bin/env bash\r` fails because `bash\r` doesn't exist.

**Symptom:** Container starts, init script silently never runs,
databases/roles never get created, app fails with `password
authentication failed for user "lahijan"` even though the password is
correct.

**Fix:** Normalize line endings before `docker compose up`. The
`scripts/Run-Dev.ps1` script does this automatically:

```powershell
foreach ($f in @(
    "deployments\postgres\init.sh",
    "deployments\powerdns\init.sh",
    "deployments\powerdns\pdns.conf.template"
)) {
    $text = [IO.File]::ReadAllText($f)
    $fixed = $text -replace "`r`n", "`n"
    [IO.File]::WriteAllText($f, $fixed, [New-Object Text.UTF8Encoding $false])
}
```

**Files to check:** Any `.sh`, `.template`, `.conf`, or `.json` file
under `deployments/` that gets bind-mounted into a Linux container.

## Start-Process + npm on Windows

**Problem:** `npm` on Windows is a `.cmd` batch wrapper, not an `.exe`.
`Start-Process -FilePath "npm"` fails with "This command cannot be run
completely because the system cannot find all the information required."

**Fix:** Always launch npm via `cmd.exe /c`:

```powershell
$proc = Start-Process -FilePath "cmd.exe" `
    -ArgumentList @("/c", "npm run dev") `
    -WorkingDirectory $WorkDir `
    -PassThru
```

**Passing args to the underlying script:** npm requires `--` before
passthrough args:

```powershell
# WRONG — npm swallows --port:
"npm run docusaurus start --port 3000"

# RIGHT — the -- separator passes args to docusaurus:
"npm run docusaurus -- start --port 3000"
```

## Process tree cleanup (taskkill /T)

**Problem:** `Stop-Process -Id $pid -Force` kills only the immediate
process. When the process is `cmd.exe → npm → node vite`, killing
`cmd.exe` leaves `node vite` running. The orphaned node process keeps
holding the port, so the next launch falls back to a random port and
health probes fail.

**Fix:** Always use `taskkill /F /T /PID` to kill the entire tree:

```powershell
& taskkill /F /T /PID $pid 2>$null | Out-Null
```

For port-based cleanup (kills anything listening on a port):

```powershell
function Stop-Port([int]$port) {
    $conns = Get-NetTCPConnection -State Listen -LocalPort $port -ErrorAction SilentlyContinue
    foreach ($c in $conns) {
        if ($c.OwningProcess -and $c.OwningProcess -ne 0) {
            & taskkill /F /T /PID $c.OwningProcess 2>$null | Out-Null
        }
    }
}
```

## IPv4 vs IPv6 health probes

**Problem:** Windows resolves `localhost` to IPv6 `::1` first. Go's
default `0.0.0.0` listener binds IPv4-only. Vite's dev server binds
IPv6-only by default. A probe to `http://localhost:PORT/` may hit the
wrong stack and time out.

**Symptom:** The dev server is running (you can see it in `netstat`),
logs show it started, but `Invoke-WebRequest http://localhost:PORT/`
times out.

**Fix:** Probe multiple hostname forms:

```powershell
$urls = @(
    "http://127.0.0.1:$Port$Path"
    "http://localhost:$Port$Path"
    "http://[::1]:$Port$Path"
)
foreach ($u in $urls) {
    try {
        $resp = Invoke-WebRequest -UseBasicParsing $u -TimeoutSec 2
        if ($resp.StatusCode -eq $expected) { return $true }
    } catch { /* try next */ }
}
```

For the Go backend specifically: probe `127.0.0.1` (IPv4) because Fiber
binds `0.0.0.0`. For Vite/Docusaurus: probe `localhost` or `[::1]`.

## Port 8080 conflict with Docker Desktop

**Problem:** Docker Desktop on Windows uses port 8080 internally for
its WSL relay (`wslrelay` / `com.docker.backend`). Binding a container
to `0.0.0.0:8080` fails with "port is already allocated."

**Fix:** Either:
- Remap the conflicting service to a different host port (e.g.
  `SEAWEEDFS_VOLUME_PORT=18080` in `.env`).
- Or use `0.0.0.0:8080` for the Go backend — Docker's relay is IPv6-only
  (`::`) so IPv4 binding works.

Check what's holding a port:

```powershell
Get-NetTCPConnection -LocalPort 8080 -State Listen | Select-Object LocalAddress, OwningProcess
Get-Process -Id $pidFromAbove
```

## Docker Desktop can't resolve host.docker.internal

**Problem:** Some Docker Desktop setups don't expose
`host.docker.internal` to containers started via `docker run` (as
opposed to `docker compose`). The migrate container fails with `lookup
host.docker.internal on 8.8.4.4:53: no such host`.

**Fix:** Attach the container to the compose network and use service
names instead:

```powershell
docker run --rm --network lahijan-dev_default `
    -v "${migrationsMount}:/migrations" migrate/migrate `
    -path=/migrations -database="postgres://lahijan:lahijan@postgres:5432/lahijan?sslmode=disable" up
```

## Stdout/stderr redirection for Start-Process

**Problem:** `Start-Process` with `-WindowStyle Minimized` (or
`-NoNewWindow`) does NOT capture stdout/stderr by default. When a
launched process fails, there's no log to inspect — the script just
sees exit code 1.

**Fix:** Always pass `-RedirectStandardOutput` and
`-RedirectStandardError`:

```powershell
$proc = Start-Process -FilePath "cmd.exe" `
    -ArgumentList @("/c", $cmdLine) `
    -WorkingDirectory $WorkDir `
    -RedirectStandardOutput $stdoutLog `
    -RedirectStandardError  $stderrLog `
    -WindowStyle Minimized `
    -PassThru
```

## SeaweedFS version drift

**Problem:** The compose file pins `chrislusf/seaweedfs:3.61` but the
config uses `weed mini` (removed in 3.61) and the old s3.json schema
(`credentials` as a single object with snake_case keys).

**Fix:**
- Use `weed server` instead of `weed mini`.
- Drop `-filer.postgres.*` flags (not accepted by `weed server`).
- Use the modern s3.json schema: `credentials` as an array with
  camelCase keys (`accessKey`, `secretKey`), `isAdmin` instead of
  `is_admin`.
- Patch the healthcheck: `weed server -ip=seaweedfs` binds the filer to
  the container's service-name IP, not localhost. Probe
  `http://seaweedfs:8888/` not `http://localhost:8888/`.

## Quick diagnostic checklist

When the dev stack won't come up on Windows:

1. **Are init scripts LF?** `Get-Content deployments\postgres\init.sh -Raw | Select-String "\r\n"` — should be empty.
2. **Is the port free?** `Get-NetTCPConnection -LocalPort <port> -State Listen`
3. **Is the process alive?** `Get-Process -Id <pid>` — check it didn't exit.
4. **What did stderr say?** `Get-Content dist\dev-logs\<service>.stderr.log -Tail 20`
5. **Does the probe hit the right IP stack?** Try both `127.0.0.1` and `localhost`.
6. **Can the migrate container reach Postgres?** Use `--network lahijan-dev_default` + service name `postgres`, not `host.docker.internal`.
