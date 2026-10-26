# Guest access

Share a computer with other people the way TeamViewer or RustDesk do: by e-mail, with a role and an end date, revocable at any time. The master key is never given to a guest.

## For the owner
1. In the client, open the contact (**Share** icon on its card) and enter the guest's e-mail.
2. Pick a role, how long the access lasts, and whether you want to be asked each time they connect (switch that off for unattended computers).
3. The guest gets an e-mail and opens the link. The people with access are listed in the same dialog, where you can disconnect or revoke them, and answer a request to connect.

Roles: **Viewer** watches the screen. **Operator** also uses mouse, keyboard and clipboard. **Technician** also gets USB devices, disks, displays and video settings. Settings, service control, key management and pairing stay with the owner.

Plans: Free allows one live guest; Pro and Enterprise have no limit and keep an audit trail. A guest reaches a Free machine through the cloud signaling relay too: the relay stays open for that machine while it has a live share, and an offer passes the relay only with a valid ticket for a live share of that very device. Owners' own relay access still needs Pro.

## For the guest
Open the link from the e-mail (`/join.html#/join/<id>` on the web client) and sign in with the address that received it (Google, GitHub, Microsoft or an e-mail code). If the owner asked to be consulted, the page waits until the person at the computer allows it.

### In the desktop app
The invitation page has an **Open in the USBridge app** button, which opens `usbridge://join?share=<id>`. The app asks you to sign in with the invited address if you are not signed in, joins the guest network with the one-use key in the guest's ticket and connects to the computer's address on it, waits for the person at the computer if they asked to be consulted, and then streams over Moonlight like the owner does.

- The agent is still the authority: the guest's ticket and key sign every request (`USBClient.SetGuestSigner`), pairing goes through `POST /api/guest/moonlight-pair` (any role; the pairing is limited to the role afterwards), and the certificate that pairs is bound to the share. Revoking, expiring or ending the share removes it from the stream host's trust list (`guestauth.Manager.dropCertificates`).
- Keyboard, mouse and gamepad: the app starts them on the agent (`POST /api/device/start`) before input works. Every kind of device needs its own capability (`deviceKindCap`): input for keyboard, mouse and gamepad, disks for drives and MTP, USB for the rest. The request only replaces the entries of the kinds the role holds, never the owner's other devices (`Server.mergeGuestDevices`). Disk image uploads (`/api/iso/upload`) are signed with the guest key and need the disks capability; the upload endpoint itself lives on the device server, which must verify the guest signature.
- The `usbridge://` scheme is registered by the installers (Windows per-user registry key, `MimeType=x-scheme-handler/usbridge` in the Linux packages and the AppImage's desktop entry, `CFBundleURLTypes` on macOS, the Android manifest). A copy no installer registered asks, at start, to register itself (`internal/platform/urlscheme_*.go`, `url_scheme_prompt.go`).

## How it works
- The **agent is the authority** (`agent/internal/guestauth`, `guests.json`). It refuses any ticket for a share it does not know or that was revoked or expired there, whatever the cloud says.
- The **backend** (`usbridge-entitlement-backend/src/shares.ts`) e-mails the invitation, checks that the signed-in account is the invited address, and issues a 10-minute Ed25519-signed ticket bound to a P-256 key the guest generated. It also enforces the Free limit and stores the audit trail. Agent-to-backend calls are signed with the agent's own Ed25519 key, registered on first use per hardware id.
- Every guest request carries the ticket plus a signature made with the guest's private key, so a stolen ticket is useless alone. Replays are refused.
- **Fail closed:** `agent/internal/api/guest.go` lists what each role may reach; every route not listed is owner-only.
- Streaming: the agent verifies a guest's WebRTC offer, then re-signs it with the master key and states the role in `X-Usbridge-Guest-Role`; rust-shine (`crates/webrtc-video/src/guest.rs`) only opens the data channels that role allows. WebTransport gets an agent-signed grant that carries the role.
- Identity is the e-mail of a USBridge account, not a Tailscale login, so nobody has to touch the Tailscale admin console, create keys or own a Tailscale account.

## Hardware KVM
The same sharing works on the KVM (`../usbridge`, `guestauth/` and `web/guest.go`): the same invitation, roles, consent, tickets and signatures, with the KVM as the authority (`/etc/usbridge/guests/guests.json`). Routes are gated by role like the desktop agent's; a guest's `/api/device/start` needs the capability of every kind of device it names and is turned into a merge, and a disk must be a bare uploaded file name (never a path, URL or NBD server). Disk uploads (`/api/iso/upload`) are verified against the guest key and need the disks capability. WebRTC offers are re-signed for rust-shine with the role; Moonlight pairing is bound to the share and removed with it. A licensed KVM counts as Pro (the backend marks it when it issues the device license).

Parity with the desktop agent: the guest network (a second kernel-mode `tailscaled` on the `tsguest0` interface, closed by an nftables table to the API and stream ports only, up only while a share is live), the cloud signaling relay for browsers, the Pro tier for a licensed KVM, and the consent prompt on the KVM's display (Allow / Deny, or answered from the owner's client). The stream host is rust-shine, so a Viewer is not paired with Moonlight there, as on the agent. Run on a Radxa ZERO 3W board (firmware 1.2.126): invitation, guest network, video, mouse, audio and the device pages work.

