package identity

import (
	"errors"
	"testing"
)

func TestParseEmailAddressCanonicalizesDeliveryAndIdentity(t *testing.T) {
	got, err := ParseEmailAddress("Founder+Signup@Bücher.Example")
	if err != nil {
		t.Fatalf("ParseEmailAddress: %v", err)
	}
	if got.DeliveryAddress != "Founder+Signup@xn--bcher-kva.example" {
		t.Fatalf("delivery = %q", got.DeliveryAddress)
	}
	if got.IdentityKey != "founder@xn--bcher-kva.example" {
		t.Fatalf("identity key = %q", got.IdentityKey)
	}
}

func TestParseEmailAddressNormalizesUnicodeLocalPart(t *testing.T) {
	got, err := ParseEmailAddress("e\u0301+tag@example.test")
	if err != nil {
		t.Fatalf("ParseEmailAddress: %v", err)
	}
	if got.DeliveryAddress != "é+tag@example.test" {
		t.Fatalf("delivery = %q", got.DeliveryAddress)
	}
	if got.IdentityKey != "é@example.test" {
		t.Fatalf("identity key = %q", got.IdentityKey)
	}
}

func TestParseEmailAddressRejectsUnsupportedMailboxForms(t *testing.T) {
	for _, input := range []string{
		"Founder <founder@example.test>",
		"founder(comment)@example.test",
		"\"founder\"@example.test",
		"+tag@example.test",
		"founder@[127.0.0.1]",
	} {
		t.Run(input, func(t *testing.T) {
			_, err := ParseEmailAddress(input)
			if !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("ParseEmailAddress err = %v, want ErrInvalidInput", err)
			}
		})
	}
}
