package transport

import "testing"

func TestValidateListenAddressDefaultsToLoopbackOnly(t *testing.T) {
	for _, addr := range []string{"127.0.0.1:0", "127.10.20.30:9182", "localhost:0", "[::1]:0"} {
		t.Run(addr, func(t *testing.T) {
			if err := ValidateListenAddress(addr, false); err != nil {
				t.Fatalf("ValidateListenAddress(%q) returned error: %v", addr, err)
			}
		})
	}
	if err := ValidateListenAddress("0.0.0.0:0", false); err == nil {
		t.Fatal("expected public bind without explicit flag to fail")
	}
	if err := ValidateListenAddress("0.0.0.0:0", true); err != nil {
		t.Fatalf("expected explicit public bind to pass: %v", err)
	}
}
