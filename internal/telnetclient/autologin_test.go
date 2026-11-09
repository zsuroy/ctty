package telnetclient

import (
	"bytes"
	"errors"
	"testing"
	"time"

	"github.com/zsuroy/ctty/internal/credential"
	"github.com/zsuroy/ctty/internal/telnetconfig"
	"github.com/zsuroy/ctty/internal/telnetcred"
)

// recordWriter captures what AutoLogin sends to the connection.
type recordWriter struct {
	bytes.Buffer
}

func TestNewAutoLoginRules(t *testing.T) {
	cases := []struct {
		name     string
		user     string
		password string
		wantNil  bool
		wantN    int
	}{
		{"nothing configured", "", "", true, 0},
		{"user only", "der", "", false, 1}, // one rule accepting login:/username:
		{"password only (password-only device)", "", "s3cret", false, 1},
		{"both", "der", "s3cret", false, 2},
		{"whitespace only", "  ", "\t", true, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := NewAutoLogin(tc.user, tc.password)
			if tc.wantNil {
				if a != nil {
					t.Fatalf("NewAutoLogin = %+v want nil", a)
				}
				return
			}
			if a == nil {
				t.Fatal("NewAutoLogin = nil want rules")
			}
			if len(a.rules) != tc.wantN {
				t.Fatalf("rules=%d want %d", len(a.rules), tc.wantN)
			}
			if a.Pending() != tc.wantN || a.Fired() != 0 {
				t.Fatalf("pending=%d fired=%d want %d/0", a.Pending(), a.Fired(), tc.wantN)
			}
		})
	}
}

// TestAutoLoginRaspberryPiSequence replays the prompt sequence from issue
// #46 (a Raspberry Pi class dev board).
func TestAutoLoginRaspberryPiSequence(t *testing.T) {
	var w recordWriter
	a := NewAutoLogin("der", "raspberry")

	// "(none) login: der" arrives, echoed back with the answer.
	a.Observe([]byte("Raspbian GNU/Linux\n\nnone login: "), &w)
	if got := w.String(); got != "der\r\n" {
		t.Fatalf("after login prompt wrote %q want %q", got, "der\r\n")
	}

	// "Password:" (and the device's hint line) follows.
	a.Observe([]byte("Password: \r\nuse root password\r\n"), &w)
	if got := w.String(); got != "der\r\nraspberry\r\n" {
		t.Fatalf("after password prompt wrote %q want %q", got, "der\r\nraspberry\r\n")
	}

	// The shell prompt: nothing more to send.
	a.Observe([]byte("Welcome to Linux.\r\n~ # "), &w)
	if got := w.String(); got != "der\r\nraspberry\r\n" {
		t.Fatalf("after shell prompt wrote %q, want both answers only", got)
	}
	// Both rules fired, so the watcher is idle: it no longer inspects the
	// session output and the deadline becomes irrelevant.
	if a.Pending() != 0 || a.Fired() != 2 {
		t.Fatalf("pending=%d fired=%d want 0/2", a.Pending(), a.Fired())
	}
}

// TestAutoLoginUserNameSentOnceForEitherPrompt guards the single user rule:
// whichever wording the device uses, and even if it later shows the other
// one (e.g. a failed login retry), the user name is never sent twice.
func TestAutoLoginUserNameSentOnceForEitherPrompt(t *testing.T) {
	for _, first := range []string{"(none) login: ", "Username: "} {
		t.Run(first, func(t *testing.T) {
			var w recordWriter
			a := NewAutoLogin("pi", "")
			a.Observe([]byte(first), &w)
			if got := w.String(); got != "pi\r\n" {
				t.Fatalf("first prompt wrote %q want %q", got, "pi\r\n")
			}
			a.Observe([]byte("Username: "), &w)
			a.Observe([]byte("login: "), &w)
			if got := w.String(); got != "pi\r\n" {
				t.Fatalf("user name sent more than once: %q", got)
			}
			if a.Pending() != 0 {
				t.Fatalf("pending=%d want 0 (watcher should be idle)", a.Pending())
			}
		})
	}
}

