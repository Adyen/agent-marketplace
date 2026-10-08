package localapi

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/internal/session"
)

func serveHarness(t *testing.T, api *API, bootstrapFields map[string]string) {
	t.Helper()
	bootstrap := os.NewFile(3, "bootstrap")
	if bootstrap == nil {
		t.Fatal("fd 3 bootstrap pipe is required")
	}
	defer func() { _ = bootstrap.Close() }()

	loopback, err := api.ListenLoopback(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = loopback.Close() }()

	bootstrapFields["url"] = loopback.URL
	bootstrapFields["credential"] = api.Credential()
	line, err := json.Marshal(bootstrapFields)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bootstrap.Write(append(line, '\n')); err != nil {
		t.Fatal(err)
	}

	deadline := time.After(2 * time.Minute)
	closed := make(chan struct{})
	go func() {
		defer close(closed)
		buf := make([]byte, 1)
		for {
			if _, err := bootstrap.Read(buf); err != nil {
				return
			}
		}
	}()
	select {
	case <-closed:
	case <-deadline:
	}
}

func TestLocalAPIHarness(t *testing.T) {
	if os.Getenv("AM_LOCALAPI_E2E") != "1" {
		t.Skip("real-core harness; enabled only by the Playwright E2E driver")
	}
	ws, home := newFixture(t)
	api, err := New(context.Background(), ws, home, session.RuntimeLifecycle(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer api.Close()
	serveHarness(t, api, map[string]string{"repo": ws.RepoRoot()})
}

func TestLocalAPIUserHarness(t *testing.T) {
	if os.Getenv("AM_LOCALAPI_USER_E2E") != "1" {
		t.Skip("user-scope harness; enabled only by the Playwright E2E driver")
	}
	home, _ := newUserAPIFixture(t, true)
	api, err := NewUser(context.Background(), home, session.RuntimeLifecycle(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer api.Close()
	serveHarness(t, api, map[string]string{"home": home})
}
