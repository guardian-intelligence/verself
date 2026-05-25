package identity

import (
	"fmt"
	"net/mail"
	"strings"
	"unicode/utf8"

	"golang.org/x/net/idna"
	"golang.org/x/text/unicode/norm"
)

const (
	emailAddressMaxBytes = 320
	emailLocalMaxBytes   = 64
	emailDomainMaxBytes  = 255
)

type EmailAddress struct {
	DeliveryAddress string
	IdentityKey     string
}

func ParseEmailAddress(value string) (EmailAddress, error) {
	raw := strings.TrimSpace(value)
	if raw == "" {
		return EmailAddress{}, fmt.Errorf("%w: email is required", ErrInvalidInput)
	}
	parsed, err := mail.ParseAddress(raw)
	if err != nil {
		return EmailAddress{}, fmt.Errorf("%w: email is invalid", ErrInvalidInput)
	}
	if parsed.Name != "" || parsed.Address != raw {
		return EmailAddress{}, fmt.Errorf("%w: email must be a single mailbox address", ErrInvalidInput)
	}
	if strings.Count(parsed.Address, "@") != 1 {
		return EmailAddress{}, fmt.Errorf("%w: email local-part must be unquoted", ErrInvalidInput)
	}
	local, domain, ok := strings.Cut(parsed.Address, "@")
	if !ok {
		return EmailAddress{}, fmt.Errorf("%w: email is invalid", ErrInvalidInput)
	}
	if local == "" || domain == "" {
		return EmailAddress{}, fmt.Errorf("%w: email local-part and domain are required", ErrInvalidInput)
	}
	if strings.HasPrefix(domain, "[") || strings.HasSuffix(domain, "]") {
		return EmailAddress{}, fmt.Errorf("%w: email domain literals are unsupported", ErrInvalidInput)
	}
	if strings.ContainsAny(local, "\"\r\n\t") || strings.ContainsAny(domain, "\r\n\t") {
		return EmailAddress{}, fmt.Errorf("%w: email contains unsupported syntax", ErrInvalidInput)
	}
	local = norm.NFC.String(local)
	if !utf8.ValidString(local) {
		return EmailAddress{}, fmt.Errorf("%w: email local-part is invalid UTF-8", ErrInvalidInput)
	}
	domainASCII, err := idna.Lookup.ToASCII(strings.TrimSuffix(domain, "."))
	if err != nil {
		return EmailAddress{}, fmt.Errorf("%w: email domain is invalid", ErrInvalidInput)
	}
	domainASCII = strings.ToLower(domainASCII)
	if domainASCII == "" {
		return EmailAddress{}, fmt.Errorf("%w: email domain is required", ErrInvalidInput)
	}
	if len(local) > emailLocalMaxBytes || len(domainASCII) > emailDomainMaxBytes {
		return EmailAddress{}, fmt.Errorf("%w: email is too long", ErrInvalidInput)
	}
	delivery := local + "@" + domainASCII
	if len(delivery) > emailAddressMaxBytes {
		return EmailAddress{}, fmt.Errorf("%w: email is too long", ErrInvalidInput)
	}
	identityLocal := strings.ToLower(local)
	if base, _, ok := strings.Cut(identityLocal, "+"); ok {
		identityLocal = base
	}
	if identityLocal == "" {
		return EmailAddress{}, fmt.Errorf("%w: email local-part identity is empty", ErrInvalidInput)
	}
	return EmailAddress{
		DeliveryAddress: delivery,
		IdentityKey:     identityLocal + "@" + domainASCII,
	}, nil
}