func TestAutoLoginSendsEachPromptOnce(t *testing.T) {
	var w recordWriter
	a := NewAutoLogin("der", "s3cret")

	// A device that repeats the password prompt (or echoes the echoed
	// answer) must not be answered twice — retrying a wrong password in a
	// loop can lock the account.
	for range 3 {
		a.Observe([]byte("Password: "), &w)
	}
	if got := w.String(); got != "s3cret\r\n" {
		t.Fatalf("wrote %q after repeated prompts want a single answer", got)
	}

	// Same for the user prompt, including the echo of what was sent.
	a.Observe([]byte("der\r\nPassword: "), &w)
	if got := w.String(); got != "s3cret\r\n" {
		t.Fatalf("wrote %q after echo+prompt want a single answer", got)
	}
}

func TestAutoLoginPasswordOnlyDevice(t *testing.T) {
	var w recordWriter
	a := NewAutoLogin("", "s3cret")

	// Devices that only ask for a password must not receive a user line.
	a.Observe([]byte("Password: "), &w)
	if got := w.String(); got != "s3cret\r\n" {
		t.Fatalf("wrote %q want %q", got, "s3cret\r\n")
	}
	if a.Pending() != 0 {
		t.Fatalf("pending=%d want 0", a.Pending())
	}
}

func TestAutoLoginUsernamePrompt(t *testing.T) {
	var w recordWriter
	a := NewAutoLogin("admin", "s3cret")

	a.Observe([]byte("Username: "), &w)
	if got := w.String(); got != "admin\r\n" {
		t.Fatalf("wrote %q want %q", got, "admin\r\n")
	}
	a.Observe([]byte("Password: "), &w)
	if got := w.String(); got != "admin\r\ns3cret\r\n" {
		t.Fatalf("wrote %q want both answers", got)
	}
}

// TestAutoLoginMatchesPromptSplitAcrossReads guards the tail window: a
// prompt cut in half by the network read boundary must still match.
func TestAutoLoginMatchesPromptSplitAcrossReads(t *testing.T) {
	var w recordWriter
	a := NewAutoLogin("der", "s3cret")

	a.Observe([]byte("Pass"), &w)
	if w.Len() != 0 {
		t.Fatalf("fired on a partial prompt: %q", w.String())
	}
	a.Observe([]byte("word: "), &w)
	if got := w.String(); got != "s3cret\r\n" {
		t.Fatalf("wrote %q want %q after the prompt completed", got, "s3cret\r\n")
	}
}

func TestAutoLoginStopsAfterDeadline(t *testing.T) {
	var w recordWriter
	a := NewAutoLogin("der", "s3cret")
	a.deadline = time.Now().Add(-time.Second) // session already past the window

	a.Observe([]byte("login: Password: "), &w)
	if w.Len() != 0 {
		t.Fatalf("wrote %q after the deadline; must hand over to manual input", w.String())
	}
	if a.Pending() != 2 {
		t.Fatalf("pending=%d want 2 (nothing fired)", a.Pending())
	}
}

func TestAutoLoginNilAndNoRules(t *testing.T) {
	var w recordWriter
	var nilAuto *AutoLogin
	nilAuto.Observe([]byte("login: Password: "), &w) // must not panic
	if w.Len() != 0 {
		t.Fatalf("nil AutoLogin wrote %q", w.String())
	}

	empty := &AutoLogin{}
	empty.Observe([]byte("login: "), &w)
	if w.Len() != 0 {
		t.Fatalf("rule-less AutoLogin wrote %q", w.String())
	}
	empty.Observe(nil, &w) // empty chunk
}

func TestAutoLoginWriteErrorStops(t *testing.T) {
	a := NewAutoLogin("der", "s3cret")
	a.Observe([]byte("login: "), failingWriter{})
	if a.Pending() != 1 {
		t.Fatalf("pending=%d want 1 (user rule consumed, password intact)", a.Pending())
	}
	// Further output must not panic or fire after a failed write.
	a.Observe([]byte("Password: "), failingWriter{})
}

// failingWriter fails every write, like a connection that died.
type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("connection closed") }

