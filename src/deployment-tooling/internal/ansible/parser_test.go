package ansible

import (
	"strings"
	"testing"
)

// fixture is a representative ansible-playbook stdout for a layer
// playbook with one play, two tasks, two hosts, and a recap. Output
// is colorless because we strip ANSI before matching.
const fixture = `
PLAY [Substrate L1 OS] *********************************************************

TASK [Gathering Facts] *********************************************************
ok: [forge-01]

TASK [bootstrap : Install Python] **********************************************
changed: [forge-01]

TASK [bootstrap : Reload systemd daemons] **************************************
ok: [forge-01]

TASK [tune : Loop over knobs] **************************************************
changed: [forge-01] => (item=net.core.somaxconn)
changed: [forge-01] => (item=vm.swappiness)

PLAY RECAP *********************************************************************
forge-01                   : ok=3    changed=2    unreachable=0    failed=0    skipped=0    rescued=0    ignored=0
`

func TestParser_BasicFlow(t *testing.T) {
	p := NewParser(strings.NewReader(fixture))
	var events []TaskEvent
	done := make(chan struct{})
	go func() {
		defer close(done)
		for ev := range p.Events() {
			events = append(events, ev)
		}
	}()
	if err := p.Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}
	<-done

	if got, want := len(events), 5; got != want {
		t.Fatalf("event count: got %d, want %d (%+v)", got, want, events)
	}
	if got, want := events[0].Status, StatusOK; got != want {
		t.Errorf("event[0].Status: got %q, want %q", got, want)
	}
	if got, want := events[1].Status, StatusChanged; got != want {
		t.Errorf("event[1].Status: got %q, want %q", got, want)
	}
	if got, want := events[1].Task, "bootstrap : Install Python"; got != want {
		t.Errorf("event[1].Task: got %q, want %q", got, want)
	}
	if got, want := events[3].Item, "net.core.somaxconn"; got != want {
		t.Errorf("event[3].Item: got %q, want %q", got, want)
	}

	recap := p.Recap()
	stats, ok := recap.Hosts["forge-01"]
	if !ok {
		t.Fatalf("recap missing forge-01: %+v", recap)
	}
	if got, want := stats.Changed, 2; got != want {
		t.Errorf("recap changed: got %d, want %d", got, want)
	}
	if got, want := recap.ChangedTotal(), 2; got != want {
		t.Errorf("ChangedTotal: got %d, want %d", got, want)
	}
}

func TestParser_FailedTaskCarriesMessage(t *testing.T) {
	const failureFixture = `
PLAY [bad] **********************************************************************

TASK [break things] *************************************************************
fatal: [forge-01]: FAILED! => {"msg": "boom"}

PLAY RECAP **********************************************************************
forge-01                   : ok=0    changed=0    unreachable=0    failed=1    skipped=0    rescued=0    ignored=0
`
	p := NewParser(strings.NewReader(failureFixture))
	var events []TaskEvent
	done := make(chan struct{})
	go func() {
		defer close(done)
		for ev := range p.Events() {
			events = append(events, ev)
		}
	}()
	if err := p.Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}
	<-done
	if got, want := len(events), 1; got != want {
		t.Fatalf("event count: got %d, want %d", got, want)
	}
	if events[0].Status != StatusFailed {
		t.Errorf("status: got %q, want %q", events[0].Status, StatusFailed)
	}
	if !strings.Contains(events[0].Message, "boom") {
		t.Errorf("message did not capture failure payload: %q", events[0].Message)
	}
}

func TestStripANSI(t *testing.T) {
	input := "\x1b[1;31mfailed:\x1b[0m [forge-01]"
	want := "failed: [forge-01]"
	if got := stripANSI(input); got != want {
		t.Errorf("stripANSI: got %q, want %q", got, want)
	}
}
