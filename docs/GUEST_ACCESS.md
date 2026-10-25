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

## How it works
- The **agent is the authority** (`agent/internal/guestauth`, `guests.json`). It refuses any ticket for a share it does not know or that was revoked or expired there, whatever the cloud says.
- The **backend** (`usbridge-entitlement-backend/src/shares.ts`) e-mails the invitation, checks that the signed-in account is the invited address, and issues a 10-minute Ed25519-signed ticket bound to a P-256 key the guest generated. It also enforces the Free limit and stores the audit trail. Agent-to-backend calls are signed with the agent's own Ed25519 key, registered on first use per hardware id.
- Every guest request carries the ticket plus a signature made with the guest's private key, so a stolen ticket is useless alone. Replays are refused.
- **Fail closed:** `agent/internal/api/guest.go` lists what each role may reach; every route not listed is owner-only.
- Streaming: the agent verifies a guest's WebRTC offer, then re-signs it with the master key and states the role in `X-Usbridge-Guest-Role`; rust-shine (`crates/webrtc-video/src/guest.rs`) only opens the data channels that role allows. WebTransport gets an agent-signed grant that carries the role.
- Identity is the e-mail of a USBridge account, not a Tailscale login, so nothing has to be done in the Tailscale admin console.

## Known limits
- Revoking, expiring or disconnecting a guest ends the running stream at once (rust-shine `POST /webrtc/kick`, needs a streamer from rust-shine 0.3.184 or newer; older streamers keep an already-running stream until it ends).
- The Share dialog is on the desktop grid card only; guest texts are English only for now.
- The full web GUI has no guest mode; guests use the dedicated join page. A native client as a guest is not built yet.
