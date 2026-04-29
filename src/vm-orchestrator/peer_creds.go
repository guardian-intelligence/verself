package vmorchestrator

import (
	"context"
	"fmt"
	"net"

	"golang.org/x/sys/unix"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

// PeerCredsTransportCredentials is a server-only TransportCredentials for
// gRPC over a Unix domain socket. The handshake reads SO_PEERCRED on the
// accepted connection and stuffs the result into AuthInfo so per-RPC
// handlers can authorize callers by uid/gid/pid via peer.FromContext.
//
// SO_PEERCRED is a Linux-specific kernel-attested credential set: the
// reader trusts that the kernel populated the struct from the peer
// process's runtime identity at connect time. There is no application-layer
// negotiation; clients can dial with insecure credentials and the server
// still gets the truthful uid.
type PeerCredsTransportCredentials struct{}

// NewPeerCredsTransportCredentials returns the server-only transport
// credentials suitable for grpc.NewServer(grpc.Creds(...)) on a Unix socket.
func NewPeerCredsTransportCredentials() credentials.TransportCredentials {
	return PeerCredsTransportCredentials{}
}

// PeerIdentity is the AuthInfo carried on contexts whose stream was accepted
// through PeerCredsTransportCredentials.
type PeerIdentity struct {
	UID int
	GID int
	PID int
}

// AuthType implements credentials.AuthInfo.
func (PeerIdentity) AuthType() string { return "unix-peer-creds" }

// ClientHandshake is a server-only credentials implementation; clients
// dialing this server use plain insecure credentials over the socket. The
// kernel attaches SO_PEERCRED regardless of client-side negotiation.
func (PeerCredsTransportCredentials) ClientHandshake(_ context.Context, _ string, conn net.Conn) (net.Conn, credentials.AuthInfo, error) {
	return conn, nil, fmt.Errorf("PeerCredsTransportCredentials is server-only")
}

// ServerHandshake reads SO_PEERCRED on the accepted Unix conn and returns a
// PeerIdentity AuthInfo. Non-Unix conns are rejected so a misconfigured TCP
// listener can't silently bypass the peer-cred gate.
func (PeerCredsTransportCredentials) ServerHandshake(rawConn net.Conn) (net.Conn, credentials.AuthInfo, error) {
	unixConn, ok := rawConn.(*net.UnixConn)
	if !ok {
		return nil, nil, fmt.Errorf("peer-creds transport requires a Unix conn, got %T", rawConn)
	}
	syscallConn, err := unixConn.SyscallConn()
	if err != nil {
		return nil, nil, fmt.Errorf("syscall conn for peer creds: %w", err)
	}
	var ucred *unix.Ucred
	var ucredErr error
	controlErr := syscallConn.Control(func(fd uintptr) {
		ucred, ucredErr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
	})
	if controlErr != nil {
		return nil, nil, fmt.Errorf("control unix conn for SO_PEERCRED: %w", controlErr)
	}
	if ucredErr != nil {
		return nil, nil, fmt.Errorf("getsockopt SO_PEERCRED: %w", ucredErr)
	}
	if ucred == nil {
		return nil, nil, fmt.Errorf("getsockopt SO_PEERCRED returned nil ucred")
	}
	return rawConn, PeerIdentity{
		UID: int(ucred.Uid),
		GID: int(ucred.Gid),
		PID: int(ucred.Pid),
	}, nil
}

// Info returns the same SecurityProtocol string as grpc/credentials/insecure
// so client-side dials with insecure creds successfully negotiate against
// this server. The trust model is delegated to the kernel, not TLS.
func (PeerCredsTransportCredentials) Info() credentials.ProtocolInfo {
	return credentials.ProtocolInfo{SecurityProtocol: "insecure"}
}

// Clone implements credentials.TransportCredentials.
func (PeerCredsTransportCredentials) Clone() credentials.TransportCredentials {
	return PeerCredsTransportCredentials{}
}

// OverrideServerName implements credentials.TransportCredentials.
func (PeerCredsTransportCredentials) OverrideServerName(string) error { return nil }

// PeerIdentityFromContext returns the SO_PEERCRED-derived identity attached
// to ctx during ServerHandshake. The bool is false if ctx was not produced
// by a stream accepted through PeerCredsTransportCredentials, which is a
// configuration bug rather than something handlers should attempt to
// recover from gracefully.
func PeerIdentityFromContext(ctx context.Context) (PeerIdentity, bool) {
	p, ok := peer.FromContext(ctx)
	if !ok || p == nil {
		return PeerIdentity{}, false
	}
	identity, ok := p.AuthInfo.(PeerIdentity)
	return identity, ok
}

// requireRootPeerCreds rejects an RPC unless the calling Unix peer is uid=0.
// This is the authorization boundary for SeedImage: only deploy-time tooling
// running as root may seed images, while sandbox-rental (a non-root system
// user in the vm-clients group) can call every other RPC unimpeded.
func requireRootPeerCreds(ctx context.Context) error {
	identity, ok := PeerIdentityFromContext(ctx)
	if !ok {
		return status.Error(codes.PermissionDenied, "peer credentials are not available")
	}
	if identity.UID != 0 {
		return status.Errorf(codes.PermissionDenied, "uid=0 required, got uid=%d", identity.UID)
	}
	return nil
}
