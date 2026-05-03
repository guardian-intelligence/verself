//go:build !linux

package vmorchestrator

import (
	"context"
	"fmt"
	"net"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

type PeerCredsTransportCredentials struct{}

func NewPeerCredsTransportCredentials() credentials.TransportCredentials {
	return PeerCredsTransportCredentials{}
}

type PeerIdentity struct {
	UID int
	GID int
	PID int
}

func (PeerIdentity) AuthType() string { return "unix-peer-creds" }

func (PeerCredsTransportCredentials) ClientHandshake(_ context.Context, _ string, conn net.Conn) (net.Conn, credentials.AuthInfo, error) {
	return conn, nil, fmt.Errorf("PeerCredsTransportCredentials is server-only")
}

func (PeerCredsTransportCredentials) ServerHandshake(rawConn net.Conn) (net.Conn, credentials.AuthInfo, error) {
	return rawConn, nil, fmt.Errorf("SO_PEERCRED peer credentials require Linux")
}

func (PeerCredsTransportCredentials) Info() credentials.ProtocolInfo {
	return credentials.ProtocolInfo{SecurityProtocol: "insecure"}
}

func (PeerCredsTransportCredentials) Clone() credentials.TransportCredentials {
	return PeerCredsTransportCredentials{}
}

func (PeerCredsTransportCredentials) OverrideServerName(string) error { return nil }

func PeerIdentityFromContext(ctx context.Context) (PeerIdentity, bool) {
	p, ok := peer.FromContext(ctx)
	if !ok || p == nil {
		return PeerIdentity{}, false
	}
	identity, ok := p.AuthInfo.(PeerIdentity)
	return identity, ok
}

func requireRootPeerCreds(context.Context) error {
	return status.Error(codes.PermissionDenied, "Linux SO_PEERCRED peer credentials are not available")
}
