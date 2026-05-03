package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	mailboxclient "github.com/verself/mailbox-service/client"
)

const (
	defaultAccount = "agents"
	remotePort     = 4246
)

func main() {
	if err := run(context.Background(), os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return usageError()
	}

	// The deploy cache is the source of truth at runtime; the authored
	// The authored inventory is named per site; prod.ini matches the only
	// inventory the repo currently authors.
	inventoryDefault := filepath.Join("..", "host-configuration", "ansible", "prod.ini")
	rootFlags := flag.NewFlagSet("mailbox-tool", flag.ContinueOnError)
	rootFlags.SetOutput(io.Discard)
	inventoryPath := rootFlags.String("inventory", inventoryDefault, "Path to ansible inventory")
	if err := rootFlags.Parse(args); err != nil {
		return usageError()
	}

	remaining := rootFlags.Args()
	if len(remaining) == 0 {
		return usageError()
	}

	command := remaining[0]
	commandArgs := remaining[1:]
	switch command {
	case "help", "-h", "--help":
		fmt.Println(usageText())
		return nil
	}

	target, err := loadTarget(*inventoryPath)
	if err != nil {
		return err
	}
	tunnel, err := openTunnel(ctx, target)
	if err != nil {
		return err
	}
	defer tunnel.Close()

	client, err := mailboxclient.NewClientWithResponses(tunnel.baseURL)
	if err != nil {
		return fmt.Errorf("create mailbox client: %w", err)
	}

	switch command {
	case "accounts":
		return runAccounts(ctx, client)
	case "mailboxes":
		return runMailboxes(ctx, client, commandArgs)
	case "list":
		return runList(ctx, client, commandArgs)
	case "read":
		return runRead(ctx, client, commandArgs)
	case "code":
		return runCode(ctx, client, commandArgs)
	default:
		return fmt.Errorf("unknown command %q\n\n%s", command, usageText())
	}
}

func runAccounts(ctx context.Context, client *mailboxclient.ClientWithResponses) error {
	resp, err := client.OperatorListAccountsWithResponse(ctx)
	if err != nil {
		return fmt.Errorf("list accounts: %w", err)
	}
	if resp.JSON200 == nil || resp.JSON200.Accounts == nil {
		return responseError("list accounts", resp.StatusCode(), resp.Body)
	}
	for _, account := range *resp.JSON200.Accounts {
		fmt.Printf("%-12s  %-30s  %s\n", account.AccountId, account.EmailAddress, account.DisplayName)
	}
	return nil
}

