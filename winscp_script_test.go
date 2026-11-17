package main

import (
	"net/url"
	"reflect"
	"strings"
	"testing"
)

func TestWinscpQuote(t *testing.T) {
	cases := map[string]string{
		"plain":      `"plain"`,
		"a b":        `"a b"`,
		`say "hi"`:   `"say ""hi"""`,
		`C:\x y\z.t`: `"C:\x y\z.t"`,
	}
	for in, want := range cases {
		if got := winscpQuote(in); got != want {
			t.Errorf("winscpQuote(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestWinscpOpenURLEscaping(t *testing.T) {
	user, pass := "us@er:x", `p@ss:w/o%r!d^&" z`
	s := winscpSession{User: user, Pass: pass, Host: "example.org:2222"}
	got := winscpOpenURL(s)
	u, err := url.Parse(got)
	if err != nil {
		t.Fatal(err)
	}
	if u.Host != "example.org:2222" || u.Scheme != "sftp" {
		t.Errorf("bad host/scheme in %q", got)
	}
	if u.User.Username() != user {
		t.Errorf("user round trip: %q", u.User.Username())
	}
	if p, _ := u.User.Password(); p != pass {
		t.Errorf("password round trip: %q", p)
	}
	rest := got[len("sftp://"):strings.LastIndex(got, "@")]
	if strings.Count(rest, ":") != 1 || strings.ContainsAny(rest, "/ \"^") {
		t.Errorf("userinfo not escaped: %q", rest)
	}
}

func TestWinscpUploadScriptExact(t *testing.T) {
	s := winscpSession{User: "lab", Pass: "secret", Host: "h:22"}
	local := `C:\Documents and Settings\lab\shuttle\x.txt`
	got := winscpUploadScript(s, local, "/data/out/A/B/x.txt")
	want := []string{
		"option batch abort",
		"option confirm off",
		`open "sftp://lab:secret@h:22/"`,
		"option batch continue",
		`mkdir "/data"`,
		`mkdir "/data/out"`,
		`mkdir "/data/out/A"`,
		`mkdir "/data/out/A/B"`,
		"option batch abort",
		`put -nopermissions -nopreservetime "` + local + `" "/data/out/A/B/x.txt"`,
		"exit",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestWinscpHostKey(t *testing.T) {
	s := winscpSession{User: "u", Pass: "p", Host: "h", HostKey: "ssh-ed25519 255 SHA256:abc"}
	got := winscpCheckScript(s)[2]
	if !strings.HasSuffix(got, ` -hostkey="ssh-ed25519 255 SHA256:abc"`) {
		t.Errorf("open line: %s", got)
	}
	s.HostKey = ""
	if got := winscpCheckScript(s)[2]; strings.Contains(got, "-hostkey") {
		t.Errorf("unexpected -hostkey: %s", got)
	}
}

func TestWinscpRootFileNoMkdir(t *testing.T) {
	got := winscpUploadScript(winscpSession{User: "u", Pass: "p", Host: "h"}, "x.txt", "/x.txt")
	for _, l := range got {
		if strings.HasPrefix(l, "mkdir") || strings.Contains(l, "batch continue") {
			t.Errorf("unexpected line %q", l)
		}
	}
	if len(got) != 5 {
		t.Errorf("want 5 lines, got %d", len(got))
	}
}

func TestWinscpCheckScript(t *testing.T) {
	got := winscpCheckScript(winscpSession{User: "u", Pass: "p", Host: "h"})
	want := []string{"option batch abort", "option confirm off", `open "sftp://u:p@h/"`, "exit"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v", got)
	}
}

func TestWinscpPasswordOnlyInOpenLine(t *testing.T) {
	s := winscpSession{User: "u", Pass: "Zq9Zq9", Host: "h"}
	for i, l := range winscpUploadScript(s, "a", "/d/e/a") {
		if i != 2 && strings.Contains(l, s.Pass) {
			t.Errorf("password in line %d: %s", i, l)
		}
	}
}
