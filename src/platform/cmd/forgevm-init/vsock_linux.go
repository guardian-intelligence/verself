package main

import (
	"fmt"
	"io"
	"os"

	"github.com/forge-metal/forge-metal/internal/vmproto"
	"golang.org/x/sys/unix"
)

type vsockListener struct {
	fd int
}

type vsockConn struct {
	file *os.File
}

func listenVsockListener() (*vsockListener, error) {
	fd, err := unix.Socket(unix.AF_VSOCK, unix.SOCK_STREAM, 0)
	if err != nil {
		return nil, fmt.Errorf("socket vsock: %w", err)
	}
	if err := unix.Bind(fd, &unix.SockaddrVM{
		CID:  unix.VMADDR_CID_ANY,
		Port: vmproto.GuestPort,
	}); err != nil {
		unix.Close(fd)
		return nil, fmt.Errorf("bind vsock: %w", err)
	}
	if err := unix.Listen(fd, 1); err != nil {
		unix.Close(fd)
		return nil, fmt.Errorf("listen vsock: %w", err)
	}
	return &vsockListener{fd: fd}, nil
}

func (l *vsockListener) Accept() (io.ReadWriteCloser, error) {
	fd, _, err := unix.Accept(l.fd)
	if err != nil {
		return nil, fmt.Errorf("accept vsock: %w", err)
	}
	return &vsockConn{file: os.NewFile(uintptr(fd), "vsock-conn")}, nil
}

func (l *vsockListener) Close() error {
	return unix.Close(l.fd)
}

func (c *vsockConn) Read(p []byte) (int, error) {
	return c.file.Read(p)
}

func (c *vsockConn) Write(p []byte) (int, error) {
	return c.file.Write(p)
}

func (c *vsockConn) Close() error {
	return c.file.Close()
}
