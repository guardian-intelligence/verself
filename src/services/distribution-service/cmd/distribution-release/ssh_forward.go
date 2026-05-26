package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

const (
	sshConnectTimeout   = 5 * time.Second
	sshHandshakeTimeout = 5 * time.Second
	sshAgentTimeout     = 2 * time.Second
)

type nomadSSHForward struct {
	ListenAddr string
	listener   net.Listener
	client     *ssh.Client
	agentConn  net.Conn
	cancel     context.CancelFunc
	once       sync.Once
}

func openNomadSSHForward(ctx context.Context, access siteSSHAccess) (*nomadSSHForward, error) {
	if len(access.Targets) == 0 {
		return nil, errors.New("no SSH targets available for Nomad forwarding")
	}
	auth, agentConn, err := sshAuthMethods()
	if err != nil {
		return nil, err
	}
	var errs []error
	for _, target := range access.Targets {
		client, err := dialSSH(ctx, target, auth)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		forward, err := startForward(client, agentConn, nomadHTTPPort)
		if err != nil {
			_ = client.Close()
			errs = append(errs, err)
			continue
		}
		return forward, nil
	}
	if agentConn != nil {
		_ = agentConn.Close()
	}
	return nil, fmt.Errorf("ssh nomad forward failed: %w", errors.Join(errs...))
}

func sshAuthMethods() ([]ssh.AuthMethod, net.Conn, error) {
	var methods []ssh.AuthMethod
	var agentConn net.Conn
	if sock := os.Getenv("SSH_AUTH_SOCK"); sock != "" {
		dialer := net.Dialer{Timeout: sshAgentTimeout}
		conn, err := dialer.Dial("unix", sock)
		if err == nil {
			_ = conn.SetDeadline(time.Now().Add(sshAgentTimeout))
			signers, signErr := agent.NewClient(conn).Signers()
			_ = conn.SetDeadline(time.Time{})
			if signErr == nil && len(signers) > 0 {
				methods = append(methods, ssh.PublicKeys(signers...))
				agentConn = conn
			} else {
				_ = conn.Close()
			}
		}
	}
	if signer, err := defaultSSHKeySigner(); err == nil {
		methods = append(methods, ssh.PublicKeys(signer))
	}
	if len(methods) == 0 {
		if agentConn != nil {
			_ = agentConn.Close()
		}
		return nil, nil, errors.New("no SSH signer available; run ssh-add or install an unencrypted default key")
	}
	return methods, agentConn, nil
}

func defaultSSHKeySigner() (ssh.Signer, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	for _, name := range []string{"id_ed25519", "id_ecdsa", "id_rsa"} {
		path := filepath.Join(home, ".ssh", name)
		body, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		signer, err := ssh.ParsePrivateKey(body)
		if err == nil {
			return signer, nil
		}
	}
	return nil, errors.New("no default SSH private key found")
}

func dialSSH(ctx context.Context, target sshTarget, auth []ssh.AuthMethod) (*ssh.Client, error) {
	if target.Port == 0 {
		target.Port = 22
	}
	cfg := &ssh.ClientConfig{
		User:            target.User,
		Auth:            auth,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         sshConnectTimeout,
	}
	addr := net.JoinHostPort(target.Host, strconv.Itoa(target.Port))
	dialer := net.Dialer{Timeout: sshConnectTimeout}
	tcp, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("%s ssh tcp dial %s: %w", target.Label, addr, err)
	}
	_ = tcp.SetDeadline(time.Now().Add(sshHandshakeTimeout))
	conn, chans, reqs, err := ssh.NewClientConn(tcp, addr, cfg)
	if err != nil {
		_ = tcp.Close()
		return nil, fmt.Errorf("%s ssh handshake %s: %w", target.Label, addr, err)
	}
	_ = tcp.SetDeadline(time.Time{})
	return ssh.NewClient(conn, chans, reqs), nil
}

func startForward(client *ssh.Client, agentConn net.Conn, remotePort int) (*nomadSSHForward, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("nomad local listener: %w", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	forward := &nomadSSHForward{
		ListenAddr: listener.Addr().String(),
		listener:   listener,
		client:     client,
		agentConn:  agentConn,
		cancel:     cancel,
	}
	go forward.accept(ctx, remotePort)
	return forward, nil
}

func (f *nomadSSHForward) accept(ctx context.Context, remotePort int) {
	for {
		local, err := f.listener.Accept()
		if err != nil {
			return
		}
		select {
		case <-ctx.Done():
			_ = local.Close()
			return
		default:
		}
		go f.proxy(local, remotePort)
	}
}

func (f *nomadSSHForward) proxy(local net.Conn, remotePort int) {
	defer func() { _ = local.Close() }()
	remote, err := f.client.Dial("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(remotePort)))
	if err != nil {
		return
	}
	defer func() { _ = remote.Close() }()
	errc := make(chan error, 2)
	go func() {
		_, err := io.Copy(remote, local)
		errc <- err
	}()
	go func() {
		_, err := io.Copy(local, remote)
		errc <- err
	}()
	<-errc
}

func (f *nomadSSHForward) Close() error {
	if f == nil {
		return nil
	}
	var err error
	f.once.Do(func() {
		f.cancel()
		err = errors.Join(f.listener.Close(), f.client.Close())
		if f.agentConn != nil {
			err = errors.Join(err, f.agentConn.Close())
		}
	})
	return err
}
