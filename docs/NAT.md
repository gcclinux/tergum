# NAT Mode — Clients Behind Network Address Translation

## Overview

Tergum uses a bidirectional communication model:

- **Client → Server** (outbound): backups, restores, heartbeats, certificate bootstrap
- **Server → Client** (inbound): trigger remote backup, start/stop watcher, get status, push restore

When a client is on a different subnet or behind NAT, the server cannot connect back to the client directly. **NAT mode** solves this by reversing the server→client path: the client maintains a persistent outbound tunnel stream to the server, and the server dispatches commands over that stream.

No port forwarding, no firewall changes, no VPN required on the client side.

## Network Requirements

| Direction | Ports | Required? |
|-----------|-------|-----------|
| Client → Server | 7400 (command), 7401 (data), 7402 (bootstrap) | Yes — always needed |
| Server → Client | 7400 (command) | **No** — replaced by the tunnel in NAT mode |

**Pre-requisites:**

1. The client must be able to reach the server's IP on ports 7400, 7401, and 7402 (outbound TCP)
2. The server does NOT need to reach the client — all communication flows over client-initiated connections
3. Both machines must be able to resolve each other at the IP level (verify with `ping`)

### Verify connectivity (from the client)

```bash
ping <server-ip>
```

If ping works, NAT mode will work. The key requirement is that outbound TCP from the client to the server is not blocked.

## Setup

### New client setup (interactive)

On the client machine:

```bash
tergum setup
```

The wizard will ask:

1. **Node role** — select `client`
2. **Client IP** — select your local IP (used for identification only in NAT mode)
3. **Server address** — enter the server's IP address (e.g., `192.168.0.65`)
4. **Enable NAT mode?** — answer `Y`
5. **Configure TLS certificates?** — answer `Y` (fetches certs from server automatically)
6. Continue with passphrase, paths, and other options as normal

### Manual configuration

Edit `~/.config/tergum/tergum.toml` on the client:

```toml
[node]
role = "client"
hostname = "my-client-hostname"
nat_mode = true

[server]
address = "192.168.0.65"
command_port = 7400
data_port = 7401
bootstrap_port = 7402

[tls]
ca_cert = "/home/user/.config/tergum/certs/ca.crt"
cert = "/home/user/.config/tergum/certs/client.crt"
key = "/home/user/.config/tergum/certs/client.key"
```

The critical setting is `nat_mode = true` under `[node]`.

### Server configuration

No changes are needed on the server. The tunnel hub is always active and accepts tunnel connections from any authenticated client. NAT clients and direct-connect clients coexist seamlessly.

## Starting the Client

```bash
TERGUM_PASSPHRASE=yourpassphrase tergum client
```

On startup with `nat_mode = true`, the client will:

1. Connect to the server's command port (7400) and data port (7401)
2. Register with the server using a `tunnel://` address marker
3. Open a persistent bidirectional gRPC stream (the command tunnel)
4. Start the heartbeat loop (pings every 30 seconds)
5. Start the file watcher (if enabled)

Log output will confirm:

```
INF tergum client daemon started client_id=<your-cert-CN> server=192.168.0.65
INF NAT command tunnel started client_id=<your-cert-CN>
```

## How It Works

```
┌──────────────────────┐         ┌──────────────────────┐
│     CLIENT           │         │     SERVER           │
│  (192.168.1.155)     │         │  (192.168.0.65)      │
│                      │         │                      │
│  ┌────────────────┐  │  TCP    │  ┌────────────────┐  │
│  │ Data Upload    │──┼────────►│  │ DataService    │  │
│  │ (port 7401)    │  │         │  │ :7401          │  │
│  └────────────────┘  │         │  └────────────────┘  │
│                      │         │                      │
│  ┌────────────────┐  │  TCP    │  ┌────────────────┐  │
│  │ Command Tunnel │──┼────────►│  │ CommandService │  │
│  │ (bidi stream)  │◄─┼────────┤│  │ :7400          │  │
│  └────────────────┘  │         │  │ + TunnelHub    │  │
│                      │         │  └────────────────┘  │
│  ┌────────────────┐  │         │                      │
│  │ ClientCommand  │  │         │  When server needs   │
│  │ Server (local) │  │         │  to send a command:  │
│  │ :7400          │  │         │  1. Finds tunnel     │
│  └────────────────┘  │         │  2. Sends command    │
│         ▲            │         │  3. Waits for reply  │
│         │            │         │                      │
│  (handles commands   │         │                      │
│   from tunnel)       │         │                      │
└──────────────────────┘         └──────────────────────┘
```

