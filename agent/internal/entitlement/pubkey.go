// Package entitlement talks to the usbridge-entitlement backend
// (github.com/itsme228/usbridge-entitlement-backend, a stateless Cloudflare
// Worker — see that repo's README for the full protocol) to let a paying
// Patreon supporter switch the agent's active streamhost.Backend from the
// free, open-source Sunshine build to the proprietary RustShine one.
//
// Security model mirrors internal/update almost exactly (see that
// package's own doc comment): the backend signs entitlement tokens with an
// Ed25519 private key that exists only as that Worker's own
// wrangler-secret env var; this package verifies every token against the
// public key below before trusting a single field of it. A hand-edited
// config.yaml claiming entitlement can't produce a token this package
// accepts — forging one requires the private key. This is a DELIBERATELY
// SEPARATE keypair from both the client/agent update-signing keys and any
// future rust-shine-release-manifest key, so compromising one can't be
// used to forge the others (see rust-shine/docs/RELEASE_SIGNING.md's docs
// on the same principle for that third key).
//
// This package never gates access to RustShine's *source* (it's a private
// repo, not part of this build) or to a binary someone already has on
// disk — only to the official, auto-fetched, auto-updating distribution
// channel this package itself drives. See the backend's own README "What's
// deliberately NOT here yet" section for that tradeoff stated plainly.
package entitlement

import (
	"crypto/ed25519"
	"encoding/base64"
)

// backendBaseURL is the usbridge-entitlement Worker's deployed URL. A var,
// not a const, purely so tests can point it at a local httptest.Server for
// the duration of a single test (see app_test.go) and restore it after --
// every production caller only ever sees the real URL.
var backendBaseURL = "https://usbridge-entitlement.fatkulinamir80.workers.dev"

// publicKeyB64 is the Ed25519 public half of the entitlement-signing
// keypair. The private half lives only as that Worker's own
// ENTITLEMENT_ED25519_PRIVATE_KEY secret (never in any repo — see
// usbridge-entitlement-backend's README "Generating the entitlement
// keypair" for provenance). Committing the public key here is safe: it
// lets this binary verify entitlement tokens but reveals nothing that
// would let anyone forge one.
const publicKeyB64 = "BOr0FAQyGQhmg6CZdgcRDekKrP2A++60WKvhW52tV58="

var publicKey = mustDecodePublicKey(publicKeyB64)

// TestSetBackendBaseURL points every entitlement backend call at url
// (typically a local httptest.Server) and returns the previous value so
// the caller can restore it -- exported only so internal/app's tests (a
// different package) can exercise recheckEntitlement against a fake
// backend instead of the real deployed one. Not meant to be called from
// production code; nothing in this repo does.
func TestSetBackendBaseURL(url string) string {
	prev := backendBaseURL
	backendBaseURL = url
	return prev
}

func mustDecodePublicKey(b64 string) ed25519.PublicKey {
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil || len(raw) != ed25519.PublicKeySize {
		// A corrupt embedded key is a build-time defect, not a runtime
		// condition callers can meaningfully recover from.
		panic("entitlement: invalid embedded public key")
	}
	return ed25519.PublicKey(raw)
}
