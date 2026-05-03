package auth

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestGeneratePairingTokenUsesConfiguredAlphabet(t *testing.T) {
	tokens := make(map[string]bool)
	for i := 0; i < 256; i++ {
		token, err := GeneratePairingToken(nil)
		if err != nil {
			t.Fatalf("generate token: %v", err)
		}
		if len(token) != PairingTokenLength {
			t.Fatalf("token length = %d, want %d", len(token), PairingTokenLength)
		}
		for _, ch := range token {
			if !strings.ContainsRune(PairingAlphabet, ch) {
				t.Fatalf("token %q contains character outside alphabet: %q", token, ch)
			}
			if strings.ContainsRune("0ILOU", ch) {
				t.Fatalf("token %q contains excluded confusable: %q", token, ch)
			}
		}
		tokens[token] = true
	}
	if len(tokens) < 250 {
		t.Fatalf("generated too many duplicate tokens: %d unique", len(tokens))
	}
}

func TestNormalizePairingTokenIsCaseInsensitiveAndRejectsConfusables(t *testing.T) {
	got, err := NormalizePairingToken("abcd-efgh-jkmn")
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if got != "ABCDEFGHJKMN" {
		t.Fatalf("normalized token = %q", got)
	}

	for _, token := range []string{
		"0BCDEFGHJKMN",
		"IBCDEFGHJKMN",
		"OBCDEFGHJKMN",
		"LBCDEFGHJKMN",
		"UBCDEFGHJKMN",
		"SHORT",
	} {
		if _, err := NormalizePairingToken(token); !errors.Is(err, ErrInvalidPairingToken) {
			t.Fatalf("NormalizePairingToken(%q) err = %v, want ErrInvalidPairingToken", token, err)
		}
	}
}

func TestEnsureInitialPairingOnlyCreatesOnce(t *testing.T) {
	ctx := context.Background()
	service := newTestBootstrapService(t, nil)

	first, created, err := service.EnsureInitialPairing(ctx)
	if err != nil {
		t.Fatalf("ensure first: %v", err)
	}
	if !created {
		t.Fatal("first ensure did not create")
	}
	if first.Role != PairingRoleOwner {
		t.Fatalf("initial role = %s", first.Role)
	}

	second, created, err := service.EnsureInitialPairing(ctx)
	if err != nil {
		t.Fatalf("ensure second: %v", err)
	}
	if created {
		t.Fatalf("second ensure created token: %+v", second)
	}

	_, err = service.ConsumePairing(ctx, ConsumePairingRequest{
		PairingToken: first.DisplayToken,
		InstanceID:   "desktop-1",
	})
	if err != nil {
		t.Fatalf("consume initial: %v", err)
	}
	_, created, err = service.EnsureInitialPairing(ctx)
	if err != nil {
		t.Fatalf("ensure after consume: %v", err)
	}
	if created {
		t.Fatal("ensure regenerated after initial token was consumed")
	}
}

func TestPairingConsumeIsSingleUseAndAppendOnly(t *testing.T) {
	ctx := context.Background()
	service := newTestBootstrapService(t, bytes.NewReader(bytes.Repeat([]byte{7}, 128)))

	issued, err := service.CreatePairing(ctx, CreatePairingRequest{Role: PairingRoleClient})
	if err != nil {
		t.Fatalf("create pairing: %v", err)
	}
	if lineCount(t, service.store.path) != 1 {
		t.Fatalf("line count after create = %d", lineCount(t, service.store.path))
	}

	record, err := service.ConsumePairing(ctx, ConsumePairingRequest{
		PairingToken: strings.ToLower(issued.DisplayToken),
		InstanceID:   "desktop-1",
	})
	if err != nil {
		t.Fatalf("consume pairing: %v", err)
	}
	if record.ConsumedAt == nil || record.ConsumedBy != "desktop-1" || record.Role != PairingRoleClient {
		t.Fatalf("bad consumed record: %+v", record)
	}
	if lineCount(t, service.store.path) != 2 {
		t.Fatalf("line count after consume = %d", lineCount(t, service.store.path))
	}

	if _, err := service.ConsumePairing(ctx, ConsumePairingRequest{
		PairingToken: issued.DisplayToken,
		InstanceID:   "desktop-2",
	}); !errors.Is(err, ErrPairingConsumed) {
		t.Fatalf("replay err = %v, want ErrPairingConsumed", err)
	}
	if lineCount(t, service.store.path) != 2 {
		t.Fatalf("replay appended a line; line count = %d", lineCount(t, service.store.path))
	}
}

