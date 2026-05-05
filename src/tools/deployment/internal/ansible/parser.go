// Package ansible runs ansible-playbook as a child process and
// projects its stdout onto a stream of typed task events.
//
// The verself_otel Ansible callback (Python, in
// src/host/ansible/callback_plugins/verself_otel.py) already
// emits a span per task. This package's parser is a complementary
// data path: it produces queryable rows in verself.ansible_task_events
// and a verself_deploy.ansible.task span family attributed to
// service.name=verself-deploy. The two views are independent so
// either side can be down without losing the other.
package ansible

import (
	"bufio"
	"io"
	"regexp"
	"strings"
	"time"
)

// TaskStatus is the per-host result of one task. Closed set; "rescued"
// and "ignored" are PLAY RECAP aggregates and never appear here.
type TaskStatus string

const (
	StatusOK          TaskStatus = "ok"
	StatusChanged     TaskStatus = "changed"
	StatusSkipped     TaskStatus = "skipped"
	StatusFailed      TaskStatus = "failed"
	StatusUnreachable TaskStatus = "unreachable"
)

// TaskEvent is one parsed row: a task ran on a host with a given
// status. Time is the wall clock at which the parser saw the result
// line; DurationMs is the gap between the task heading and the first
// result line (so on multi-host tasks only the first host's duration
// is the "true" task duration — subsequent hosts will read zero).
//
// run.go's recorder turns each TaskEvent into both a
// verself_deploy.ansible.task span (precise timing via the SDK) and
// a verself.ansible_task_events row (the queryable projection).
type TaskEvent struct {
	Time       time.Time
	Play       string
	Task       string
	Host       string
	Status     TaskStatus
	Item       string
	DurationMs int64
	Message    string
}

// PlayRecap aggregates the post-run PLAY RECAP totals per host.
type PlayRecap struct {
	Hosts map[string]RecapStats
}

// RecapStats is one host's columns in PLAY RECAP.
type RecapStats struct {
	OK          int
	Changed     int
	Unreachable int
	Failed      int
	Skipped     int
	Rescued     int
	Ignored     int
}

// ChangedTotal is the summed changed= counts across every host.
func (r *PlayRecap) ChangedTotal() int {
	total := 0
	for _, s := range r.Hosts {
		total += s.Changed
	}
	return total
}

// Parser turns an io.Reader of ansible-playbook stdout into a stream
// of TaskEvents. Wire it as the right half of an io.MultiWriter so
// the operator's terminal still sees the live output unmodified.
type Parser struct {
	events  chan TaskEvent
	now     func() time.Time
	scanner *bufio.Scanner

	currentPlay string
	currentTask string
	taskStart   time.Time
	taskEmitted bool

	recap   PlayRecap
	inRecap bool
}

// NewParser wraps r and exposes events on Events(). Run() drives the
// scan loop; the channel closes when r returns EOF or any read
// error. Recap() is valid only after Run() returns.
func NewParser(r io.Reader) *Parser {
	scanner := bufio.NewScanner(r)
	// Ansible's verbose JSON-on-failure output can be longer than
	// bufio's default 64 KiB; raise the cap to a safe ceiling.
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	return &Parser{
		events:  make(chan TaskEvent, 256),
		now:     time.Now,
		scanner: scanner,
		recap:   PlayRecap{Hosts: make(map[string]RecapStats)},
	}
}

// Events is the event channel. Drained by the recorder; never closed
// before Run returns.
func (p *Parser) Events() <-chan TaskEvent { return p.events }

// Recap returns the post-run PLAY RECAP aggregate. Only valid after
// Run returns.
func (p *Parser) Recap() PlayRecap { return p.recap }

// Run blocks until the underlying reader is exhausted, emitting one
// TaskEvent per result line and accumulating the recap. The returned
// error is the scanner error (or nil on clean EOF). The events
// channel is always closed before Run returns so range loops on the
// recorder side terminate.
func (p *Parser) Run() error {
	defer close(p.events)
	for p.scanner.Scan() {
		p.line(p.scanner.Text())
	}
	return p.scanner.Err()
}

// line dispatches a single stripped line to its handler. Ansible
// peppers stdout with ANSI color codes; we strip them before
// matching so the regexes don't have to anchor on escape sequences.
func (p *Parser) line(raw string) {
	clean := stripANSI(raw)
	if clean == "" {
		return
	}
	if recap := matchPlayRecapHeader(clean); recap {
		p.inRecap = true
		return
	}
	if p.inRecap {
		p.consumeRecapLine(clean)
		return
	}
	if play := matchPlay(clean); play != "" {
		p.currentPlay = play
		return
	}
	if task := matchTask(clean); task != "" {
		p.currentTask = task
		p.taskStart = p.now()
		p.taskEmitted = false
		return
	}
	if status, host, item, message, ok := matchResult(clean); ok {
		p.emit(status, host, item, message)
	}
}

