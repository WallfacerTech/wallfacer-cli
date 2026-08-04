# VMs

## Read

```bash
wallfacer vms list -o json
wallfacer vms get <vm-id> -o json
```

VM statuses: `creating`, `running`, `stopping`, `stopped`, `failed`. A running VM also has `ready: true` when fully booted.

## Create

The `up` shortcut handles the full flow — wait for snapshot, create VM, wait for boot:

```bash
wallfacer up <environment-id>
```

Manual approach (if needed):

```bash
echo '{"environment_id":"<uuid>","snapshot_id":"<snapshot-id>"}' | wallfacer vms create
```

Optional fields: `env` (per-boot env var overrides).

**Important:** Creating a VM without `snapshot_id` gives a bare VM with no repo or setup. Always wait for the environment's snapshot to reach `"ready"` status first (see environments reference), then pass both IDs.

Returns the new VM with `id`. VMs boot asynchronously — poll `vms get` until `status == "running"` and `ready == true`. With a snapshot, boot takes ~2s; without, setup runs fresh (~30s+).

After snapshot reaches "ready", there may be a 5-15s propagation delay before the first `vms create` succeeds. The `up` command handles this automatically; if creating manually, retry after 10-15s.

## Execute a command

Shortcut (preferred):

```bash
wallfacer exec --vm <vm-id> -- ls -la /workspace
wallfacer exec --vm <vm-id> --dir /workspace --timeout 30 -- ls -la
```

Stdin form (still works):

```bash
echo '{"command":"ls -la /workspace","working_directory":"/workspace","timeout":30}' | wallfacer vms commands <vm-id>
```

Response shape:

```json
{ "exit_code": 0, "stdout": "…", "stderr": "" }
```

`timeout` is 1-300 seconds. Long-running commands can hit API gateway timeouts — start a command run instead (below).

## Command runs

A run is the tracked form of `exec`: the record is written before the process starts, so it stays addressable even if the response to the request that started it is lost, and the VM is not reclaimed as idle while the run is in flight. Use it for anything that outlives a request (test suites, builds, soak scripts); use `exec` for a quick command.

```bash
wallfacer runs start <vm-id> --name test --wait      # start and follow to completion
wallfacer runs start <vm-id> --name test             # start in the background, prints the run
wallfacer runs list <vm-id> --active true            # runs still holding the VM
wallfacer runs get <vm-id> <run-id> --since 4096     # record plus output from a byte cursor
wallfacer runs follow <vm-id> <run-id>               # stream output to completion
wallfacer runs cancel <vm-id> <run-id>
```

`start` takes either `--name` (a command declared in the manifest the VM booted from) or an ad hoc command after `--`, never both. Other flags: `--dir`, `--env KEY=VALUE` (repeatable), `--timeout` (seconds, default 1800, max 14400), `--wait-seconds` (how long each read waits for the run to finish, default 10), `--poll` (seconds between reads, default 1).

`--wait` and `follow` write the command's output to stdout and a one-line summary to stderr, then exit with the remote command's exit code — so `wallfacer runs start <vm-id> --name test --wait` fails the shell exactly when the suite fails. A run that ended without an exit code exits `124` (timed out), `130` (canceled), or `1`.

Run statuses: `pending`, `running`, `completed`, `failed`, `canceled`, `timed_out`. A named command runs one at a time per VM; starting one that is already in flight is rejected. `runs cancel` exits 1 when the cancellation was recorded but not delivered — the run keeps going until its deadline, so repeat the command to retry.

## Logs

```bash
wallfacer vms logs <vm-id>                    # list all log sources
wallfacer vms log <vm-id> <source>            # read a specific source
```

Per-manifest-step logs. Live while the VM is running; persisted (drained at teardown) after destruction.

## Delete

```bash
wallfacer vms delete <vm-id>
```

Irreversible — the VM and all its resources are destroyed.

## Simulator (iOS)

Only applicable to VMs running the iOS simulator stack.

```bash
wallfacer vms simulator <vm-id> -o json      # state: { ready, websocket_url, … }
wallfacer vms simulator-screenshot <vm-id>   # returns a signed URL
wallfacer vms simulator-logs <vm-id>
wallfacer vms simulator-builds <vm-id> -o json
```

Wait for `ready == true` in the simulator state before driving the simulator over the WebSocket URL.