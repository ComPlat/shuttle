package main

// Pure builders for WinSCP scripts (used by the Windows XP SFTP path in
// transfer_sftp_xp.go). This file is untagged so that every toolchain, and
// modern "go test", compiles it. Keep it Go 1.10 clean.

import (
	"net/url"
	"path"
	"strings"
)

// winscpSession describes how to open the SFTP session.
type winscpSession struct {
	User, Pass, Host string // Host includes ":port" if given
	HostKey          string // "" = rely on WinSCP's cached key
}

// winscpQuote wraps s in double quotes and doubles any inner double quote.
func winscpQuote(s string) string {
	return "\"" + strings.Replace(s, "\"", "\"\"", -1) + "\""
}

// winscpOpenURL returns the sftp:// URL with user and password URL-escaped.
func winscpOpenURL(s winscpSession) string {
	u := url.URL{
		Scheme: "sftp",
		User:   url.UserPassword(s.User, s.Pass),
		Host:   s.Host,
		Path:   "/",
	}
	return u.String()
}

// winscpOpenLine returns the "open" command line.
func winscpOpenLine(s winscpSession) string {
	line := "open " + winscpQuote(winscpOpenURL(s))
	if s.HostKey != "" {
		line += " -hostkey=" + winscpQuote(s.HostKey)
	}
	return line
}

func winscpHeader(s winscpSession) []string {
	return []string{
		"option batch abort",
		"option confirm off",
		winscpOpenLine(s),
	}
}

// winscpMkdirLevels returns every folder level of dir from the top, excluding "/".
func winscpMkdirLevels(dir string) []string {
	var levels []string
	cur := ""
	for _, part := range strings.Split(path.Clean(dir), "/") {
		if part == "" || part == "." {
			continue
		}
		cur += "/" + part
		levels = append(levels, cur)
	}
	return levels
}

// winscpUploadScript returns the script lines for one upload: open, (mkdir for
// every level of the remote folder), put, exit.
func winscpUploadScript(s winscpSession, localPath, remotePath string) []string {
	lines := winscpHeader(s)
	levels := winscpMkdirLevels(path.Dir(remotePath))
	if len(levels) > 0 {
		lines = append(lines, "option batch continue")
		for _, l := range levels {
			lines = append(lines, "mkdir "+winscpQuote(l))
		}
		lines = append(lines, "option batch abort")
	}
	lines = append(lines,
		"put -nopermissions -nopreservetime "+winscpQuote(localPath)+" "+winscpQuote(remotePath),
		"exit")
	return lines
}

// winscpCheckScript returns the script lines for a connection check.
func winscpCheckScript(s winscpSession) []string {
	return append(winscpHeader(s), "exit")
}

// redactSecrets removes every secret from s, both as given and in the
// URL-escaped form used in the session URL. WinSCP output can quote the
// session, so its output must pass through this before it is logged.
func redactSecrets(s string, secrets ...string) string {
	for _, secret := range secrets {
		if secret == "" {
			continue
		}
		forms := []string{secret}
		// url.UserPassword("", p).String() is ":" + escaped p.
		if escaped := strings.TrimPrefix(url.UserPassword("", secret).String(), ":"); escaped != secret {
			forms = append(forms, escaped)
		}
		for _, f := range forms {
			s = strings.Replace(s, f, "***", -1)
		}
	}
	return s
}
