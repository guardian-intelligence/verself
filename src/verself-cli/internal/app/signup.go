package app

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"strings"
	"unicode"

	verself "github.com/verself/verself-go"
)

type signupStartOutput struct {
	Message                 string `json:"message"`
	SignupIntentID          string `json:"signupIntentId"`
	ResourceName            string `json:"resourceName"`
	OrganizationDisplayName string `json:"organizationDisplayName"`
	OrganizationSlug        string `json:"organizationSlug,omitempty"`
	Status                  string `json:"status"`
	VerificationExpiresAt   string `json:"verificationExpiresAt"`
}

type signupVerifyOutput struct {
	Message      string               `json:"message"`
	Organization verself.Organization `json:"organization"`
	LoginURL     string               `json:"loginUrl"`
}

type publicIAMFlags struct {
	iamURL      string
	traceparent string
}

func (c CLI) authSignup(ctx context.Context, args []string) error {
	if len(args) > 0 && args[0] == "verify" {
		return c.authSignupVerify(ctx, args[1:])
	}
	return c.authSignupStart(ctx, args)
}

func (c CLI) authSignupStart(ctx context.Context, args []string) error {
	fs, iamFlags := publicIAMFlagSet("auth signup", c.err)
	email := fs.String("email", "", "email address to verify")
	org := fs.String("org", "", "organization display name")
	slug := fs.String("slug", "", "organization slug")
	givenName := fs.String("given-name", "", "account given name")
	familyName := fs.String("family-name", "", "account family name")
	idempotencyKey := fs.String("idempotency-key", "", "stable mutation key")
	if err := parseInterspersed(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("usage: auth signup --email EMAIL [--org NAME] [--slug SLUG]")
	}
	emailValue := strings.TrimSpace(*email)
	if emailValue == "" {
		return errors.New("auth signup requires --email")
	}
	orgDisplayName := strings.TrimSpace(*org)
	if orgDisplayName == "" {
		orgDisplayName = defaultSignupOrganizationDisplayName(emailValue)
	}
	if orgDisplayName == "" {
		return errors.New("auth signup requires --org when the organization name cannot be derived from --email")
	}
	client, err := c.publicIAMClient(*iamFlags)
	if err != nil {
		return err
	}
	intent, err := client.IAM.StartSignup(ctx, verself.StartSignupInput{
		Email:                   emailValue,
		OrganizationDisplayName: orgDisplayName,
		OrganizationSlug:        trimOptionalString(*slug),
		GivenName:               trimOptionalString(*givenName),
		FamilyName:              trimOptionalString(*familyName),
		IdempotencyKey:          *idempotencyKey,
	})
	if err != nil {
		return err
	}
	return writeJSON(c.out, signupStartOutputFromIntent(emailValue, intent))
}

func (c CLI) authSignupVerify(ctx context.Context, args []string) error {
	fs, iamFlags := publicIAMFlagSet("auth signup verify", c.err)
	actionURL := fs.String("url", "", "signup verification URL")
	signupIntentID := fs.String("signup-intent-id", "", "signup intent id")
	verificationToken := fs.String("verification-token", "", "signup verification token")
	passwordEnv := fs.String("password-env", "", "environment variable containing the account password")
	passwordFile := fs.String("password-file", "", "owner-only file containing the account password")
	passwordStdin := fs.Bool("password-stdin", false, "read the account password from stdin")
	idempotencyKey := fs.String("idempotency-key", "", "stable mutation key")
	if err := parseInterspersed(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("usage: auth signup verify --url URL --password-env NAME")
	}
	intentID, token, err := signupVerificationCredentials(*actionURL, *signupIntentID, *verificationToken)
	if err != nil {
		return err
	}
	password, err := c.signupVerificationPassword(signupPasswordOptions{
		Env:   *passwordEnv,
		File:  *passwordFile,
		Stdin: *passwordStdin,
	})
	if err != nil {
		return err
	}
	client, err := c.publicIAMClient(*iamFlags)
	if err != nil {
		return err
	}
	result, err := client.IAM.VerifySignup(ctx, verself.VerifySignupInput{
		SignupIntentID:    intentID,
		VerificationToken: token,
		Credential:        verself.AccountCredential{Password: password},
		IdempotencyKey:    *idempotencyKey,
	})
	if err != nil {
		return err
	}
	return writeJSON(c.out, signupVerifyOutputFromResult(result))
}

func publicIAMFlagSet(name string, stderr io.Writer) (*flag.FlagSet, *publicIAMFlags) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	flags := &publicIAMFlags{}
	fs.StringVar(&flags.iamURL, "iam-url", "", "IAM service base URL")
	fs.StringVar(&flags.traceparent, "traceparent", "", "trace context to join")
	return fs, flags
}

func (c CLI) publicIAMClient(flags publicIAMFlags) (*verself.Client, error) {
	iamURL := strings.TrimSpace(flags.iamURL)
	if iamURL == "" {
		iamURL = strings.TrimSpace(c.getenv("VERSELF_IAM_API_URL"))
	}
	return verself.New(verself.Options{
		IAMURL:      iamURL,
		Traceparent: flags.traceparent,
	})
}