func (p *Parser) emit(status TaskStatus, host, item, message string) {
	now := p.now()
	durMs := int64(0)
	if !p.taskEmitted && !p.taskStart.IsZero() {
		durMs = now.Sub(p.taskStart).Milliseconds()
	}
	p.taskEmitted = true
	p.events <- TaskEvent{
		Time:       now,
		Play:       p.currentPlay,
		Task:       p.currentTask,
		Host:       host,
		Status:     status,
		Item:       item,
		DurationMs: durMs,
		Message:    message,
	}
}

// consumeRecapLine parses one host's recap row. Format from Ansible
// 2.x onward:
//
//	<host> : ok=<n> changed=<n> unreachable=<n> failed=<n> skipped=<n> rescued=<n> ignored=<n>
//
// Whitespace varies; the regex handles tab/space mixtures.
func (p *Parser) consumeRecapLine(line string) {
	m := recapRowRe.FindStringSubmatch(line)
	if m == nil {
		return
	}
	host := strings.TrimSpace(m[1])
	stats := RecapStats{
		OK:          atoi(m[2]),
		Changed:     atoi(m[3]),
		Unreachable: atoi(m[4]),
		Failed:      atoi(m[5]),
		Skipped:     atoi(m[6]),
		Rescued:     atoi(m[7]),
		Ignored:     atoi(m[8]),
	}
	p.recap.Hosts[host] = stats
}

// stripANSI removes CSI escape sequences from Ansible's colorized
// stdout. The regex matches `ESC [ ... <terminator>`; non-CSI escapes
// (like the OSC color-reset Ansible emits between tasks) are rare
// and not worth a separate path.
var ansiRe = regexp.MustCompile(`\x1b\[[0-9;?]*[A-Za-z]`)

func stripANSI(s string) string { return ansiRe.ReplaceAllString(s, "") }

// playRe matches "PLAY [name] *****" — exact bracketing of a quoted
// play name, then a run of asterisks Ansible uses as the line filler.
var playRe = regexp.MustCompile(`^PLAY\s+\[(.*?)\]\s+\*+\s*$`)

func matchPlay(s string) string {
	m := playRe.FindStringSubmatch(s)
	if m == nil {
		return ""
	}
	return m[1]
}

// taskRe covers TASK and the handler variants. Ansible's HANDLER lines
// look like "RUNNING HANDLER [name] *****"; treat them as tasks because
// the result lines downstream use the same format.
var taskRe = regexp.MustCompile(`^(?:TASK|RUNNING\s+HANDLER)\s+\[(.*?)\]\s+\*+\s*$`)

func matchTask(s string) string {
	m := taskRe.FindStringSubmatch(s)
	if m == nil {
		return ""
	}
	return m[1]
}

// resultRe matches "ok: [host]" / "changed: [host]" / "skipping: [host]"
// / "failed: [host]" / "fatal: [host]: FAILED!" / "unreachable: [host]:".
// Trailing detail (=> {...}, => (item=...), error message) is captured
// loosely; the parser keeps it intact in Message and pulls Item out
// when it follows the "(item=...)" form. The optional ":" right
// after "]" is Ansible's separator for failed/unreachable payloads —
// loop tasks elide it.
var resultRe = regexp.MustCompile(`^(ok|changed|skipping|skipped|failed|fatal|unreachable):\s+\[([^\]]+)\]:?\s*(.*)$`)

// itemRe captures " => (item=<value>)" appearing on the same line as
// a result; Ansible emits it for looped tasks.
var itemRe = regexp.MustCompile(`=>\s+\(item=([^)]*)\)`)

func matchResult(s string) (status TaskStatus, host, item, message string, ok bool) {
	m := resultRe.FindStringSubmatch(s)
	if m == nil {
		return "", "", "", "", false
	}
	rawStatus := m[1]
	host = strings.SplitN(strings.TrimSpace(m[2]), " ", 2)[0]
	tail := m[3]
	switch rawStatus {
	case "ok":
		status = StatusOK
	case "changed":
		status = StatusChanged
	case "skipping", "skipped":
		status = StatusSkipped
	case "failed", "fatal":
		status = StatusFailed
	case "unreachable":
		status = StatusUnreachable
	default:
		return "", "", "", "", false
	}
	if im := itemRe.FindStringSubmatch(tail); im != nil {
		item = strings.TrimSpace(im[1])
	}
	if status == StatusFailed || status == StatusUnreachable {
		message = strings.TrimSpace(tail)
	}
	return status, host, item, message, true
}

// recapHeaderRe matches the start of the PLAY RECAP block; once we
// see it, every subsequent line is treated as a recap row (or
// trailing whitespace).
var recapHeaderRe = regexp.MustCompile(`^PLAY\s+RECAP\s+\*+\s*$`)

func matchPlayRecapHeader(s string) bool { return recapHeaderRe.MatchString(s) }

// recapRowRe captures one host's columns. Ansible aligns the names
// to a fixed width using spaces; the regex tolerates tab/space mixes.
var recapRowRe = regexp.MustCompile(`^(\S+)\s*:\s*ok=(\d+)\s+changed=(\d+)\s+unreachable=(\d+)\s+failed=(\d+)\s+skipped=(\d+)\s+rescued=(\d+)\s+ignored=(\d+)`)

func atoi(s string) int {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0
		}
		n = n*10 + int(r-'0')
	}
	return n
}