func runMailboxes(ctx context.Context, client *mailboxclient.ClientWithResponses, args []string) error {
	flags := flag.NewFlagSet("mailboxes", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	account := flags.String("account", defaultAccount, "Mailbox account")
	if err := flags.Parse(args); err != nil {
		return usageError()
	}

	resp, err := client.OperatorListMailboxesWithResponse(ctx, *account)
	if err != nil {
		return fmt.Errorf("list mailboxes: %w", err)
	}
	if resp.JSON200 == nil || resp.JSON200.Mailboxes == nil {
		return responseError("list mailboxes", resp.StatusCode(), resp.Body)
	}
	for _, mailbox := range *resp.JSON200.Mailboxes {
		fmt.Printf("%-18s  %-18s  unread=%-4d total=%-4d %s\n",
			mailbox.Id,
			mailbox.Role,
			int(mailbox.UnreadEmails),
			int(mailbox.TotalEmails),
			mailbox.Name,
		)
	}
	return nil
}

func runList(ctx context.Context, client *mailboxclient.ClientWithResponses, args []string) error {
	flags := flag.NewFlagSet("list", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	account := flags.String("account", defaultAccount, "Mailbox account")
	limit := flags.Int("limit", 10, "Number of emails to list")
	mailboxID := flags.String("mailbox-id", "", "Optional mailbox id filter")
	if err := flags.Parse(args); err != nil {
		return usageError()
	}

	resp, err := client.OperatorListEmailsWithResponse(ctx, *account, &mailboxclient.OperatorListEmailsParams{
		Limit:     int64Ptr(*limit),
		MailboxId: stringPtr(*mailboxID),
	})
	if err != nil {
		return fmt.Errorf("list emails: %w", err)
	}
	if resp.JSON200 == nil || resp.JSON200.Emails == nil {
		return responseError("list emails", resp.StatusCode(), resp.Body)
	}
	emails := *resp.JSON200.Emails
	if len(emails) == 0 {
		fmt.Println("(empty inbox)")
		return nil
	}
	for _, email := range emails {
		fmt.Printf("  %-12s  %-19s  %-30s  %s\n",
			email.EmailId,
			trimTimestamp(email.ReceivedAt),
			email.FromEmail,
			email.Subject,
		)
	}
	return nil
}

func runRead(ctx context.Context, client *mailboxclient.ClientWithResponses, args []string) error {
	flags := flag.NewFlagSet("read", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	account := flags.String("account", defaultAccount, "Mailbox account")
	emailID := flags.String("id", "", "Email id")
	if err := flags.Parse(args); err != nil {
		return usageError()
	}
	if strings.TrimSpace(*emailID) == "" {
		return fmt.Errorf("read requires --id\n\n%s", usageText())
	}

	resp, err := client.OperatorGetEmailWithResponse(ctx, *account, *emailID)
	if err != nil {
		return fmt.Errorf("read email: %w", err)
	}
	if resp.JSON200 == nil {
		return responseError("read email", resp.StatusCode(), resp.Body)
	}
	email := resp.JSON200

	fmt.Printf("From:    %s\n", formatMailbox(email.FromName, email.FromEmail))
	fmt.Printf("To:      %s\n", formatAddressList(email.To))
	if email.Cc != nil && len(*email.Cc) > 0 {
		fmt.Printf("Cc:      %s\n", formatAddressList(email.Cc))
	}
	if email.ReplyTo != nil && len(*email.ReplyTo) > 0 {
		fmt.Printf("Reply-To:%s\n", padHeader(formatAddressList(email.ReplyTo)))
	}
	fmt.Printf("Date:    %s\n", email.ReceivedAt)
	fmt.Printf("Subject: %s\n", email.Subject)
	fmt.Println("---")
	body := strings.TrimSpace(email.TextBody)
	if body == "" {
		body = strings.TrimSpace(email.HtmlBody)
	}
	fmt.Println(body)
	return nil
}

func runCode(ctx context.Context, client *mailboxclient.ClientWithResponses, args []string) error {
	flags := flag.NewFlagSet("code", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	account := flags.String("account", defaultAccount, "Mailbox account")
	if err := flags.Parse(args); err != nil {
		return usageError()
	}

	resp, err := client.OperatorListEmailsWithResponse(ctx, *account, &mailboxclient.OperatorListEmailsParams{Limit: int64Ptr(5)})
	if err != nil {
		return fmt.Errorf("list emails for code extraction: %w", err)
	}
	if resp.JSON200 == nil || resp.JSON200.Emails == nil {
		return responseError("list emails for code extraction", resp.StatusCode(), resp.Body)
	}

	patterns := []*regexp.Regexp{
		regexp.MustCompile(`\b(\d{6})\b`),
		regexp.MustCompile(`(?i)(?:code|otp|token|pin)[:\s]+(\S+)`),
		regexp.MustCompile(`(?i)(?:verification|confirm)[:\s]+(\S+)`),
	}

	for _, summary := range *resp.JSON200.Emails {
		detail, err := client.OperatorGetEmailWithResponse(ctx, *account, summary.EmailId)
		if err != nil {
			return fmt.Errorf("read email %s: %w", summary.EmailId, err)
		}
		if detail.JSON200 == nil {
			return responseError("read email for code extraction", detail.StatusCode(), detail.Body)
		}
		text := strings.TrimSpace(detail.JSON200.Subject + " " + detail.JSON200.TextBody)
		for _, pattern := range patterns {
			matches := pattern.FindStringSubmatch(text)
			if len(matches) > 1 {
				fmt.Printf("  %s  (%s from %s: %s)\n",
					matches[1],
					trimTimestamp(summary.ReceivedAt),
					summary.FromEmail,
					summary.Subject,
				)
				return nil
			}
		}
	}

	return errors.New("no verification code found in recent emails")
}

type target struct {
	host string
	user string
}

func loadTarget(path string) (target, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return target{}, fmt.Errorf("read inventory: %w", err)
	}
	var out target
	for _, field := range strings.Fields(string(data)) {
		if strings.HasPrefix(field, "ansible_host=") && out.host == "" {
			out.host = strings.TrimPrefix(field, "ansible_host=")
		}
		if strings.HasPrefix(field, "ansible_user=") && out.user == "" {
			out.user = strings.TrimPrefix(field, "ansible_user=")
		}
	}
	if out.host == "" || out.user == "" {
		return target{}, fmt.Errorf("parse inventory %s: missing ansible_host or ansible_user", path)
	}
	return out, nil
}

type sshTunnel struct {
	cmd     *exec.Cmd
	baseURL string
}

func openTunnel(ctx context.Context, target target) (*sshTunnel, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("reserve local port: %w", err)
	}
	localPort := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()

	args := []string{
		"-o", "IPQoS=none",
		"-o", "StrictHostKeyChecking=no",
		"-o", "ExitOnForwardFailure=yes",
		"-N",
		"-L", fmt.Sprintf("127.0.0.1:%d:127.0.0.1:%d", localPort, remotePort),
		fmt.Sprintf("%s@%s", target.user, target.host),
	}
	cmd := exec.CommandContext(ctx, "ssh", args...)
	stderr := &strings.Builder{}
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start ssh tunnel: %w", err)
	}

	baseURL := fmt.Sprintf("http://127.0.0.1:%d", localPort)
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/healthz", nil)
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return &sshTunnel{cmd: cmd, baseURL: baseURL}, nil
			}
		}
		if cmd.ProcessState != nil && cmd.ProcessState.Exited() {
			return nil, fmt.Errorf("ssh tunnel exited early: %s", strings.TrimSpace(stderr.String()))
		}
		time.Sleep(100 * time.Millisecond)
	}

	_ = cmd.Process.Kill()
	_, _ = cmd.Process.Wait()
	return nil, fmt.Errorf("ssh tunnel not ready: %s", strings.TrimSpace(stderr.String()))
}