func TestPairingConsumeIsAtomicAcrossConcurrentRequests(t *testing.T) {
	ctx := context.Background()
	service := newTestBootstrapService(t, nil)
	issued, err := service.CreatePairing(ctx, CreatePairingRequest{Role: PairingRoleOwner})
	if err != nil {
		t.Fatalf("create pairing: %v", err)
	}

	var wg sync.WaitGroup
	errs := make(chan error, 16)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := service.ConsumePairing(ctx, ConsumePairingRequest{
				PairingToken: issued.DisplayToken,
				InstanceID:   "desktop-concurrent",
			})
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)

	successes := 0
	consumed := 0
	for err := range errs {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, ErrPairingConsumed):
			consumed++
		default:
			t.Fatalf("unexpected concurrent consume error: %v", err)
		}
	}
	if successes != 1 || consumed != 15 {
		t.Fatalf("successes=%d consumed=%d, want 1/15", successes, consumed)
	}
}

func TestRevokePreventsConsumption(t *testing.T) {
	ctx := context.Background()
	service := newTestBootstrapService(t, nil)
	issued, err := service.CreatePairing(ctx, CreatePairingRequest{Role: PairingRoleClient})
	if err != nil {
		t.Fatalf("create pairing: %v", err)
	}
	record, err := service.RevokePairing(ctx, RevokePairingRequest{TokenID: issued.TokenID, Actor: "owner"})
	if err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if record.RevokedAt == nil || record.RevokedBy != "owner" {
		t.Fatalf("bad revoked record: %+v", record)
	}
	if _, err := service.ConsumePairing(ctx, ConsumePairingRequest{
		PairingToken: issued.DisplayToken,
		InstanceID:   "desktop-1",
	}); !errors.Is(err, ErrPairingRevoked) {
		t.Fatalf("consume revoked err = %v, want ErrPairingRevoked", err)
	}
}

func TestListPairingsFoldsJSONLState(t *testing.T) {
	ctx := context.Background()
	service := newTestBootstrapService(t, nil)
	first, err := service.CreatePairing(ctx, CreatePairingRequest{Role: PairingRoleOwner})
	if err != nil {
		t.Fatalf("create first: %v", err)
	}
	second, err := service.CreatePairing(ctx, CreatePairingRequest{Role: PairingRoleClient})
	if err != nil {
		t.Fatalf("create second: %v", err)
	}
	if _, err := service.ConsumePairing(ctx, ConsumePairingRequest{PairingToken: second.DisplayToken, InstanceID: "desktop-2"}); err != nil {
		t.Fatalf("consume second: %v", err)
	}

	records, err := service.ListPairings(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("records length = %d", len(records))
	}
	if records[0].TokenID != first.TokenID || !records[0].Active() {
		t.Fatalf("first record = %+v", records[0])
	}
	if records[1].TokenID != second.TokenID || records[1].ConsumedAt == nil {
		t.Fatalf("second record = %+v", records[1])
	}
}

func newTestBootstrapService(t *testing.T, random *bytes.Reader) *BootstrapCredentialService {
	t.Helper()
	var reader interface {
		Read([]byte) (int, error)
	}
	if random != nil {
		reader = random
	}
	service, err := NewBootstrapCredentialService(BootstrapCredentialConfig{
		Path:   filepath.Join(t.TempDir(), "pairing.jsonl"),
		Now:    fixedClock("2026-05-03T20:00:00Z"),
		Random: reader,
	})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	return service
}

func lineCount(t *testing.T, path string) int {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return strings.Count(string(data), "\n")
}

func fixedClock(value string) func() time.Time {
	t := mustTime(value)
	return func() time.Time { return t }
}

func mustTime(value string) time.Time {
	t, err := time.Parse(time.RFC3339, value)
	if err != nil {
		panic(err)
	}
	return t
}
