package tigerbeetle

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"strconv"
	"strings"

	tb "github.com/tigerbeetle/tigerbeetle-go"

	opruntime "github.com/verself/operator-runtime/runtime"
)

const (
	DefaultRemotePath = "/opt/verself/profile/bin/tigerbeetle"
	DefaultRunAsUser  = "tigerbeetle"
	DefaultClusterID  = "0"
	DefaultAddresses  = "127.0.0.1:3320"
)

const maxQueryLimit = 8189

type Config struct {
	RemotePath string
	RunAsUser  string
	ClusterID  string
	Addresses  string
}

type Client struct {
	Native   tb.Client
	forwards []*opruntime.Forward
}

func (c Config) normalizedClient() Config {
	if c.ClusterID == "" {
		c.ClusterID = DefaultClusterID
	}
	if c.Addresses == "" {
		c.Addresses = DefaultAddresses
	}
	return c
}

func (c Config) normalizedShell() (Config, error) {
	c = c.normalizedClient()
	if c.RemotePath == "" {
		c.RemotePath = DefaultRemotePath
	}
	if c.RunAsUser == "" {
		c.RunAsUser = DefaultRunAsUser
	}
	if !safeUnixUser(c.RunAsUser) {
		return Config{}, fmt.Errorf("tigerbeetle: invalid run-as user %q", c.RunAsUser)
	}
	if !strings.HasPrefix(c.RemotePath, "/") {
		return Config{}, fmt.Errorf("tigerbeetle: remote path must be absolute: %q", c.RemotePath)
	}
	return c, nil
}

func OpenClient(ctx context.Context, rt *opruntime.Runtime, cfg Config) (*Client, error) {
	if rt == nil || rt.SSH == nil {
		return nil, errors.New("tigerbeetle: operator runtime with SSH is required")
	}
	cfg = cfg.normalizedClient()
	clusterID, err := strconv.ParseUint(cfg.ClusterID, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("tigerbeetle: cluster ID must be a uint64: %w", err)
	}
	remoteAddresses, err := normalizeAddresses(cfg.Addresses)
	if err != nil {
		return nil, err
	}
	client := &Client{}
	localAddresses := make([]string, 0, len(remoteAddresses))
	for _, remoteAddress := range remoteAddresses {
		forward, err := rt.SSH.Forward(ctx, "tigerbeetle", remoteAddress)
		if err != nil {
			_ = client.Close()
			return nil, fmt.Errorf("tigerbeetle: forward %s: %w", remoteAddress, err)
		}
		client.forwards = append(client.forwards, forward)
		localAddresses = append(localAddresses, forward.ListenAddr)
	}
	native, err := tb.NewClient(tb.ToUint128(clusterID), localAddresses)
	if err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("tigerbeetle: create native client: %w", err)
	}
	client.Native = native
	return client, nil
}

func (c *Client) Close() error {
	if c == nil {
		return nil
	}
	if c.Native != nil {
		c.Native.Close()
		c.Native = nil
	}
	var err error
	for _, forward := range c.forwards {
		err = errors.Join(err, forward.Close())
	}
	c.forwards = nil
	return err
}

func (c *Client) LookupAccounts(ids []tb.Uint128) ([]tb.Account, error) {
	if c == nil || c.Native == nil {
		return nil, errors.New("tigerbeetle: native client is required")
	}
	return c.Native.LookupAccounts(ids)
}

func (c *Client) QueryAccounts(filter tb.QueryFilter) ([]tb.Account, error) {
	if c == nil || c.Native == nil {
		return nil, errors.New("tigerbeetle: native client is required")
	}
	if filter.Limit == 0 {
		return nil, errors.New("tigerbeetle: query account limit is required")
	}
	if filter.Limit > maxQueryLimit {
		return nil, fmt.Errorf("tigerbeetle: query account limit exceeds %d", maxQueryLimit)
	}
	return c.Native.QueryAccounts(filter)
}

func RunShell(ctx context.Context, rt *opruntime.Runtime, cfg Config, stdin io.Reader, stdout, stderr io.Writer) error {
	if rt == nil || rt.SSH == nil {
		return errors.New("tigerbeetle: operator runtime with SSH is required")
	}
	cfg, err := cfg.normalizedShell()
	if err != nil {
		return err
	}
	remote, err := remoteCommand(cfg.RunAsUser, cfg.RemotePath, replArgs(cfg))
	if err != nil {
		return err
	}
	return rt.SSH.RunPTY(ctx, remote, stdin, stdout, stderr)
}

func replArgs(cfg Config) []string {
	return []string{
		"repl",
		"--cluster=" + cfg.ClusterID,
		"--addresses=" + cfg.Addresses,
	}
}

