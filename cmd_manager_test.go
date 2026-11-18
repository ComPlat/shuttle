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
		{"https://cam.example.org", "chemassistent", "https", "cam.example.org", "/api/jobs/trigger", false},
		{"https://cam.example.org/", "chemassistent", "https", "cam.example.org", "/api/jobs/trigger", false},
		{"https://h/custom/trigger", "chemassistent", "https", "h", "/custom/trigger", false},
		{"https://h/custom/trigger/", "chemassistent", "https", "h", "/custom/trigger", false},
		{"http://h:8000", "chemassistent", "http", "h:8000", "/api/jobs/trigger", false},
		{"cam.example.org", "chemassistent", "", "", "", true},
		{"ftp://h", "chemassistent", "", "", "", true},
		{"http:///p", "chemassistent", "", "", "", true},
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

func TestParseArgsChemAssistent(t *testing.T) {
	base := []string{"-src", "/tmp/src", "-name", "n", "-dst", "https://cam.example.org", "-duration", "30", "-type", "file", "-transfer", "chemassistent", "-user", "ignored"}
	with := func(extra ...string) []string { return append(append([]string{}, base...), extra...) }
	const key = "cam_0123456789abcdef"

	a, err := parseTestArgs(with("-pass", key)...)
	if err != nil {
		t.Fatal(err)
	}
	if a.dst.String() != "https://cam.example.org/api/jobs/trigger" || a.pass != key || a.tType != "chemassistent" {
		t.Errorf("args = %+v", a)
	}
	if a.maxUploadMB != chemAssistentDefaultMaxMB {
		t.Errorf("maxUploadMB = %d, want default", a.maxUploadMB)
	}
	ls := a.LogString()
	if strings.Contains(ls, key) || strings.Contains(ls, "ignored") {
		t.Errorf("LogString leaks the key or user: %s", ls)
	}
	if !strings.Contains(ls, "transfer=chemassistent") || !strings.Contains(ls, "dst=https://cam.example.org/api/jobs/trigger") || !strings.Contains(ls, "cam_0123...") {
		t.Errorf("LogString = %s", ls)
	}
	short := a
	short.pass = "cam_short"
	if ls := short.LogString(); strings.Contains(ls, "cam_short") || strings.Contains(ls, "key=") {
		t.Errorf("LogString of a short key = %s", ls)
	}

	// a key without the cam_ prefix is accepted (only a warning)
	if a, err = parseTestArgs(with("-pass", "other-key-format")...); err != nil || a.pass != "other-key-format" {
		t.Errorf("key without prefix: %+v, %v", a, err)
	}
	if a, err = parseTestArgs(with("-pass", key, "-maxUpload", "7")...); err != nil || a.maxUploadMB != 7 {
		t.Errorf("maxUpload 7: %+v, %v", a, err)
	}

	bad := map[string][]string{
		"no pass":          with(),
		"empty pass":       with("-pass", ""),
		"placeholder pass": with("-pass", "{"+"{ password }"+"}"),
		"preconvert":       with("-pass", key, "-preconvert", "1", "-public", "http://p/", "-dstConverter", "https://c/x"),
		"maxUpload 0":      with("-pass", key, "-maxUpload", "0"),
		"maxUpload neg":    with("-pass", key, "-maxUpload", "-3"),
		"maxUpload text":   with("-pass", key, "-maxUpload", "x"),
		"dst no scheme":    with("-pass", key, "-dst", "cam.example.org"),
		"dst ftp":          with("-pass", key, "-dst", "ftp://h"),
	}
	for name, argv := range bad {
		if _, err := parseTestArgs(argv...); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
	if _, err := parseTestArgs(with("-pass", key, "-preconvert", "1")...); err == nil || !strings.Contains(err.Error(), "-preconvert is not supported with -transfer chemassistent") {
		t.Errorf("preconvert error = %v", err)
	}
	if _, err := parseTestArgs(with()...); err == nil || !strings.Contains(err.Error(), "-pass must hold the ChemAssistentManager API key") {
		t.Errorf("pass error = %v", err)
	}

	// other transfer types ignore -maxUpload but still carry the default
	if a, err = parseTestArgs("-src", "/s", "-name", "n", "-dst", "h:22/d", "-duration", "5", "-type", "file", "-transfer", "sftp"); err != nil || a.maxUploadMB != chemAssistentDefaultMaxMB {
		t.Errorf("sftp args = %+v, %v", a, err)
	}
}
