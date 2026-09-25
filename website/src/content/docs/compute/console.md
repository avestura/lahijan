---
title: Console and exec
description: Open an interactive shell in an instance, run one-off commands through the API, view a virtual machine's screen and read console logs.
---

You can reach the inside of an instance in four ways: an interactive shell in the browser, a one-shot command through the API, the graphical screen of a virtual machine, and the console log in the **Logs** tab. None of them need SSH or a public IP address.

## Interactive shell

Open the instance and select the **Console** tab. When the instance is running and you have permission, the terminal connects on its own and gives you a shell. Everything you type goes straight to the shell, and its output comes back to the terminal.

- The shell is `/bin/sh`, run as the instance's default user.
- The terminal follows the size of the browser window, so full-screen programs such as `vim`, `htop` and `less` draw correctly.
- **Disconnect** closes the session. **Connect** opens a new one.
- Leaving the tab or the page closes the session, and the shell process inside the instance ends.
- Every tab or window you open starts its own separate shell. There is no way to reattach to an earlier session.

The shell works for both containers and virtual machines. If the instance is stopped, the tab shows **Start the instance to open a console.** If your role lacks the permission, it shows **You do not have permission to open a console.**

Opening a shell needs the `compute.instance.console.exec` permission, which the default Member, Admin and Owner roles have. Each session opened is recorded in the audit log as `compute.instance.console.exec.connect`, even when the connection then fails.

> [!NOTE]
> The instance's image must contain `/bin/sh`. Minimal images without a shell cannot use this console.

### Connect from your own tools

The shell runs over a WebSocket at:

```http
GET /api/v1/compute/instances/{instanceId}/console
```

The dashboard authenticates with your session cookie and passes the tenant as the `tenant_id` query parameter, because browsers cannot set custom headers on a WebSocket. A client that can set headers may send `Authorization: Bearer <token>` and `X-Tenant-Id` instead.

The optional `cmd` query parameter picks the program to run, as a comma-separated argument list. For example `?cmd=/bin/bash` or `?cmd=bash,-c,echo%20hi`. Without it the server runs `/bin/sh`.

On the socket, text and binary frames you send are passed to the program as keyboard input, and its output comes back as frames. To resize the terminal, send a JSON text frame on the same socket:

```json
{ "type": "resize", "cols": 132, "rows": 40 }
```

Before the upgrade, the server returns a normal error response when something is wrong: `404` for an unknown instance, `409` when the instance is not running, and `503` when the shell cannot be opened.

## Run a single command

For scripts and automation, `POST /api/v1/compute/instances/{instanceId}/exec` runs one command, waits for it to finish and returns its output. The instance must be running; otherwise the call returns `409`.

```sh
curl -X POST https://lahijan.example.com/api/v1/compute/instances/$INSTANCE_ID/exec \
  -H "Authorization: Bearer $LAHIJAN_TOKEN" \
  -H "X-Tenant-Id: $TENANT_ID" \
  -H "Content-Type: application/json" \
  -d '{
    "command": ["sh", "-c", "uptime && df -h /"],
    "environment": { "LANG": "C.UTF-8" },
    "cwd": "/root",
    "user": 0,
    "group": 0
  }'
```

| Field           | Required | Meaning                                                                          |
| --------------- | -------- | -------------------------------------------------------------------------------- |
| `command`       | yes      | The program and its arguments, as a list. Must not be empty.                     |
| `environment`   | no       | Extra environment variables.                                                     |
| `user`, `group` | no       | Numeric user and group IDs. The default `0` runs as the instance's default user. |
| `cwd`           | no       | Working directory.                                                               |
| `stdin`         | no       | Text sent to the program's standard input.                                       |

The response carries `exitCode`, and `stdout` and `stderr` when the program printed anything. Output that is not valid UTF-8 is returned base64-encoded.

```json
{ "exitCode": 0, "stdout": " 10:02:11 up 3 days, ..." }
```

Running a command this way needs `compute.instance.start` and is recorded in the audit log.

## Graphical console (virtual machines)

Virtual machines have an extra tab, **Console (Graphical)**, that shows the machine's screen in the browser. Use it for installers, boot menus or a desktop. Containers have no screen and do not get this tab.

The tab offers **Connect**, **Disconnect**, **Fullscreen** and **Scale to fit**. **Clipboard sync** opens a text box whose content is copied to your local clipboard; sending clipboard text into the virtual machine is not supported yet.

The graphical console needs the `compute.instance.console.vnc` permission and a running virtual machine. It is served over a WebSocket at `GET /api/v1/compute/instances/{instanceId}/vnc`, which returns `409` for a container or a stopped machine and `503` when the screen cannot be reached (for example, while the machine is still booting). Each connection is recorded in the audit log as `compute.instance.console.vnc.connect`.

> [!NOTE]
> The graphical console client is a separate download that your operator installs on the server. If the tab shows **noVNC assets not installed**, ask your operator to complete that step.

## Console log

The **Logs** tab lists the log files kept for the instance. Pick one from the **Log file** list:

- `console.log` is the instance's console output: boot messages and anything written to the console. It is always listed, and the tab refreshes it every 5 seconds while it is selected.
- The other entries are runtime logs kept by the system for the instance. Click **Refresh** to reload them.

Only the last 1 MiB of a log is shown; the tab then says **Showing the last 1 MiB of this log.**

> [!NOTE]
> The console output is collected by the Lahijan server while you read it and is kept in memory. It starts empty again after the server restarts.

Through the API, `GET /api/v1/compute/instances/{instanceId}/logs` returns the list of log names and `GET /api/v1/compute/instances/{instanceId}/logs/{logFile}` returns one log as `name`, `content` and `truncated`. Both need `compute.instance.read`, so the Viewer role can read logs.

## Limitations

- The shell and command endpoints need a running instance.
- One shell session per open tab; sessions end when the tab closes and cannot be resumed.
- The graphical console is for virtual machines only and depends on the operator installing its client files.
- Clipboard sync in the graphical console is one-way (to your computer).
- The console log shows at most the last 1 MiB and is not kept across server restarts.