func remoteCommand(runAs, path string, args []string) (string, error) {
	quoted := make([]string, 0, len(args)+1)
	pathWord, err := opruntime.ShellWord(path)
	if err != nil {
		return "", err
	}
	quoted = append(quoted, pathWord)
	for _, arg := range args {
		word, err := opruntime.ShellWord(arg)
		if err != nil {
			return "", err
		}
		quoted = append(quoted, word)
	}
	script := `exec "$1" "${@:2}"`
	scriptWord, err := opruntime.ShellWord(script)
	if err != nil {
		return "", err
	}
	return "sudo -u " + runAs + " /bin/bash -lc " + scriptWord + " _ " + strings.Join(quoted, " "), nil
}

func ParseUint128(s string) (tb.Uint128, error) {
	value := strings.TrimSpace(s)
	if value == "" {
		return tb.Uint128{}, errors.New("tigerbeetle: Uint128 value is required")
	}
	if strings.HasPrefix(value, "0x") || strings.HasPrefix(value, "0X") {
		parsed, err := tb.HexStringToUint128(value[2:])
		if err != nil {
			return tb.Uint128{}, fmt.Errorf("tigerbeetle: parse hex Uint128 %q: %w", s, err)
		}
		return parsed, nil
	}
	n := new(big.Int)
	if _, ok := n.SetString(value, 10); !ok {
		return tb.Uint128{}, fmt.Errorf("tigerbeetle: parse decimal Uint128 %q", s)
	}
	if n.Sign() < 0 || n.BitLen() > 128 {
		return tb.Uint128{}, fmt.Errorf("tigerbeetle: Uint128 out of range: %q", s)
	}
	return tb.BigIntToUint128(n), nil
}

func QueryFilterFlags(reversed bool) uint32 {
	return tb.QueryFilterFlags{Reversed: reversed}.ToUint32()
}

func AccountsTable(accounts []tb.Account) opruntime.Table {
	table := opruntime.Table{
		Headers: []string{
			"id",
			"debits_pending",
			"debits_posted",
			"credits_pending",
			"credits_posted",
			"user_data_128",
			"user_data_64",
			"user_data_32",
			"ledger",
			"code",
			"flags",
			"timestamp",
		},
	}
	for _, account := range accounts {
		table.Rows = append(table.Rows, []string{
			uint128Decimal(account.ID),
			uint128Decimal(account.DebitsPending),
			uint128Decimal(account.DebitsPosted),
			uint128Decimal(account.CreditsPending),
			uint128Decimal(account.CreditsPosted),
			uint128Decimal(account.UserData128),
			strconv.FormatUint(account.UserData64, 10),
			strconv.FormatUint(uint64(account.UserData32), 10),
			strconv.FormatUint(uint64(account.Ledger), 10),
			strconv.FormatUint(uint64(account.Code), 10),
			strconv.FormatUint(uint64(account.Flags), 10),
			strconv.FormatUint(account.Timestamp, 10),
		})
	}
	return table
}

func uint128Decimal(value tb.Uint128) string {
	return value.BigInt().String()
}

func normalizeAddresses(raw string) ([]string, error) {
	if strings.TrimSpace(raw) == "" {
		raw = DefaultAddresses
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		address, err := normalizeAddress(strings.TrimSpace(part))
		if err != nil {
			return nil, err
		}
		out = append(out, address)
	}
	if len(out) == 0 {
		return nil, errors.New("tigerbeetle: at least one address is required")
	}
	return out, nil
}

func normalizeAddress(raw string) (string, error) {
	if raw == "" {
		return "", errors.New("tigerbeetle: empty address")
	}
	if strings.ContainsRune(raw, 0) {
		return "", errors.New("tigerbeetle: address contains NUL")
	}
	host, port, err := net.SplitHostPort(raw)
	if err == nil {
		if host == "" {
			host = "127.0.0.1"
		}
		return net.JoinHostPort(host, port), nil
	}
	parsedPort, parseErr := strconv.ParseUint(raw, 10, 16)
	if parseErr == nil && parsedPort > 0 {
		return net.JoinHostPort("127.0.0.1", raw), nil
	}
	return "", fmt.Errorf("tigerbeetle: invalid address %q: %w", raw, err)
}

func safeUnixUser(user string) bool {
	if user == "" {
		return false
	}
	for i, r := range user {
		switch {
		case r >= 'a' && r <= 'z':
		case i > 0 && r >= '0' && r <= '9':
		case i > 0 && (r == '_' || r == '-'):
		default:
			return false
		}
	}
	return true
}