### What a role reaches on the KVM

Every route not listed in `routeCaps` (`web/guest.go`) is owner-only and answers a guest `403 {"error":"not_permitted"}`; a client must not poll such a route. A role holds the rights of the roles above it in this list:

| Role | Rights | Routes |
|---|---|---|
| Viewer | view, audio | device info and status, video info and devices, screen, WebTransport grant, Moonlight pairing, audio info, devices, **start and stop**, polling of script / PCPanel / benchmark **status** |
| Operator | + input, clipboard | keyboard, mouse (also tablet and touchscreen), gamepad (REST and WebSocket), PCPanel buttons; `/api/device/start` for keyboard, mouse and gamepad |
| Technician | + USB, disks, display, video settings | USB passthrough, `/api/device/start` and `stop` for USB network and sound, drives and MTP (a drive is an uploaded file name only), disk space, uploads and storage status, **backup snapshots** (versions, snapshot list, file), video device / FEC / source, script **list, read and log** |

Never given to a guest, whatever the role:

- Running, writing, stopping or deleting **scripts** (`/api/scripts/run|write|stop|delete`): that is code execution on the KVM itself, not on the target computer.
- Benchmark control (`/api/bench/*` other than status).
- Settings, power, Wi-Fi and network configuration, service control, key management, Tailscale registration, auth sync.
- Deleting disk images (`/api/iso/delete`), sharing (`/api/guests*`: inviting, revoking, deciding on a request).
- Any path, URL or NBD server as a drive source.

A new route is owner-only until it is added to `routeCaps` on purpose.

### The guest network on the board

- The board runs a second `tailscaled` on `tsguest0`; the owner's own `tailscaled` installs an iptables rule that drops every `100.64.0.0/10` source not arriving on `tailscale0`, which would swallow all guests. The agent therefore keeps `iptables -I INPUT 1 -i tsguest0 -j ACCEPT` first in `INPUT` while the node is up (re-checked on every watchdog tick, removed with the node). The nftables table `usbridge_guest` still closes the interface to the API and the stream ports. The kernel needs `CONFIG_NF_TABLES`, `NF_TABLES_INET` and `NFT_CT` (`rz3w-extra-modules.cfg`).
- Headscale names a node `<name>-N` when the hostname is taken by an earlier registration that was not removed, so admission accepts `g-<share>` and `g-<share>-<digits>` of a live share (`guestNodeLabelMatches`). Stale registrations pile up in Headscale until the backend removes them (ephemeral nodes or deleting the same-named node before issuing a key is not done yet).

