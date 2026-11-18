package main

import (
	"flag"
	"io/ioutil"
	"strings"
	"testing"
	"time"
)

func TestUnsetPlaceholder(t *testing.T) {
	open, close := "{"+"{", "}"+"}"
	cases := map[string]string{
		"":                              "",
		open + " hostkey " + close:      "",
		"  " + open + "hostkey" + close: "",
		"SHA256:abc":                    "SHA256:abc",
		"ssh-ed25519 255 SHA256:abc":    "ssh-ed25519 255 SHA256:abc",
		open + " half":                  open + " half",
	}
	for in, want := range cases {
		if got := unsetPlaceholder(in); got != want {
			t.Errorf("unsetPlaceholder(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestIsTrue(t *testing.T) {
	for _, v := range []string{"1", "true", "TRUE", " t ", "True"} {
		if !isTrue(v) {
			t.Errorf("isTrue(%q) = false", v)
		}
	}
	for _, v := range []string{"", "0", "false", "yes", "{" + "{ insecure }" + "}"} {
		if isTrue(v) {
			t.Errorf("isTrue(%q) = true", v)
		}
	}
}

func TestParseDst(t *testing.T) {
	cases := []struct {
		raw, tType, wantScheme, wantHost, wantPath string
		err                                        bool
	}{
		{"myserver:22/data", "sftp", "ssh", "myserver:22", "/data", false},
		{"1.2.3.4:220", "sftp", "ssh", "1.2.3.4:220", "", false},
		{"myserver/data", "sftp", "ssh", "myserver:22", "/data", false},
		{"ssh://h/p", "sftp", "ssh", "h:22", "/p", false},
		{"SSH://h:2200/p", "sftp", "ssh", "h:2200", "/p", false},
		{"sftp://h:2222/p", "sftp", "ssh", "h:2222", "/p", false},
		{"https://h/x", "webdav", "https", "h", "/x", false},
		{"http://h:8080/x", "webdav", "http", "h:8080", "/x", false},
		{"h/x", "webdav", "", "", "", true},
		{"h:80/x", "webdav", "", "", "", true},
		{"ftp://h/x", "webdav", "", "", "", true},
		{"", "webdav", "", "", "", true},
		{"", "sftp", "", "", "", true},
		{"ssh:///p", "sftp", "", "", "", true},
		{"http:///p", "webdav", "", "", "", true},
		{"h", "other", "", "", "", true},
	}
	for _, c := range cases {
		u, err := parseDst(c.raw, c.tType)
		if (err != nil) != c.err {
			t.Errorf("parseDst(%q, %s) err = %v", c.raw, c.tType, err)
			continue
		}
		if c.err {
			continue
		}
		if u.Scheme != c.wantScheme || u.Host != c.wantHost || u.Path != c.wantPath {
			t.Errorf("parseDst(%q, %s) = %s %s %s", c.raw, c.tType, u.Scheme, u.Host, u.Path)
		}
	}
}

func parseTestArgs(argv ...string) (Args, error) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	fs.SetOutput(ioutil.Discard)
	return parseArgs(fs, argv)
}

func TestParseArgs(t *testing.T) {
	base := []string{"-src", "/tmp/src", "-name", "n", "-dst", "myserver:22/data", "-duration", "30", "-type", "file", "-transfer", "sftp"}
	with := func(extra ...string) []string { return append(append([]string{}, base...), extra...) }

	a, err := parseTestArgs(base...)
	if err != nil {
		t.Fatal(err)
	}
	if a.dst.Host != "myserver:22" || a.duration != 30*time.Second || a.dstConverter.Host != a.dst.Host {
		t.Errorf("args = %+v", a)
	}
	if !strings.Contains(a.LogString(), "name=n") {
		t.Error("LogString lacks the name")
	}

	bad := map[string][]string{
		"duration 0":               with("-duration", "0"),
		"duration negative":        with("-duration", "-5"),
		"duration text":            with("-duration", "x"),
		"bad type":                 with("-type", "weird"),
		"bad transfer":             with("-transfer", "ftp"),
		"empty src":                with("-src", ""),
		"empty dst":                with("-dst", ""),
		"webdav without scheme":    with("-transfer", "webdav", "-dst", "host/x"),
		"bad regex":                with("-type", "flat_tar", "-commonRegex", "(["),
		"preconvert without pub":   with("-preconvert", "1", "-dstConverter", "h:22/c"),
		"preconvert empty dstconv": with("-preconvert", "1", "-public", "http://p/", "-dstConverter", ""),
		"unknown flag":             with("-nope"),
	}
	for name, argv := range bad {
		if _, err := parseTestArgs(argv...); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
	if _, err := parseTestArgs(with("-type", "flat_tar", "-commonRegex", "([")...); err == nil || !strings.Contains(err.Error(), "invalid -commonRegex") {
		t.Errorf("regex error = %v", err)
	}

	a, err = parseTestArgs(with("-preconvert", "true", "-public", "http://p/", "-dstConverter", "ssh://c:2/x")...)
	if err != nil || !a.preconvert || a.dstConverter.Host != "c:2" {
		t.Errorf("preconvert args = %+v, %v", a, err)
	}
	a, err = parseTestArgs("-version")
	if err != nil || !a.version {
		t.Errorf("-version = %+v, %v", a, err)
	}
}
