# AnyDesk Killer Remote

V2 uses an n8n form to enqueue an allowlisted `KILL_ANYDESK` command in a private Redis list. The Windows agent consumes it over an SSH local tunnel; Redis is never exposed publicly.

## Contents

- `n8n/Kill AnyDesk V2.json`: portable n8n workflow export.
- `agent/kill_anydesk_agent.pyw`: Windows agent.
- `agent/config.example.json`: safe configuration template.
- `agent/requirements.txt`: Python dependencies.
- `agent-go/`: Go implementation of the Windows agent (recommended for deployment).
- `.github/workflows/agent-go.yml`: CI for every push/PR; versioned Releases only for `v*` tags.

## Install the Go agent (recommended)

1. Create a restricted SSH user on the VPS and generate a dedicated key pair.
2. Copy the private key and a verified `known_hosts` file to the computer. Never leave `knownHosts` empty.
3. Copy `agent/config.example.json` to a local `config.json`, set the `machineId`, and keep it out of Git.
4. Download `anydesk-killer-agent-windows-amd64.zip` from a [GitHub Release](https://github.com/hugoalves-dev/anydesk-killer-remote/releases), extract it, then run:

   ```powershell
   .\anydesk-killer-agent-windows-amd64.exe --config C:\AnyDeskKiller\config.json
   ```

The executable accepts only an unexpired, version-1 `KILL_ANYDESK` envelope targeted to its configured machine ID. The only optional argument is `reopenAnyDesk: true`; when set, it starts AnyDesk from known local paths only after it terminated at least one process. There is no generic shell or remote-command execution path.

To publish a new binary, push a tag such as `v1.0.0`. The Release includes a ZIP and its SHA-256 checksum. Normal pushes and pull requests only run validation/build and upload a temporary Actions artifact.

## Restricted SSH access to Redis

The SSH account used by the agent must exist only to open a local TCP tunnel to Redis. It must not have password access, an interactive shell, command execution, SFTP/SCP, reverse forwarding, X11 forwarding, or SSH-agent forwarding.

The examples below assume:

- Ubuntu or Debian on the VPS;
- OpenSSH listening on the VPS;
- Redis reachable from the SSH server at `127.0.0.1:6379`;
- the Windows machine ID is `jcpc38`.

If Redis is reachable through another hostname, replace `127.0.0.1` everywhere. The value must be identical in `authorized_keys`, `sshd_config`, and `config.json`; OpenSSH compares the permitted destination literally.

### 1. Keep Redis private

When Redis runs with Docker Compose, publish it only on loopback:

```yaml
services:
  redis:
    image: redis:7-alpine
    ports:
      - "127.0.0.1:6379:6379"
```

Apply and verify:

```bash
docker compose up -d redis
nc -vz 127.0.0.1 6379
```

Do not publish Redis as `0.0.0.0:6379:6379`.

### 2. Create the `remote-agent` user

```bash
sudo adduser --disabled-password --gecos "" remote-agent
sudo usermod --shell /usr/sbin/nologin remote-agent
```

Do not add this user to `sudo`, `docker`, or other privileged groups.

Create its SSH directory:

```bash
sudo install -d -m 700 -o remote-agent -g remote-agent /home/remote-agent/.ssh
sudo touch /home/remote-agent/.ssh/authorized_keys
sudo chown remote-agent:remote-agent /home/remote-agent/.ssh/authorized_keys
sudo chmod 600 /home/remote-agent/.ssh/authorized_keys
```

### 3. Generate a dedicated key on Windows

Run in PowerShell:

```powershell
New-Item -ItemType Directory -Force "$env:USERPROFILE\.ssh" | Out-Null

ssh-keygen -t ed25519 -a 100 `
  -f "$env:USERPROFILE\.ssh\anydesk-killer-redis" `
  -C "anydesk-killer-jcpc38"
```

This creates:

```text
C:\Users\vitor.hugo\.ssh\anydesk-killer-redis
C:\Users\vitor.hugo\.ssh\anydesk-killer-redis.pub
```

The file without `.pub` is the private key used by the agent. Never copy it to the VPS or commit it to Git.

Display the public key:

```powershell
Get-Content "$env:USERPROFILE\.ssh\anydesk-killer-redis.pub"
```

### 4. Restrict the key to Redis forwarding

On the VPS, edit:

```bash
sudo nano /home/remote-agent/.ssh/authorized_keys
```

Add the public key as one line, prefixed with these options:

```text
restrict,port-forwarding,permitopen="127.0.0.1:6379" ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAA... anydesk-killer-jcpc38
```

Then restore ownership and permissions:

```bash
sudo chown remote-agent:remote-agent /home/remote-agent/.ssh/authorized_keys
sudo chmod 600 /home/remote-agent/.ssh/authorized_keys
```

`restrict` disables forwarding and other SSH features by default. `port-forwarding` re-enables forwarding for this key, while `permitopen` restricts the reachable destination to Redis. The server configuration below additionally permits only local forwarding.

### 5. Restrict the user in OpenSSH

Append this block to `/etc/ssh/sshd_config`:

```text
Match User remote-agent
    PubkeyAuthentication yes
    PasswordAuthentication no
    KbdInteractiveAuthentication no
    AuthenticationMethods publickey

    AllowTcpForwarding local
    AllowStreamLocalForwarding no
    PermitOpen 127.0.0.1:6379
    PermitListen none

    PermitTTY no
    X11Forwarding no
    AllowAgentForwarding no
    PermitUserRC no
    MaxSessions 0
```

`MaxSessions 0` blocks shell, login, command, and subsystem sessions while still allowing forwarding. `AllowTcpForwarding local` blocks reverse tunnels, and `PermitOpen` limits the only allowed destination.

Validate before reloading OpenSSH:

```bash
sudo sshd -t
sudo systemctl reload ssh 2>/dev/null || sudo systemctl reload sshd
```

Inspect the effective configuration:

```bash
sudo sshd -T \
  -C user=remote-agent,host=ssh.hugojava.dev,addr=127.0.0.1 \
  | grep -E 'authenticationmethods|allowtcpforwarding|allowstreamlocalforwarding|permitopen|permitlisten|permittty|maxsessions'
```

### 6. Register and verify `known_hosts`

On the VPS, display every enabled host-key fingerprint:

```bash
sudo ssh-keygen -lf /etc/ssh/ssh_host_ed25519_key.pub
sudo ssh-keygen -lf /etc/ssh/ssh_host_ecdsa_key.pub
sudo ssh-keygen -lf /etc/ssh/ssh_host_rsa_key.pub
```

On Windows, collect the public host keys. For a non-default SSH port, OpenSSH stores the host as `[hostname]:port`:

```powershell
$knownHosts = "$env:USERPROFILE\.ssh\known_hosts"

New-Item -ItemType Directory -Force "$env:USERPROFILE\.ssh" | Out-Null
New-Item -ItemType File -Force $knownHosts | Out-Null

ssh-keyscan -p 2222 -t ed25519,ecdsa,rsa ssh.hugojava.dev 2>$null |
  Add-Content -Encoding ascii $knownHosts

ssh-keygen -F "[ssh.hugojava.dev]:2222" -f $knownHosts
```

Compare every collected key with the fingerprints obtained directly from the VPS before trusting it. `ssh-keyscan` alone does not authenticate the server.

### 7. Configure the agent

Example `config.json`:

```json
{
  "machineId": "jcpc38",
  "anyDeskExecutablePath": "C:\\\\Program Files (x86)\\\\AnyDesk\\\\AnyDesk.exe",
  "ssh": {
    "host": "ssh.hugojava.dev",
    "port": 2222,
    "username": "remote-agent",
    "clientKey": "C:\\Users\\vitor.hugo\\.ssh\\anydesk-killer-redis",
    "knownHosts": "C:\\Users\\vitor.hugo\\.ssh\\known_hosts"
  },
  "redis": {
    "remoteHost": "127.0.0.1",
    "remotePort": 6379
  },
  "logFile": "C:\\Users\\vitor.hugo\\Desktop\\agent.log"
}
```

`clientKey` must reference the private key and must not end in `.pub`. `knownHosts` must reference the `known_hosts` file itself, not the `.ssh` directory. `anyDeskExecutablePath` is optional: when provided, it is the first executable considered after a successful kill; if the file is absent or fails to launch, the agent falls back to the standard AnyDesk locations. Use a literal absolute path; environment variables are not expanded.

### 8. Test the tunnel manually

Open the tunnel from Windows:

```powershell
ssh -N `
  -L 6380:127.0.0.1:6379 `
  -i "$env:USERPROFILE\.ssh\anydesk-killer-redis" `
  -p 2222 `
  remote-agent@ssh.hugojava.dev
```

From another terminal, test Redis if `redis-cli` is installed:

```powershell
redis-cli -h 127.0.0.1 -p 6380 ping
```

Expected result:

```text
PONG
```

Confirm that command execution is blocked:

```powershell
ssh `
  -i "$env:USERPROFILE\.ssh\anydesk-killer-redis" `
  -p 2222 `
  remote-agent@ssh.hugojava.dev "id"
```

The command must fail because the account cannot create SSH sessions.

Finally, start the agent:

```powershell
.\anydesk-killer-agent-windows-amd64.exe --config .\config.json
```

### Troubleshooting

#### `SSH clientKey: CreateFile ... O sistema não pode encontrar o arquivo especificado`

Confirm that the private key exists and that `clientKey` points to the file without `.pub`:

```powershell
Get-ChildItem "$env:USERPROFILE\.ssh\anydesk-killer-redis*"
```

#### `SSH knownHosts: CreateFile %USERPROFILE%\.known_hosts ...`

The Go agent currently treats paths from `config.json` literally. It does not expand the CMD-style `%USERPROFILE%` placeholder. Use an absolute Windows path with escaped backslashes:

```json
"knownHosts": "C:\\Users\\vitor.hugo\\.ssh\\known_hosts"
```

The same rule applies to `clientKey` and `logFile`.

#### `transport failure ... read C:/Users/.../.ssh: Incorrect function.`

`knownHosts` points to a directory instead of the `known_hosts` file. Use the complete file path:

```json
"knownHosts": "C:\\Users\\vitor\\.ssh\\known_hosts"
```

Incorrect:

```json
"knownHosts": "C:\\Users\\vitor\\.ssh"
```

Create the file when it does not exist:

```powershell
New-Item -ItemType Directory -Force "$env:USERPROFILE\.ssh" | Out-Null
New-Item -ItemType File -Force "$env:USERPROFILE\.ssh\known_hosts" | Out-Null
```

#### `knownhosts: key is unknown` or `knownhosts: key mismatch`

The `known_hosts` file is readable, but it either has no entry for the configured host and port or contains only a different host-key algorithm. Remove stale entries and collect all host-key types enabled by the server:

```powershell
$knownHosts = "$env:USERPROFILE\.ssh\known_hosts"

ssh-keygen -R "[ssh.hugojava.dev]:2222" -f $knownHosts

ssh-keyscan -p 2222 -t ed25519,ecdsa,rsa ssh.hugojava.dev 2>$null |
  Add-Content -Encoding ascii $knownHosts

ssh-keygen -F "[ssh.hugojava.dev]:2222" -f $knownHosts
```

Before accepting the replacement, verify the fingerprints directly on the VPS:

```bash
sudo ssh-keygen -lf /etc/ssh/ssh_host_ed25519_key.pub
sudo ssh-keygen -lf /etc/ssh/ssh_host_ecdsa_key.pub
sudo ssh-keygen -lf /etc/ssh/ssh_host_rsa_key.pub
```

Do not blindly replace a previously trusted key. An unexpected host-key change may indicate a rebuilt server, a changed SSH endpoint, or a man-in-the-middle attack.

#### `WARNING: UNPROTECTED PRIVATE KEY FILE!` or `Permissions ... are too open`

Windows OpenSSH rejects private keys inherited by broad groups such as `Authenticated Users`, `Users`, or `Everyone`. Open PowerShell as Administrator and restrict the file to the current user:

```powershell
$key = "C:\AnyDeskKiller\anydesk-killer-redis"
$user = [System.Security.Principal.WindowsIdentity]::GetCurrent().Name

icacls $key /setowner "$user"
icacls $key /inheritance:r
icacls $key /remove:g "*S-1-5-11" "*S-1-5-32-545" "*S-1-1-0"
icacls $key /grant:r "${user}:(R)"
```

Replace `$key` with the actual private-key path and verify the resulting ACL:

```powershell
icacls $key
```

Then test authentication manually:

```powershell
ssh -p 2222 `
  -i $key `
  remote-agent@ssh.hugojava.dev
```

The SSH account may reject shell creation because it is intentionally tunnel-only. That is expected; the private key must no longer be rejected for open permissions.

#### `Permission denied (publickey)`

Check ownership, permissions, and SSH logs:

```bash
sudo ls -ld /home/remote-agent/.ssh
sudo ls -l /home/remote-agent/.ssh/authorized_keys
sudo journalctl -u ssh -f
```

Expected permissions:

```text
/home/remote-agent/.ssh                  700
/home/remote-agent/.ssh/authorized_keys 600
```

#### `administratively prohibited: open failed`

The requested destination does not match `permitopen` or `PermitOpen`. Check all three values:

```text
authorized_keys: permitopen="127.0.0.1:6379"
sshd_config:     PermitOpen 127.0.0.1:6379
config.json:     remoteHost 127.0.0.1 + remotePort 6379
```

#### `transport failure ... wsarecv: ... conexão existente pelo host remoto`

This Windows error can hide an SSH `direct-tcpip` rejection. The local tunnel listener may be open while the SSH server refuses the remote destination because `authorized_keys`, the effective `sshd_config`, and `config.json` do not permit the same literal `host:port`.

First verify Redis from both the container and the VPS host:

```bash
docker exec redis redis-cli PING
docker run --rm --network host redis:7-alpine \
  redis-cli -h 127.0.0.1 -p 6379 PING
sudo ss -lntp | grep ':6379'
```

Expected results are two `PONG` responses and a listener bound only to `127.0.0.1:6379`.

Then inspect every SSH restriction:

```bash
sudo cat /home/remote-agent/.ssh/authorized_keys

sudo grep -RniE \
  'Match|PermitOpen|AllowTcpForwarding|DisableForwarding|ForceCommand' \
  /etc/ssh/sshd_config /etc/ssh/sshd_config.d

sudo sshd -T \
  -C user=remote-agent,host=localhost,addr=127.0.0.1 \
  | grep -E 'allowtcpforwarding|permitopen|disableforwarding|forcecommand'
```

All three destinations must be identical:

```text
authorized_keys: permitopen="127.0.0.1:6379"
sshd_config:     PermitOpen 127.0.0.1:6379
config.json:     remoteHost 127.0.0.1 + remotePort 6379
```

Values such as `redis:6379`, `172.0.0.1:6379`, and `127.0.0.1:6379` are different to OpenSSH even when they eventually reach the same Redis instance.

After correcting `authorized_keys` or `sshd_config`, validate and reload OpenSSH:

```bash
sudo sshd -t
sudo systemctl reload ssh 2>/dev/null || sudo systemctl reload sshd
```

Close existing SSH tunnels and restart the agent. Existing authenticated sessions may continue using the restrictions that were active when they connected.

#### `connect failed: Connection refused`

SSH accepted the tunnel, but Redis is not reachable from the SSH server:

```bash
nc -vz 127.0.0.1 6379
docker compose ps redis
docker compose logs --tail=100 redis
```

When Redis runs in Docker and the SSH daemon runs on the host, publish Redis only on loopback:

```yaml
ports:
  - "127.0.0.1:6379:6379"
```

A container shown only as `6379/tcp` is not published to the host. The expected `docker ps` output contains `127.0.0.1:6379->6379/tcp`. Never publish it as `0.0.0.0:6379:6379`.

### Official references

- [OpenSSH `sshd(8)`](https://man.openbsd.org/sshd.8)
- [OpenSSH `sshd_config(5)`](https://man.openbsd.org/sshd_config)
- [Windows OpenSSH key management](https://learn.microsoft.com/windows-server/administration/openssh/openssh_keymanagement)
- [Windows `icacls`](https://learn.microsoft.com/windows-server/administration/windows-commands/icacls)
- [Docker port publishing](https://docs.docker.com/engine/network/port-publishing/)
- [Ubuntu `adduser(8)`](https://manpages.ubuntu.com/manpages/noble/man8/adduser.8.html)

## Python MVP agent

1. Create a restricted SSH user on the VPS and generate a dedicated key pair.
2. Copy the private key and a verified `known_hosts` file to the computer. Never use `known_hosts: null`.
3. Create `agent/config.json` from the example and set the local `machineId`.
4. Install dependencies with `py -m pip install -r agent/requirements.txt`.
5. Start it with `pyw agent/kill_anydesk_agent.pyw --config agent/config.json`.

The script has no shell or remote-command execution path. It accepts only an unexpired, version-1 `KILL_ANYDESK` envelope targeted to its own machine ID.

## n8n setup

The workflow exists in n8n as **Kill AnyDesk V2** and is intentionally unpublished. Before publishing, create the `Redis private VPS` credential in n8n pointing to the Redis endpoint reachable from the n8n container/host. Keep Redis bound to loopback or a private Docker network; do not publish port 6379.

Then attach that credential to **LPUSH na fila privada**, test each destination, and publish only after an agent has connected successfully.

## Workflow Git Sync

The n8n Data Table **Workflow Git Sync** holds one row per workflow destination. A workflow may have several rows, so it can be mirrored to more than one repository. Only repositories under `hugoalves-dev/*` are permitted.

Required mapping columns are:

- `workflowId`, `workflowName`, `repository`, `branch`, and `filePath` identify the destination.
- `enabled` controls whether the mapping participates in sync.
- `status`, the two last hashes, and the conflict fields are managed by the sync workflows.

The synchronizer is deliberately conflict-safe: if both GitHub and n8n changed since the last baseline, it changes neither side, marks `CONFLICT`, and sends an e-mail. Resolve it only through the **Workflow Sync — Resolve Conflict** form with `N8N_WINS` or `GITHUB_WINS`.

Before enabling the workers, create an n8n API key in **Settings → n8n API** and save it as a least-privilege `n8n API` credential in n8n. Do not put the key in a workflow export or repository.