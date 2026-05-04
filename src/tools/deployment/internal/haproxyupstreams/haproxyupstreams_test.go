package haproxyupstreams

import (
	"testing"

	"github.com/verself/deployment-tools/internal/nomadclient"
)

func TestValidateLoopbackAddress(t *testing.T) {
	if err := validateLoopbackAddress(nomadclient.ServiceAddress{Name: "billing-public-http", Address: "127.0.0.1", Port: 24501}); err != nil {
		t.Fatalf("validateLoopbackAddress rejected loopback endpoint: %v", err)
	}

	for _, addr := range []nomadclient.ServiceAddress{
		{Name: "billing-public-http", Address: "10.0.0.12", Port: 24501},
		{Name: "billing-public-http", Address: "127.0.0.1", Port: 0},
		{Name: "billing-public-http", Address: "127.0.0.1", Port: 70000},
	} {
		if err := validateLoopbackAddress(addr); err == nil {
			t.Fatalf("validateLoopbackAddress accepted invalid endpoint: %+v", addr)
		}
	}
}