**Data path** (backup/restore): Client opens a streaming gRPC connection to the server's DataService. This is always outbound from the client and works through NAT without changes.

**Command path** (trigger backup, start/stop watcher, get status): Instead of the server dialing back to the client, the server pushes a command over the existing tunnel stream. The client processes it locally and responds on the same stream.

**Automatic reconnect**: If the tunnel stream drops (network interruption, server restart), the client reconnects automatically every 5 seconds.

## Operations That Work Over NAT

| Operation | Path | Works in NAT mode? |
|-----------|------|:------------------:|
| Manual backup (`tergum backup`) | Client → Server | ✓ |
| File watcher (ongoing backup) | Client → Server | ✓ |
| Scheduled backup | Client → Server | ✓ |
| Restore (`tergum restore`) | Client → Server | ✓ |
| Server triggers backup (WebUI) | Server → Client (tunnel) | ✓ |
| Server starts/stops watcher (WebUI) | Server → Client (tunnel) | ✓ |
| Server queries client status (WebUI) | Server → Client (tunnel) | ✓ |
| Push restore (cross-client) | Server → Client (tunnel) | Partial* |
| Certificate bootstrap (`tergum setup`) | Client → Server | ✓ |

\* Push restore initiation works over the tunnel, but the actual file data currently requires a direct stream. For full cross-client restore support behind NAT, use `tergum restore` from the target client instead.

## Troubleshooting

### "no route to host" during setup

This means the client cannot reach the server. Verify:

```bash
# From the client
ping <server-ip>
nc -zv <server-ip> 7402   # test bootstrap port
nc -zv <server-ip> 7400   # test command port
nc -zv <server-ip> 7401   # test data port
```

If ping works but ports are blocked, check the server's firewall (not the client's — only outbound matters for the client).

### Tunnel not connecting

Check the client logs for:
- `tunnel session established` — success
- `tunnel session ended, reconnecting` — the stream dropped, will retry

Common causes:
- Server not running (`tergum server` must be started first)
- TLS certificate mismatch (re-run `tergum setup` to re-fetch certs)
- Firewall on the server blocking inbound on port 7400

### Server shows client as offline

The tunnel registration happens when the stream opens. If the client appears offline in the WebUI:
- Verify the client daemon is running (`tergum client`)
- Check that `nat_mode = true` is set in the client's config
- Look at the client's log output for tunnel errors

### Certificate bootstrap fails

The bootstrap service runs on port 7402. If the client can ping the server but can't fetch certs:

1. Make sure the server is running
2. Test connectivity: `nc -zv <server-ip> 7402`
3. If blocked, manually copy certificates:

```bash
# From the client machine
scp user@<server-ip>:~/.config/tergum/certs/ca.crt     ~/.config/tergum/certs/
scp user@<server-ip>:~/.config/tergum/certs/client.crt ~/.config/tergum/certs/
scp user@<server-ip>:~/.config/tergum/certs/client.key ~/.config/tergum/certs/
```

Then re-run setup and skip the cert question, or answer `N` to "Configure TLS certificates?".

## Example: Two-Subnet Setup

```
Server:  192.168.0.65 (subnet A)
Client:  192.168.1.155 (subnet B)
Router:  192.168.1.1 (connects both subnets)
```

**Server** — no changes needed, just run:
```bash
TERGUM_PASSPHRASE=mypass tergum server
```

**Client** — setup with NAT mode:
```bash
tergum setup
# Role: client
# Client IP: 192.168.1.155
# Server address: 192.168.0.65
# NAT mode: Y
# TLS: Y (fetches certs from server)
# Passphrase: mypass
# Paths: /home/user/Documents, etc.
```

Start:
```bash
TERGUM_PASSPHRASE=mypass tergum client
```

The client connects outbound to the server, backs up files, and receives commands via the tunnel. No port forwarding or firewall changes required on the client side.
