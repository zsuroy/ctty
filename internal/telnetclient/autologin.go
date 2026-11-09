package telnetclient

import (
	"io"
	"strings"
	"time"
)

// autoLoginTimeout bounds how long AutoLogin watches for prompts after the
// session starts. Devices that prompt later than this — or never — simply
// get manual input; nothing is ever blocked waiting for a match.
const autoLoginTimeout = 10 * time.Second

// autoLoginTailWindow bounds the unfinished last line carried between reads
// so a prompt split across two reads still matches. Only that single line is
// kept — never a slice of earlier, already-complete lines.
const autoLoginTailWindow = 64

// AutoLogin answers telnet login prompts automatically.
//
// Telnet sessions to lab gear and dev boards typically ask for a user name
// and a password on the console (e.g. a Raspberry Pi board prints
// "(none) login:" then "Password:"). AutoLogin watches the session's display
// stream and answers each configured prompt exactly once, then gets out of
// the way:
//
//   - every rule fires at most one time — a wrong password must not retry in
//     a loop and lock the account;
//   - a session deadline stops the watcher when the device never prompts, so
//     manual input is never blocked;
//   - a rule list with nothing to send (no user, no stored password) is nil,
//     so sessions without credentials behave exactly as before.
//
// Telnet is cleartext: this saves typing, it does not add security.
type AutoLogin struct {
	rules    []autoLoginRule
	deadline time.Time
	tail     []byte
	fired    int
}

// autoLoginRule is one "wait for any of these prompts, send that" pair.
// Several prompts share a single rule so the answer is sent exactly once,
// whichever wording the device happens to use.
type autoLoginRule struct {
	expects []string // lowercase prompt endings, e.g. "login:", "passwd:"
	send    string
	sent    bool
}

// NewAutoLogin builds the rule set for a session: the user name is sent at
// common login wordings and the password at common password wordings
// (a rule is omitted entirely when there is nothing to send for it, so
// password-only and user-only devices both work). Returns nil when neither
// credential is configured.
//
// Every variant still has to end the line and start at a word boundary;
// adding a wording widens coverage but never relaxes the "this is a prompt,
// not a sentence" test.
func NewAutoLogin(user, password string) *AutoLogin {
	user = strings.TrimSpace(user)
	password = strings.TrimSpace(password)
	if user == "" && password == "" {
		return nil
	}
	a := &AutoLogin{deadline: time.Now().Add(autoLoginTimeout)}
	if user != "" {
		// One rule for all "who are you" wordings so the user name is
		// sent only once even if the device uses two of them (e.g. a
		// failed login retry).
		a.rules = append(a.rules, autoLoginRule{
			expects: []string{"login:", "username:", "user:", "login as:"},
			send:    user,
		})
	}
	if password != "" {
		a.rules = append(a.rules, autoLoginRule{
			expects: []string{"password:", "passwd:"},
			send:    password,
		})
	}
	return a
}

// Fired reports how many rules have already sent their value.
func (a *AutoLogin) Fired() int {
	if a == nil {
		return 0
	}
	return a.fired
}

// Pending reports how many rules have not fired yet.
func (a *AutoLogin) Pending() int {
	if a == nil {
		return 0
	}
	return len(a.rules) - a.fired
}

// Observe feeds session display output to the matcher and writes any due
// answers (each terminated by CRLF) to w — the connection. A rule fires only
// when a display *line ends* with its prompt; merely containing "login:" or
// "password:" (a "Last login:" motd, a config file, a sentence) is not a
// prompt and must never leak a credential into the session; the prompt must
// also start at a word boundary. Rules fire at most once, and only before
// the session deadline.
//
// A real device asks for the user name, waits for the answer, then asks for
// the password — those prompts arrive in different reads. If both show up in
// one window, it is a block of text (a config file, a motd), not an
// interactive login: only the user name is answered and the password waits
// for a genuinely later prompt. Safe to call from the session's read pump;
// net.Conn writes are safe for concurrent use.
func (a *AutoLogin) Observe(chunk []byte, w io.Writer) {
	if a == nil || len(chunk) == 0 || len(a.rules) == 0 {
		return
	}
	if a.Pending() == 0 || time.Now().After(a.deadline) {
		return
	}

	window := make([]byte, 0, len(a.tail)+len(chunk))
	window = append(window, a.tail...)
	window = append(window, chunk...)
	lower := strings.ToLower(string(window))
	lines := strings.Split(strings.ReplaceAll(lower, "\r", "\n"), "\n")

	// NewAutoLogin emits the user rule first and the password rule second,
	// so with two rules index 1 is the password rule.
	userFiredNow := false
	skipTail := false
	for i := range a.rules {
		r := &a.rules[i]
		if r.sent {
			continue
		}
		matched := false
		for _, line := range lines {
			if lineHasPrompt(line, r.expects) {
				matched = true
				break
			}
		}
		if !matched {
			continue
		}

		// Both prompts in a single read is never an interactive login.
		// Hold the password back; clearing the tail also stops this same
		// text (e.g. a final "  password:" line with no newline) from
		// being replayed as a fresh prompt on the next call.
		if len(a.rules) > 1 && i == 1 && userFiredNow {
			skipTail = true
			continue
		}

		r.sent = true
		a.fired++
		if len(a.rules) > 1 && i == 0 {
			userFiredNow = true
		}
		if _, err := io.WriteString(w, r.send+"\r\n"); err != nil {
			return
		}
	}

	if skipTail {
		a.tail = a.tail[:0]
		return
	}

	// Carry only the unfinished last line: a prompt cut by a read boundary
	// still matches on the next call, but two unrelated lines can no longer
	// be stitched into a fake "password:" by the tail window.
	last := len(window)
	for last > 0 && window[last-1] != '\r' && window[last-1] != '\n' {
		last--
	}
	tail := window[last:]
	if len(tail) > autoLoginTailWindow {
		tail = tail[len(tail)-autoLoginTailWindow:]
	}
	a.tail = append(a.tail[:0], tail...)
}

// lineHasPrompt reports whether a display line ends with any of the rule's
// accepted prompts.
func lineHasPrompt(line string, expects []string) bool {
	for _, expect := range expects {
		if promptAtLineEnd(line, expect) {
			return true
		}
	}
	return false
}

// promptAtLineEnd reports whether line ends with expect, allowing only
// trailing blanks and ANSI escape sequences after the prompt. Ordinary text
// after the substring means it was part of a sentence, not a prompt — e.g.
// "Last login: Mon Nov 9" or "password: <redacted>".
//
// The match must also start at a word boundary: short variants such as
// "user:" would otherwise fire inside "superuser:" or "mypasswd:".
func promptAtLineEnd(line, expect string) bool {
	idx := strings.LastIndex(line, expect)
	if idx < 0 {
		return false
	}
	if idx > 0 && isWordByte(line[idx-1]) {
		return false
	}
	rest := line[idx+len(expect):]
	for i := 0; i < len(rest); {
		switch {
		case rest[i] == ' ' || rest[i] == '\t':
			i++
		case rest[i] == 0x1b: // ESC sequence: skip through its final byte
			i++
			if i < len(rest) && rest[i] == '[' {
				i++
			}
			for i < len(rest) && (rest[i] < 0x40 || rest[i] > 0x7e) {
				i++
			}
			if i < len(rest) {
				i++
			}
		default:
			return false
		}
	}
	return true
}

// isWordByte reports whether b is part of a word (letter, digit or
// underscore). A prompt variant must not be glued to one, otherwise
// "superuser:" would satisfy the short "user:" variant.
func isWordByte(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' ||
		b >= '0' && b <= '9' || b == '_'
}
