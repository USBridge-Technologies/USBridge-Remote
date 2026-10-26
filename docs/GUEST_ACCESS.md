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
The invitation page has an **Open in the USBridge app** button (Operator and Technician roles), which opens `usbridge://join?share=<id>`. The app asks you to sign in with the invited address if you are not signed in, connects to the computer's tailnet address that comes with the guest's ticket, waits for the person at the computer if they asked to be consulted, and then streams over Moonlight like the owner does.

- The agent is still the authority: the guest's ticket and key sign every request (`USBClient.SetGuestSigner`), pairing goes through `POST /api/guest/moonlight-pair` (needs the input capability, so Operator and Technician), and the certificate that pairs is bound to the share. Revoking, expiring or ending the share removes it from the stream host's trust list (`guestauth.Manager.dropCertificates`).
- The `usbridge://` scheme is registered by the installers (Windows per-user registry key, `MimeType=x-scheme-handler/usbridge` in the Linux packages and the AppImage's desktop entry, `CFBundleURLTypes` on macOS, the Android manifest). A copy no installer registered asks, at start, to register itself (`internal/platform/urlscheme_*.go`, `url_scheme_prompt.go`).

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
- The full web GUI has no guest mode; guests use the dedicated join page.
- The desktop app reaches the computer over the network, not through the cloud relay the browser can use: on the same network, or over Tailscale when the guest's Tailscale (the app's own) has been given access to the computer's node. The address is dialled through the app's Tailscale. The computer must be on Tailscale to be shared: the owner's client opens the Tailscale sign-in when the agent is not yet on a tailnet, and the agent reports its tailnet address with the invitation (stored in `shares.host`, handed out only with a signed-in guest's ticket). Viewer guests use the browser only: the Moonlight stream host has no per-guest input limit yet, so the agent refuses to pair a Viewer.
- In the desktop app a guest's file uploads (disk images) are still signed with the master key only, so they are refused. On macOS the OS hands the link to the app as an Apple Event, which the app does not read yet, so the link opens the app but not the invitation there.
