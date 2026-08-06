package update

import (
	"crypto/ed25519"
	"encoding/base64"
)

// appName identifies this binary in the signed update manifest — must match
// the "app" field CI writes via scripts/sign_update_manifest.go.
const appName = "client"

// repoOwner/repoName locate the GitHub release that publishes
// manifest-client.json/.sig alongside the platform build artifacts.
const (
	repoOwner = "USBridge-Technologies"
	repoName  = "USBridge-Remote"
)

// publicKeyB64 is the Ed25519 public half of the client's update-signing
// keypair. The private half lives only in the CLIENT_UPDATE_ED25519_PRIVATE_KEY
// GitHub Actions secret (see ~/usbridge-update-keys/README.md at keypair
// generation time — that directory is intentionally outside this repo).
// Committing the public key here is safe: it lets this binary verify update
// manifests but reveals nothing that would let anyone forge one.
const publicKeyB64 = "e+/PFeKeURNTtH2PFyRFk1ODY38CPdQoF0gXPLIECIQ="

var publicKey = mustDecodePublicKey(publicKeyB64)

func mustDecodePublicKey(b64 string) ed25519.PublicKey {
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil || len(raw) != ed25519.PublicKeySize {
		// A corrupt embedded key is a build-time defect, not a runtime
		// condition callers can meaningfully recover from.
		panic("update: invalid embedded public key")
	}
	return ed25519.PublicKey(raw)
}
