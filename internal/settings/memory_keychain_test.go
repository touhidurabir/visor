package settings

import (
	"errors"
	"sync"
	"testing"

	"github.com/zalando/go-keyring"
)

func TestMemoryKeychain(t *testing.T) {
	var k Keychain = &MemoryKeychain{}

	if _, err := k.Get("svc", "acct"); !errors.Is(err, keyring.ErrNotFound) {
		t.Fatalf("Get on empty keychain = %v, want ErrNotFound", err)
	}
	if err := k.Delete("svc", "acct"); !errors.Is(err, keyring.ErrNotFound) {
		t.Fatalf("Delete of missing = %v, want ErrNotFound", err)
	}
	if err := k.Set("svc", "acct", "secret"); err != nil {
		t.Fatal(err)
	}
	if got, err := k.Get("svc", "acct"); err != nil || got != "secret" {
		t.Fatalf("Get = %q, %v", got, err)
	}
	if _, err := k.Get("svc", "other"); !errors.Is(err, keyring.ErrNotFound) {
		t.Fatalf("Get other account = %v, want ErrNotFound", err)
	}
	if err := k.Delete("svc", "acct"); err != nil {
		t.Fatal(err)
	}
	if _, err := k.Get("svc", "acct"); !errors.Is(err, keyring.ErrNotFound) {
		t.Fatalf("Get after Delete = %v, want ErrNotFound", err)
	}
}

func TestMemoryKeychainWorksWithPasswordHelpers(t *testing.T) {
	k := &MemoryKeychain{}
	if pw, err := Password(k, "/c"); err != nil || pw != "" {
		t.Fatalf("Password = %q, %v", pw, err)
	}
	if err := SetPassword(k, "/c", "pw"); err != nil {
		t.Fatal(err)
	}
	if pw, _ := Password(k, "/c"); pw != "pw" {
		t.Fatalf("Password = %q", pw)
	}
	if err := SetPassword(k, "/c", ""); err != nil {
		t.Fatal(err)
	}
	if pw, _ := Password(k, "/c"); pw != "" {
		t.Fatalf("Password after removal = %q", pw)
	}
}

func TestMemoryKeychainConcurrent(t *testing.T) {
	k := &MemoryKeychain{}
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = k.Set("s", "a", "x")
			_, _ = k.Get("s", "a")
			_ = k.Delete("s", "a")
		}()
	}
	wg.Wait()
}
