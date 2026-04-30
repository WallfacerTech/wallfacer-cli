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

`timeout` is 1-300 seconds. Long-running commands can hit API gateway timeouts — move heavy work into manifest `setup` steps instead.

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