// TestAutoLoginForResolvesVaultCredentials covers the credential resolution
// the connect paths use: user from the host record, password from the
// encrypted vault, nothing for hosts without either.
func TestAutoLoginForResolvesVaultCredentials(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("HOME", dir)
	credential.ResetStoreForTest()
	t.Cleanup(credential.ResetStoreForTest)

	if err := telnetcred.SetPassword("rpi", "s3cret"); err != nil {
		t.Fatalf("seed vault: %v", err)
	}
	// A same-named SSH entry must never leak into the telnet rules.
	if err := credential.SetPassword("other", "ssh-pass"); err != nil {
		t.Fatalf("seed ssh entry: %v", err)
	}

	full := AutoLoginFor(telnetconfig.TelnetHost{Name: "rpi", Host: "10.0.0.5", User: "der"})
	if full == nil || len(full.rules) != 2 {
		t.Fatalf("AutoLoginFor(rpi) rules=%d want 2 (user + vault password)", len(full.rules))
	}
	var w recordWriter
	full.Observe([]byte("login: "), &w)
	full.Observe([]byte("Password: "), &w)
	if got := w.String(); got != "der\r\ns3cret\r\n" {
		t.Fatalf("wrote %q want %q", got, "der\r\ns3cret\r\n")
	}

	// A host with a password but no user: password-only device.
	pwOnly := AutoLoginFor(telnetconfig.TelnetHost{Name: "rpi", Host: "10.0.0.6"})
	if pwOnly == nil || len(pwOnly.rules) != 1 {
		t.Fatalf("AutoLoginFor(password-only) rules=%d want 1", len(pwOnly.rules))
	}

	// No credentials anywhere: no auto-login at all.
	if got := AutoLoginFor(telnetconfig.TelnetHost{Name: "other", Host: "10.0.0.7"}); got != nil {
		t.Fatalf("AutoLoginFor(no credentials) = %+v want nil", got)
	}
	// Ad-hoc addresses (no name) resolve nothing.
	if got := AutoLoginFor(telnetconfig.TelnetHost{Host: "10.0.0.8"}); got != nil {
		t.Fatalf("AutoLoginFor(ad-hoc) = %+v want nil", got)
	}
}

// TestAutoLoginIgnoresTextThatOnlyContainsAPrompt is a regression test:
// matching any occurrence of "login:" / "password:" in the display stream
// used to send credentials into an ordinary shell, leaking them in the
// clear. Only a line that ends with the prompt qualifies.
func TestAutoLoginIgnoresTextThatOnlyContainsAPrompt(t *testing.T) {
	cases := []struct {
		name   string
		user   string
		chunks [][]byte
	}{
		{"Last login in motd", "pi", [][]byte{[]byte("Welcome\r\nLast login: Mon Nov 9 from 10.0.0.1\r\n")}},
		{"file content with password", "pi", [][]byte{[]byte("~ $ cat notes.txt\r\npassword: <redacted>\r\n")}},
		{"hint text after prompt", "", [][]byte{[]byte("Password: use root password\r\n")}},
		{"unrelated chunks spliced", "", [][]byte{[]byte("your pass"), []byte("word: appears in the docs")}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var w recordWriter
			a := NewAutoLogin(tc.user, "s3cret")
			for _, chunk := range tc.chunks {
				a.Observe(chunk, &w)
			}
			if w.Len() != 0 {
				t.Fatalf("sent %q on non-prompt text", w.String())
			}
		})
	}
}

// TestAutoLoginStillMatchesRealPrompts makes sure the stricter line-end rule
// did not break genuine prompts: a banner mentioning "Last login:" must not
// mask the real prompt, and an ANSI cursor sequence after a prompt is fine.
func TestAutoLoginStillMatchesRealPrompts(t *testing.T) {
	var w recordWriter
	a := NewAutoLogin("pi", "s3cret")
	a.Observe([]byte("Welcome\r\nLast login: Mon Nov 9 from 10.0.0.1\r\n(none) login: "), &w)
	if got := w.String(); got != "pi\r\n" {
		t.Fatalf("real login prompt after motd wrote %q want %q", got, "pi\r\n")
	}

	var w2 recordWriter
	b := NewAutoLogin("", "s3cret")
	b.Observe([]byte("Password: \x1b[?25h"), &w2)
	if got := w2.String(); got != "s3cret\r\n" {
		t.Fatalf("prompt followed by ANSI wrote %q want %q", got, "s3cret\r\n")
	}
}