## Known limits
- Revoking, expiring or disconnecting a guest ends the running stream at once (rust-shine `POST /webrtc/kick`, needs a streamer from rust-shine 0.3.184 or newer; older streamers keep an already-running stream until it ends).
- In the browser the transport order is WebTransport first, WebRTC when it fails (a guest behind the relay or a NAT has no direct route for WebTransport, so it ends on WebRTC, which punches through with ICE); a transport picked in the header is not overridden. Direct WebTransport behind a NAT (UPnP / NAT-PMP port mapping on the agent) is not done.
- The Share dialog is on the desktop grid card only; guest texts are English only for now.
- The full web GUI has no guest mode; guests use the dedicated join page.
- **Guest network.** The desktop app does not use the cloud relay the browser uses; it reaches the computer over a network. By default that is the **USBridge guest network**: a Headscale (self-hosted Tailscale control server, `hs.usbridge.io`) run by USBridge on `ota.usbridge.io` (the control server answers `/key`, `/ts2021`, `/api/v1/` there through the existing Traefik; DERP relays on `:8444`, STUN on `3478/udp`). Nobody needs a Tailscale account or the Tailscale dashboard: the agent brings up a second tsnet node (`app/guest_tailnet.go`, its own state dir and name `h-…`) using a one-use key from `POST /v1/device/tailnet-key`, reports its address with `POST /v1/device/shares/{id}/host`, and the guest's ticket carries a one-use key for their own node (`g-<share>`, `tailnet` block of the ticket). The guest's app swaps that node in for its own Tailscale for the duration of the session (`useGuestNetwork`), so the user's personal Tailscale is never touched. Only nodes whose name belongs to a live share get through the agent's proxy and HTTP listener (`WhoIs` check, `StreamProxy.SetAdmit`); the guest node is stopped when no share is live. The control server URL travels with the keys (`control_url`), so another Headscale can be used later. A switch in the agent's Tailscale dialog (`guest_own_tailscale`) makes new shares use the owner's own Tailscale instead (the guest's Tailscale must then see the computer, e.g. a node shared by hand in the Tailscale admin). Every role can use the app: after a guest's Moonlight client pairs, the agent limits it on the stream host to the role (`guestMoonlightPerm`, `/api/clients/update` of the Sunshine fork: Viewer view only, Operator adds input and clipboard, Technician adds file transfer, nobody may run host commands). A client that cannot be limited is unpaired. A stream host without per-client permissions (rust-shine, Punktfunk) cannot hold a Viewer, so the agent refuses to pair one there; Viewer uses the browser.
- **Ops (Headscale).** On the Mender box (`ota.usbridge.io`), files in `/opt/headscale`: `docker-compose.yml` (Headscale 0.29.6 + our DERP relay `usbridge-derper`), `config/config.yaml`, `config/policy.hujson` (the ACL: `tag:guest` may reach `tag:host`, nothing else), `config/derp.yaml` (the DERP map: our relay only), `derp/rate.json`, `data/` (SQLite, keys: **back this up**). Backend secrets: `HEADSCALE_URL` (`https://ota.usbridge.io`), `HEADSCALE_API_KEY` (`docker exec headscale headscale apikeys create`), `HEADSCALE_USER` (numeric id of the `usbridge` user); without them guests get no network key and invitations fail with "could not set up the guest network". Keys are one-use, ephemeral, tagged and valid 10 minutes; ephemeral nodes disappear 30 minutes after going offline.
- **Relay limits.** The box has about 20 TB of traffic in total, so the DERP relay only lets in nodes that are registered with Headscale (`-verify-client-url`, fail closed) and limits every client to 1 MiB/s (burst 2 MiB) with `derp/rate.json`; change the file and run `docker kill -s HUP usbridge-derper`. Direct (NAT-traversed) paths are not relayed and not limited. The relay's TLS certificate is the box's Let's Encrypt one, copied by `/etc/letsencrypt/renewal-hooks/deploy/usbridge-derper.sh` on renewal.
- **Linux agent and the Grant buttons.** The agent's Grant buttons (USB passthrough, virtual display, firewall) run `pkexec`, which finds the desktop's authentication dialog through the process's login session. Start the agent from the desktop session (autostart, or a user service: `systemd-run --user --unit=usbridge-agent ...`), not from an ssh session; from ssh the grants fail with "/dev/tty: no such device or address".
- In the desktop app a guest's file uploads (disk images) are still signed with the master key only, so they are refused. On macOS the OS hands the link to the app as an Apple Event, which the app does not read yet, so the link opens the app but not the invitation there.
