package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	opch "github.com/verself/operator-runtime/clickhouse"
	opruntime "github.com/verself/operator-runtime/runtime"
	"gopkg.in/yaml.v3"
)

type mailOptions struct {
	operatorRuntimeOptions
}

type mailMainVars struct {
	VerselfDomain    string `yaml:"verself_domain"`
	ResendSubdomain  string `yaml:"resend_subdomain"`
	ResendSenderName string `yaml:"resend_sender_name"`
}

func cmdMail(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("mail: missing subcommand (try `send` or `passwords`)")
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "send":
		return cmdMailSend(rest)
	case "passwords":
		return cmdMailPasswords(rest)
	default:
		return fmt.Errorf("mail: unknown subcommand: %s", sub)
	}
}

func cmdMailSend(args []string) error {
	fs := flagSet("mail send")
	opts := &mailOptions{}
	addOperatorRuntimeFlags(&opts.operatorRuntimeOptions)
	fs.StringVar(&opts.site, "site", opts.site, "Deploy site")
	fs.StringVar(&opts.repoRoot, "repo-root", "", "verself-sh checkout root (defaults to cwd)")
	to := fs.String("to", "", "Recipient (agents, ceo, or user@example.com)")
	subject := fs.String("subject", "", "Email subject")
	body := fs.String("body", "", "Email body")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *to == "" {
		return errors.New("mail send: --to is required")
	}
	if *subject == "" {
		return errors.New("mail send: --subject is required")
	}
	if *body == "" {
		return errors.New("mail send: --body is required")
	}
	return runOperatorRuntime("mail.send", opts.operatorRuntimeOptions, false, opch.Config{Database: "verself"}, func(rt *opruntime.Runtime, _ *opch.Client) error {
		cfg, err := loadMailConfig(rt.RepoRoot)
		if err != nil {
			return err
		}
		apiKey, err := opruntime.DecryptSOPSValue(rt.Ctx, opruntime.SecretsPath(rt.RepoRoot), "resend_api_key")
		if err != nil {
			return err
		}
		toAddress := *to
		if toAddress == "ceo" || toAddress == "agents" {
			toAddress = toAddress + "@" + cfg.VerselfDomain
		}
		fromAddress := "noreply@" + cfg.ResendSubdomain + "." + cfg.VerselfDomain
		payload, err := json.Marshal(map[string]any{
			"from":    fmt.Sprintf("%s <%s>", cfg.ResendSenderName, fromAddress),
			"to":      []string{toAddress},
			"subject": *subject,
			"text":    *body,
		})
		if err != nil {
			return err
		}
		req, err := http.NewRequestWithContext(rt.Ctx, http.MethodPost, "https://api.resend.com/emails", bytes.NewReader(payload))
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+apiKey)
		req.Header.Set("Content-Type", "application/json")
		client := &http.Client{Timeout: 5 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			return fmt.Errorf("send via Resend: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()
		raw, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
		if err != nil {
			return err
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return fmt.Errorf("send via Resend: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
		}
		var out struct {
			ID string `json:"id"`
		}
		_ = json.Unmarshal(raw, &out)
		fmt.Fprintf(os.Stdout, "sent via Resend: %s -> %s\n", fromAddress, toAddress)
		if out.ID != "" {
			fmt.Fprintf(os.Stdout, "resend_id: %s\n", out.ID)
		} else {
			fmt.Fprintln(os.Stdout, strings.TrimSpace(string(raw)))
		}
		return nil
	})
}

func cmdMailPasswords(args []string) error {
	fs := flagSet("mail passwords")
	opts := &mailOptions{}
	addOperatorRuntimeFlags(&opts.operatorRuntimeOptions)
	fs.StringVar(&opts.site, "site", opts.site, "Deploy site")
	fs.StringVar(&opts.repoRoot, "repo-root", "", "verself-sh checkout root (defaults to cwd)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	return runOperatorRuntime("mail.passwords", opts.operatorRuntimeOptions, false, opch.Config{Database: "verself"}, func(rt *opruntime.Runtime, _ *opch.Client) error {
		cfg, err := loadMailConfig(rt.RepoRoot)
		if err != nil {
			return err
		}
		for _, label := range []string{"ceo", "agents"} {
			password, err := opruntime.DecryptSOPSValue(rt.Ctx, opruntime.SecretsPath(rt.RepoRoot), "stalwart_"+label+"_password")
			if err != nil {
				return err
			}
			fmt.Fprintf(os.Stdout, "%s@%s:\n%s\n\n", label, cfg.VerselfDomain, password)
		}
		return nil
	})
}

func loadMailConfig(repoRoot string) (mailMainVars, error) {
	path := filepath.Join(repoRoot, "src", "host-configuration", "ansible", "group_vars", "all", "topology", "ops.yml")
	raw, err := os.ReadFile(path)
	if err != nil {
		return mailMainVars{}, fmt.Errorf("read %s: %w", path, err)
	}
	var cfg mailMainVars
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		return mailMainVars{}, fmt.Errorf("parse %s: %w", path, err)
	}
	missing := []string{}
	if cfg.VerselfDomain == "" {
		missing = append(missing, "verself_domain")
	}
	if cfg.ResendSubdomain == "" {
		missing = append(missing, "resend_subdomain")
	}
	if cfg.ResendSenderName == "" {
		missing = append(missing, "resend_sender_name")
	}
	if len(missing) > 0 {
		return mailMainVars{}, fmt.Errorf("%s missing %s", path, strings.Join(missing, ", "))
	}
	return cfg, nil
}