// TestAutoLoginExtendedPromptVariants covers the additional console wordings
// accepted by NewAutoLogin. Each must still be a line-ending prompt, must
// send its answer exactly once, and must leave the watcher idle.
func TestAutoLoginExtendedPromptVariants(t *testing.T) {
	cases := []struct {
		name   string
		user   string
		pass   string
		prompt string
		want   string
	}{
		{"user", "pi", "", "user: ", "pi\r\n"},
		{"login as", "pi", "", "Login as: ", "pi\r\n"},
		{"enter user", "pi", "", "enter user: ", "pi\r\n"},
		{"classic login", "pi", "", "(none) login: ", "pi\r\n"},
		{"username", "pi", "", "Username: ", "pi\r\n"},
		{"passwd", "", "s3cret", "passwd: ", "s3cret\r\n"},
		{"enter PASSWD", "", "s3cret", "Enter PASSWD: ", "s3cret\r\n"},
		{"root's password", "", "s3cret", "root's password: ", "s3cret\r\n"},
		{"classic password", "", "s3cret", "Password: ", "s3cret\r\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var w recordWriter
			a := NewAutoLogin(tc.user, tc.pass)
			a.Observe([]byte(tc.prompt), &w)
			if got := w.String(); got != tc.want {
				t.Fatalf("prompt %q wrote %q want %q", tc.prompt, got, tc.want)
			}
			// A differently worded prompt later in the session must not
			// trigger a second answer.
			a.Observe([]byte("login: Password: "), &w)
			if got := w.String(); got != tc.want {
				t.Fatalf("answer sent twice: %q", got)
			}
			if a.Pending() != 0 {
				t.Fatalf("pending=%d want 0", a.Pending())
			}
		})
	}
}

// TestAutoLoginPromptVariantsRespectWordBoundary guards the short "user:"
// and "passwd:" variants: without a word boundary they would fire inside
// longer words, widening the prompt list would start leaking credentials
// into ordinary text.
func TestAutoLoginPromptVariantsRespectWordBoundary(t *testing.T) {
	cases := []struct {
		name  string
		user  string
		pass  string
		chunk string
	}{
		{"superuser is not a user prompt", "pi", "", "superuser: "},
		{"mypasswd is not a password prompt", "", "s3cret", "mypasswd: "},
		{"user with a value is a config line", "pi", "", "  user: root\r\n"},
		{"passwd with a value is a config line", "", "s3cret", "passwd: x\r\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var w recordWriter
			a := NewAutoLogin(tc.user, tc.pass)
			a.Observe([]byte(tc.chunk), &w)
			if w.Len() != 0 {
				t.Fatalf("sent %q on non-prompt text", w.String())
			}
		})
	}

	// The boundary check must not reject genuine prompts preceded by text.
	var w recordWriter
	a := NewAutoLogin("pi", "s3cret")
	a.Observe([]byte("Welcome\r\nroot's password: "), &w)
	if got := w.String(); got != "s3cret\r\n" {
		t.Fatalf("genuine prompt after text wrote %q want s3cret", got)
	}
}

// TestAutoLoginDoesNotSendBothAnswersFromOneTextBlock is a regression test
// for a block of text (e.g. a YAML file cat'ed inside the session) whose
// lines happen to end with "user:" and "password:". Both wordings are valid
// prompts on their own, but they arrived together, so this is not an
// interactive login: only the user name may be sent, and the password line
// must never be replayed by the tail window.
func TestAutoLoginDoesNotSendBothAnswersFromOneTextBlock(t *testing.T) {
	cases := []struct {
		name  string
		block string
	}{
		{"ends on the password line", "server:\r\n  user:\r\n  password:"},
		{"followed by a shell prompt", "server:\r\n  user:\r\n  password:\r\n$ "},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var w recordWriter
			a := NewAutoLogin("pi", "s3cret")
			a.Observe([]byte(tc.block), &w)
			if got := w.String(); got != "pi\r\n" {
				t.Fatalf("wrote %q want only the user name", got)
			}
			// Later output must not replay the password line from the block.
			a.Observe([]byte("~ $ "), &w)
			a.Observe([]byte("anything else\r\n"), &w)
			if got := w.String(); got != "pi\r\n" {
				t.Fatalf("password leaked from a static text block: %q", got)
			}
			if a.Pending() != 1 {
				t.Fatalf("pending=%d want 1 (password still available)", a.Pending())
			}
		})
	}
}

// TestAutoLoginStillAnswersInteractivePromptsAcrossReads makes sure the
// single-read guard did not break a normal device: it asks for the user
// name, echoes the answer, then asks for the password in a later read.
func TestAutoLoginStillAnswersInteractivePromptsAcrossReads(t *testing.T) {
	var w recordWriter
	a := NewAutoLogin("pi", "s3cret")

	a.Observe([]byte("(none) login: "), &w)
	a.Observe([]byte("pi\r\nPassword: "), &w)
	if got := w.String(); got != "pi\r\ns3cret\r\n" {
		t.Fatalf("interactive login wrote %q want both answers", got)
	}
	if a.Pending() != 0 {
		t.Fatalf("pending=%d want 0", a.Pending())
	}
}
