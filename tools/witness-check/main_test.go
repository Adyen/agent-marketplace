package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func stream(t *testing.T, events ...event) string {
	t.Helper()
	var b strings.Builder
	for _, e := range events {
		data, err := json.Marshal(e)
		if err != nil {
			t.Fatal(err)
		}
		b.Write(data)
		b.WriteByte('\n')
	}
	return b.String()
}

func guard(action string, witnesses ...string) []event {
	events := []event{{Action: "run", Package: guardPackage, Test: guardTest}}
	for _, name := range witnesses {
		events = append(events, event{Action: "output", Package: guardPackage, Test: guardTest, Output: "    docs_contract_test.go:1: " + witnessPrefix + name + "\n"})
	}
	return append(events, event{Action: action, Package: guardPackage, Test: guardTest})
}

func TestCheckRequiresEveryWitnessToPass(t *testing.T) {
	passing := append(guard("pass", "TestA", "TestB", "TestA"),
		event{Action: "pass", Package: guardPackage, Test: "TestA"},
		event{Action: "pass", Package: guardPackage, Test: "TestB"},
		event{Action: "output", Package: guardPackage, Output: "PASS\n"},
		event{Action: "output", Package: guardPackage, Output: "ok  \tpkg\t1.0s\n"},
		event{Action: "pass", Package: guardPackage},
	)
	var out strings.Builder
	if err := check(strings.NewReader("make[1]: Entering directory\n"+stream(t, passing...)), &out, nil); err != nil {
		t.Fatalf("passing run: %v", err)
	}
	for _, want := range []string{"make[1]: Entering directory", "ok  \tpkg", "2 behavior witnesses ran and passed"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("output missing %q:\n%s", want, out.String())
		}
	}
	if strings.Contains(out.String(), witnessPrefix) || strings.Contains(out.String(), "PASS\n") {
		t.Fatalf("passing test output was relayed:\n%s", out.String())
	}

	for name, events := range map[string][]event{
		"skipped witness": append(guard("pass", "TestA"),
			event{Action: "output", Package: guardPackage, Test: "TestA", Output: "    a_test.go:9: docker unavailable\n"},
			event{Action: "skip", Package: guardPackage, Test: "TestA"}),
		"missing witness": guard("pass", "TestA"),
		"guard skipped":   guard("skip", "TestA"),
		"guard missing":   {{Action: "pass", Package: guardPackage, Test: "TestA"}},
		"no witnesses":    guard("pass"),
		"package failed": append(guard("pass", "TestA"),
			event{Action: "pass", Package: guardPackage, Test: "TestA"},
			event{Action: "output", Package: "other", Test: "TestBroken", Output: "broken detail\n"},
			event{Action: "fail", Package: "other", Test: "TestBroken"},
			event{Action: "fail", Package: "other"}),
	} {
		t.Run(name, func(t *testing.T) {
			var out strings.Builder
			err := check(strings.NewReader(stream(t, events...)), &out, nil)
			if err == nil {
				t.Fatal("check passed")
			}
			if name == "skipped witness" && (!strings.Contains(err.Error(), "TestA: skip") || !strings.Contains(err.Error(), "docker unavailable")) {
				t.Fatalf("error = %v", err)
			}
			if name == "missing witness" && !strings.Contains(err.Error(), "TestA: never ran") {
				t.Fatalf("error = %v", err)
			}
			if name == "package failed" && !strings.Contains(out.String(), "broken detail") {
				t.Fatalf("failing test output not relayed:\n%s", out.String())
			}
		})
	}
	var relayed strings.Builder
	unrelated := stream(t, append(passing[:len(passing):len(passing)], event{Action: "skip", Package: "other", Test: "TestRoutine"})...)
	if err := check(strings.NewReader(`{"level":"info","msg":"setup"}`+"\n"+unrelated), &relayed, nil); err != nil || !strings.Contains(relayed.String(), `{"level":"info","msg":"setup"}`) {
		t.Fatalf("non-event JSON was not relayed (err %v):\n%s", err, relayed.String())
	}
	if err := check(strings.NewReader("ok  \tpkg\t1.0s\n"), &strings.Builder{}, nil); err == nil || !strings.Contains(err.Error(), "-json") {
		t.Fatalf("plain output error = %v", err)
	}
}

func TestCheckFailsWhenAPackageNeverFinishes(t *testing.T) {
	events := append([]event{{Action: "start", Package: guardPackage}, {Action: "start", Package: "killed"}}, guard("pass", "TestA")...)
	events = append(events, event{Action: "pass", Package: guardPackage, Test: "TestA"}, event{Action: "pass", Package: guardPackage})
	err := check(strings.NewReader(stream(t, events...)), &strings.Builder{}, nil)
	if err == nil || !strings.Contains(err.Error(), "killed") {
		t.Fatalf("truncated package stream error = %v", err)
	}
}

func TestCheckRequireModeNeedsEveryNamedTest(t *testing.T) {
	run := func(events ...event) error {
		t.Helper()
		return check(strings.NewReader(stream(t, events...)), &strings.Builder{}, []string{"TestUpgrade", "TestLocks"})
	}
	pkg := "github.com/Adyen/agent-marketplace/e2e"
	start := event{Action: "start", Package: pkg}
	done := event{Action: "pass", Package: pkg}
	if err := run(start, event{Action: "pass", Package: pkg, Test: "TestUpgrade"}, event{Action: "pass", Package: pkg, Test: "TestLocks"}, done); err != nil {
		t.Fatalf("both required tests passed: %v", err)
	}
	if err := run(start, event{Action: "pass", Package: pkg, Test: "TestUpgrade"}, done); err == nil || !strings.Contains(err.Error(), "TestLocks: never ran") {
		t.Fatalf("renamed or filtered test error = %v", err)
	}
	if err := run(start, event{Action: "pass", Package: pkg, Test: "TestUpgrade"}, event{Action: "skip", Package: pkg, Test: "TestLocks"}, done); err == nil || !strings.Contains(err.Error(), "TestLocks: skip") {
		t.Fatalf("skipped required test error = %v", err)
	}
}
