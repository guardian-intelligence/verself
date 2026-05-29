// Command agent-mail sends a notification email from a Verself agent to the
// operator through email-service's internal endpoint.
//
// It must run as the agent_mail user on a Verself node so the SPIRE workload
// API issues an SVID for spiffe://<td>/svc/agent-mail, which email-service's
// internal peer allowlist trusts. The HTML body is a React Email template
// rendered to a Go text/template at build time (see the package's BUILD.bazel);
// the plaintext alternative is assembled here. Delivery, idempotency, and the
// outbound ledger all stay inside email-service.
//
// Usage:
//
//	agent-mail --subject="build green" --body="all checks passed"
//	agent-mail --subject="..." --body=-   # read body from stdin
package main

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"flag"
	"fmt"
	"html"
	"io"
	"os"
	"strings"
	"text/template"

	emailinternalclient "github.com/verself/email-service/internalclient"
	workloadauth "github.com/verself/service-runtime/workload"
)

//go:embed agent_notification.html.tmpl
var htmlTemplateSource string

// Parsed once; the template is the React Email render with {{.Subject}} and
// {{.Body}} actions. Fields are HTML-escaped before execution, so text/template
// (not html/template) is correct and avoids re-parsing the rendered markup.
var htmlTemplate = template.Must(template.New("agent-notification").Parse(htmlTemplateSource))

const (
	defaultFrom   = "agents@guardianintelligence.org"
	defaultTo     = "anveio@guardianintelligence.org"
	defaultOrgID  = "org_B7HWGKW0SH7G4EXW9XT8TCT60C"
	actorSubject  = workloadauth.ServiceAgentMail
	workflowKey   = "agent.mail.notify"
	spiffeDefault = "unix:///run/spire-agent/sockets/agent.sock"
)

func main() {
	if err := run(context.Background(), os.Args[1:], os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "agent-mail:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	fs := flag.NewFlagSet("agent-mail", flag.ContinueOnError)
	subject := fs.String("subject", "", "Email subject (required).")
	body := fs.String("body", "", "Email body; pass - to read from stdin (required).")
	from := fs.String("from", defaultFrom, "Sender address.")
	to := fs.String("to", defaultTo, "Recipient address.")
	orgID := fs.String("org-id", defaultOrgID, "Owning organization ID.")
	idempotencyKey := fs.String("idempotency-key", "", "Override the content-derived idempotency key.")
	spiffeSocket := fs.String("spiffe-socket", spiffeDefault, "SPIFFE workload API socket address.")
	if err := fs.Parse(args); err != nil {
		return err
	}

	subjectText := strings.TrimSpace(*subject)
	if subjectText == "" {
		return fmt.Errorf("--subject is required")
	}
	bodyText := *body
	if strings.TrimSpace(bodyText) == "-" {
		raw, err := io.ReadAll(stdin)
		if err != nil {
			return fmt.Errorf("read body from stdin: %w", err)
		}
		bodyText = string(raw)
	}
	bodyText = strings.TrimRight(bodyText, "\n")
	if strings.TrimSpace(bodyText) == "" {
		return fmt.Errorf("--body is required")
	}

	htmlBody, err := renderHTML(subjectText, bodyText)
	if err != nil {
		return err
	}
	textBody := renderText(subjectText, bodyText)

	key := strings.TrimSpace(*idempotencyKey)
	if key == "" {
		key = contentKey(*from, *to, subjectText, bodyText)
	}

	source, err := workloadauth.Source(ctx, *spiffeSocket)
	if err != nil {
		return fmt.Errorf("spiffe source: %w", err)
	}
	defer func() { _ = source.Close() }()

	httpClient, err := workloadauth.MTLSClientForService(source, workloadauth.ServiceEmail, nil)
	if err != nil {
		return fmt.Errorf("email-service mtls: %w", err)
	}
	client, err := emailinternalclient.New(workloadauth.InternalURL(workloadauth.ServiceEmail), httpClient)
	if err != nil {
		return fmt.Errorf("email-service client: %w", err)
	}

	result, err := client.Send(ctx, emailinternalclient.SendRequest{
		OrgID:          *orgID,
		FromAddress:    *from,
		ToAddress:      *to,
		Subject:        subjectText,
		TextBody:       textBody,
		HTMLBody:       htmlBody,
		IdempotencyKey: key,
		WorkflowKey:    workflowKey,
		ActorSubject:   actorSubject,
	})
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(stdout, "sent message_id=%s provider=%s provider_message_id=%s status=%s\n",
		result.MessageID, result.Provider, result.ProviderMessageID, result.Status)
	return nil
}

func renderHTML(subject, body string) (string, error) {
	var buf strings.Builder
	if err := htmlTemplate.Execute(&buf, struct{ Subject, Body string }{
		Subject: html.EscapeString(subject),
		Body:    html.EscapeString(body),
	}); err != nil {
		return "", fmt.Errorf("render html: %w", err)
	}
	return buf.String(), nil
}

func renderText(subject, body string) string {
	return subject + "\n\n" + body + "\n\n— Sent by an agent working in the verself.sh repository.\n"
}

func contentKey(from, to, subject, body string) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{from, to, subject, body}, "\x00")))
	return "agent-mail:" + hex.EncodeToString(sum[:])
}
