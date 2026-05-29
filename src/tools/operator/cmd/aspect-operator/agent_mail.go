package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	opch "github.com/verself/operator-runtime/clickhouse"
	opruntime "github.com/verself/operator-runtime/runtime"
)

const (
	agentMailTarget = "//src/tools/operator/cmd/agent-mail:agent-mail"
	agentMailUser   = "agent_mail"
)

type agentMailOptions struct {
	operatorRuntimeOptions
	subject string
	body    string
	from    string
	to      string
	orgID   string
}

// cmdAgentMail uploads the agent-mail binary to the node and runs it as the
// agent_mail user, so SPIRE issues the agent-mail SVID email-service trusts.
func cmdAgentMail(args []string) error {
	fs := flagSet("agent-mail")
	opts := agentMailOptions{}
	fs.StringVar(&opts.repoRoot, "repo-root", "", "Repository root.")
	fs.StringVar(&opts.site, "site", "", "Deployment site.")
	fs.StringVar(&opts.subject, "subject", "", "Email subject (required).")
	fs.StringVar(&opts.body, "body", "", "Email body (required).")
	fs.StringVar(&opts.from, "from", "", "Override sender address.")
	fs.StringVar(&opts.to, "to", "", "Override recipient address.")
	fs.StringVar(&opts.orgID, "org-id", "", "Override owning organization ID.")
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return nil
		}
		return err
	}
	addOperatorRuntimeFlags(&opts.operatorRuntimeOptions)
	if strings.TrimSpace(opts.subject) == "" {
		return fmt.Errorf("agent-mail: --subject is required")
	}
	if strings.TrimSpace(opts.body) == "" {
		return fmt.Errorf("agent-mail: --body is required")
	}
	return runOperatorRuntime("agent.mail", opts.operatorRuntimeOptions, false, opch.Config{Database: "verself"}, func(rt *opruntime.Runtime, _ *opch.Client) error {
		remoteArgs := []string{"--subject", opts.subject, "--body", opts.body}
		for flagName, value := range map[string]string{"--from": opts.from, "--to": opts.to, "--org-id": opts.orgID} {
			if v := strings.TrimSpace(value); v != "" {
				remoteArgs = append(remoteArgs, flagName, v)
			}
		}
		fmt.Fprintf(os.Stderr, "agent-mail: target=%s user=%s subject=%q\n", agentMailTarget, agentMailUser, opts.subject)
		return runRemoteBazelExecutable(rt, agentMailTarget, "agent-mail", agentMailUser, remoteArgs)
	})
}
