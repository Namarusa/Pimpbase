package queries

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"syscall"
)

// NetPolicy says which servers API queries and flow web requests may
// reach. The zero policy allows every address, plain http, localhost and
// private networks included, which suits a self-hosted server talking to
// its own network. A shared server can turn on BlockPrivate so that app
// builders cannot reach the server's own machine or internal network.
type NetPolicy struct {
	BlockPrivate bool
	// Allow lists networks that stay reachable when BlockPrivate is on,
	// such as 10.1.0.0/16 or 192.168.1.20.
	Allow []netip.Prefix
}

// ParseNetPolicy reads the --allow-networks list: IP addresses and CIDR
// ranges separated by commas or spaces.
func ParseNetPolicy(blockPrivate bool, allow string) (NetPolicy, error) {
	p := NetPolicy{BlockPrivate: blockPrivate}
	for _, f := range strings.FieldsFunc(allow, func(r rune) bool { return r == ',' || r == ' ' }) {
		if pre, err := netip.ParsePrefix(f); err == nil {
			p.Allow = append(p.Allow, pre.Masked())
			continue
		}
		a, err := netip.ParseAddr(f)
		if err != nil {
			return p, fmt.Errorf("%q is not an IP address or range such as 10.1.0.0/16", f)
		}
		p.Allow = append(p.Allow, netip.PrefixFrom(a, a.BitLen()))
	}
	return p, nil
}

// Blocked reports why the policy refuses ip, or "" when it may be reached.
func (p NetPolicy) Blocked(ip netip.Addr) string {
	if !p.BlockPrivate {
		return ""
	}
	ip = ip.Unmap()
	for _, a := range p.Allow {
		if a.Contains(ip) {
			return ""
		}
	}
	switch {
	case ip.IsLoopback():
		return "this server's own address"
	case ip.IsPrivate(), ip.IsLinkLocalUnicast(), ip.IsLinkLocalMulticast(), ip.IsUnspecified(),
		ip.Is4() && (ip.As4()[0] == 100 && ip.As4()[1]&0xc0 == 64): // carrier-grade NAT
		return "a private network address"
	}
	return ""
}

// Transport is an HTTP transport that checks every address it connects
// to, after DNS and on each redirect, against the policy.
func (p NetPolicy) Transport() http.RoundTripper {
	t := http.DefaultTransport.(*http.Transport).Clone()
	if !p.BlockPrivate {
		return t
	}
	d := &net.Dialer{Control: func(network, address string, _ syscall.RawConn) error {
		ap, err := netip.ParseAddrPort(address)
		if err != nil {
			return err
		}
		if why := p.Blocked(ap.Addr()); why != "" {
			return fmt.Errorf("%s is %s, which this Pimpbase server blocks (--block-private-networks); an admin can allow it with --allow-networks", ap.Addr(), why)
		}
		return nil
	}}
	t.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		return d.DialContext(ctx, network, addr)
	}
	return t
}
