package tailscale

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// ErrHTTPSCertsDisabled: the tailnet hands out no certificate domains --
// "HTTPS Certificates" is off in its admin console (DNS page), or this is a
// control server without them (Headscale).
var ErrHTTPSCertsDisabled = errors.New("tailscale: HTTPS certificates are not enabled for this tailnet")

// CertPair returns a browser-trusted certificate for this node's own
// MagicDNS name (<node>.<tailnet>.ts.net), issued through the tailnet's
// control server -- no record in any zone of ours. tsnet keeps the pair in
// its state dir and renews it itself, so calling this on every
// device-cert tick is cheap once the first issuance (a few seconds) is
// done.
func (s *Service) CertPair(ctx context.Context) (domain string, certPEM, keyPEM []byte, err error) {
	lc, err := s.localClient()
	if err != nil {
		return "", nil, nil, err
	}
	st, err := lc.StatusWithoutPeers(ctx)
	if err != nil {
		return "", nil, nil, err
	}
	if st.BackendState != "Running" || st.Self == nil {
		return "", nil, nil, fmt.Errorf("tailscale: not running (%s)", st.BackendState)
	}
	domain = strings.TrimSuffix(st.Self.DNSName, ".")
	if !certDomainListed(st.CertDomains, domain) {
		return "", nil, nil, ErrHTTPSCertsDisabled
	}
	certPEM, keyPEM, err = lc.CertPair(ctx, domain)
	if err != nil {
		return "", nil, nil, err
	}
	return domain, certPEM, keyPEM, nil
}

func certDomainListed(domains []string, domain string) bool {
	if domain == "" {
		return false
	}
	for _, d := range domains {
		if strings.EqualFold(strings.TrimSuffix(d, "."), domain) {
			return true
		}
	}
	return false
}