func signupStartOutputFromIntent(email string, intent verself.SignupIntent) signupStartOutput {
	message := fmt.Sprintf("Signup started for %s; verification email delivery is queued.", email)
	if strings.TrimSpace(intent.VerificationExpiresAt) != "" {
		message = fmt.Sprintf("Signup started for %s; verification email delivery is queued and valid until %s.", email, intent.VerificationExpiresAt)
	}
	return signupStartOutput{
		Message:                 message,
		SignupIntentID:          intent.SignupIntentID,
		ResourceName:            intent.ResourceName,
		OrganizationDisplayName: intent.OrganizationDisplayName,
		OrganizationSlug:        intent.OrganizationSlug,
		Status:                  intent.Status,
		VerificationExpiresAt:   intent.VerificationExpiresAt,
	}
}

func signupVerifyOutputFromResult(result verself.SignupVerificationResult) signupVerifyOutput {
	message := fmt.Sprintf("Organization %s is ready. Run `verself auth login` to create a local session.", result.Organization.DisplayName)
	if strings.TrimSpace(result.LoginURL) != "" {
		message = fmt.Sprintf("Organization %s is ready. Sign in at %s or run `verself auth login` to create a local session.", result.Organization.DisplayName, result.LoginURL)
	}
	return signupVerifyOutput{
		Message:      message,
		Organization: result.Organization,
		LoginURL:     result.LoginURL,
	}
}

func signupVerificationCredentials(actionURL, signupIntentID, verificationToken string) (string, string, error) {
	intentID := strings.TrimSpace(signupIntentID)
	token := strings.TrimSpace(verificationToken)
	if strings.TrimSpace(actionURL) != "" {
		urlIntentID, urlToken, err := signupVerificationCredentialsFromURL(actionURL)
		if err != nil {
			return "", "", err
		}
		if intentID != "" && intentID != urlIntentID {
			return "", "", errors.New("--signup-intent-id does not match --url")
		}
		if token != "" && token != urlToken {
			return "", "", errors.New("--verification-token does not match --url")
		}
		intentID = urlIntentID
		token = urlToken
	}
	if intentID == "" || token == "" {
		return "", "", errors.New("auth signup verify requires --url or both --signup-intent-id and --verification-token")
	}
	return intentID, token, nil
}

func signupVerificationCredentialsFromURL(raw string) (string, string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", "", fmt.Errorf("parse signup verification URL: %w", err)
	}
	query := parsed.Query()
	intentID := firstNonEmpty(
		query.Get("signup_intent_id"),
		query.Get("signupIntentId"),
		query.Get("signup_intent"),
	)
	token := firstNonEmpty(
		query.Get("verification_token"),
		query.Get("verificationToken"),
		query.Get("token"),
	)
	if intentID == "" || token == "" {
		return "", "", errors.New("signup verification URL must include signup_intent_id and verification_token")
	}
	return intentID, token, nil
}

type signupPasswordOptions struct {
	Env   string
	File  string
	Stdin bool
}

func (c CLI) signupVerificationPassword(opts signupPasswordOptions) (string, error) {
	sources := 0
	if strings.TrimSpace(opts.Env) != "" {
		sources++
	}
	if strings.TrimSpace(opts.File) != "" {
		sources++
	}
	if opts.Stdin {
		sources++
	}
	if sources != 1 {
		return "", errors.New("auth signup verify requires exactly one of --password-env, --password-file, or --password-stdin")
	}
	var password string
	switch {
	case strings.TrimSpace(opts.Env) != "":
		password = c.getenv(strings.TrimSpace(opts.Env))
	case strings.TrimSpace(opts.File) != "":
		secret, err := readOwnerOnlySecretFile(strings.TrimSpace(opts.File), "password file")
		if err != nil {
			return "", err
		}
		password = secret
	case opts.Stdin:
		data, err := io.ReadAll(c.in)
		if err != nil {
			return "", err
		}
		password = string(data)
	}
	password = strings.TrimSpace(password)
	if password == "" {
		return "", errors.New("signup verification password is empty")
	}
	return password, nil
}

func trimOptionalString(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func defaultSignupOrganizationDisplayName(email string) string {
	local, _, ok := strings.Cut(strings.TrimSpace(email), "@")
	if !ok {
		return ""
	}
	local = strings.TrimSpace(local)
	if local == "" {
		return ""
	}
	var b strings.Builder
	lastWasSpace := true
	for _, r := range local {
		switch {
		case unicode.IsLetter(r), unicode.IsDigit(r):
			if lastWasSpace {
				b.WriteRune(unicode.ToUpper(r))
			} else {
				b.WriteRune(r)
			}
			lastWasSpace = false
		default:
			if !lastWasSpace {
				b.WriteByte(' ')
				lastWasSpace = true
			}
		}
	}
	return strings.TrimSpace(b.String())
}