func (t *sshTunnel) Close() {
	if t == nil || t.cmd == nil || t.cmd.Process == nil {
		return
	}
	_ = t.cmd.Process.Kill()
	_, _ = t.cmd.Process.Wait()
}

func usageError() error {
	return errors.New(usageText())
}

func usageText() string {
	return strings.TrimSpace(`
Usage:
  mailbox-tool [--inventory path] accounts
  mailbox-tool [--inventory path] mailboxes [--account agents]
  mailbox-tool [--inventory path] list [--account agents] [--limit 10] [--mailbox-id id]
  mailbox-tool [--inventory path] read [--account agents] --id EMAIL_ID
  mailbox-tool [--inventory path] code [--account agents]
`)
}

func responseError(action string, status int, body []byte) error {
	return fmt.Errorf("%s failed with HTTP %d: %s", action, status, strings.TrimSpace(string(body)))
}

func trimTimestamp(value string) string {
	if value == "" {
		return "?"
	}
	return strings.TrimSuffix(strings.ReplaceAll(value, "T", " "), "Z")
}

func int64Ptr(value int) *int64 {
	out := int64(value)
	return &out
}

func stringPtr(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func formatAddressList(list *[]mailboxclient.MailboxOperatorAddress) string {
	if list == nil || len(*list) == 0 {
		return ""
	}
	parts := make([]string, 0, len(*list))
	for _, address := range *list {
		parts = append(parts, formatMailbox(address.Name, address.Email))
	}
	return strings.Join(parts, ", ")
}

func formatMailbox(name, email string) string {
	if name == "" {
		return email
	}
	return fmt.Sprintf("%s <%s>", name, email)
}

func padHeader(value string) string {
	if value == "" {
		return ""
	}
	return " " + value
}
