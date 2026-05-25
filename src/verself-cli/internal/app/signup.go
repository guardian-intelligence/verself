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
	Message               string `json:"message"`
	Status                string `json:"status"`
	VerificationExpiresAt string `json:"verificationExpiresAt"`
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
	result, err := client.IAM.StartSignup(ctx, verself.StartSignupInput{
		Email:                   emailValue,
		OrganizationDisplayName: orgDisplayName,
		OrganizationSlug:        trimOptionalString(*slug),
		GivenName:               trimOptionalString(*givenName),
		FamilyName:              trimOptionalString(*familyName),
	})
	if err != nil {
		return err
	}
	return writeJSON(c.out, signupStartOutputFromResult(result))
}

func (c CLI) authSignupVerify(ctx context.Context, args []string) error {
	fs, iamFlags := publicIAMFlagSet("auth signup verify", c.err)
	actionURL := fs.String("url", "", "signup verification URL")
	signupIntentID := fs.String("signup-intent-id", "", "signup intent id")
	verificationToken := fs.String("verification-token", "", "signup verification token")
	organizationDisplayName := fs.String("org", "", "organization display name")
	organizationSlug := fs.String("slug", "", "organization slug")
	if err := parseInterspersed(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("usage: auth signup verify --url URL")
	}
	credentials, err := signupVerificationCredentials(*actionURL, *signupIntentID, *verificationToken)
	if err != nil {
		return err
	}
	verifyOrganizationDisplayName := trimOptionalString(*organizationDisplayName)
	verifyOrganizationSlug := trimOptionalString(*organizationSlug)
	client, err := c.publicIAMClient(*iamFlags)
	if err != nil {
		return err
	}
	result, err := client.IAM.VerifySignup(ctx, verself.VerifySignupInput{
		SignupIntentID:          credentials.signupIntentID,
		VerificationToken:       credentials.verificationToken,
		OrganizationDisplayName: verifyOrganizationDisplayName,
		OrganizationSlug:        verifyOrganizationSlug,
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

func signupStartOutputFromResult(result verself.SignupStartResult) signupStartOutput {
	return signupStartOutput{
		Message:               result.Message,
		Status:                result.Status,
		VerificationExpiresAt: result.VerificationExpiresAt,
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

type signupVerificationCredentialSet struct {
	signupIntentID    string
	verificationToken string
}

func signupVerificationCredentials(actionURL, signupIntentID, verificationToken string) (signupVerificationCredentialSet, error) {
	intentID := strings.TrimSpace(signupIntentID)
	token := strings.TrimSpace(verificationToken)
	credentials := signupVerificationCredentialSet{}
	if strings.TrimSpace(actionURL) != "" {
		urlCredentials, err := signupVerificationCredentialsFromURL(actionURL)
		if err != nil {
			return signupVerificationCredentialSet{}, err
		}
		if intentID != "" && intentID != urlCredentials.signupIntentID {
			return signupVerificationCredentialSet{}, errors.New("--signup-intent-id does not match --url")
		}
		if token != "" && token != urlCredentials.verificationToken {
			return signupVerificationCredentialSet{}, errors.New("--verification-token does not match --url")
		}
		credentials = urlCredentials
		intentID = urlCredentials.signupIntentID
		token = urlCredentials.verificationToken
	}
	if intentID == "" || token == "" {
		return signupVerificationCredentialSet{}, errors.New("auth signup verify requires --url or both --signup-intent-id and --verification-token")
	}
	credentials.signupIntentID = intentID
	credentials.verificationToken = token
	return credentials, nil
}

func signupVerificationCredentialsFromURL(raw string) (signupVerificationCredentialSet, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return signupVerificationCredentialSet{}, fmt.Errorf("parse signup verification URL: %w", err)
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
		return signupVerificationCredentialSet{}, errors.New("signup verification URL must include signup_intent_id and verification_token")
	}
	return signupVerificationCredentialSet{
		signupIntentID:    intentID,
		verificationToken: token,
	}, nil
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
