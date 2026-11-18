package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type Args struct {
	src, user, pass, crt                 string
	dst                                  url.URL
	duration                             time.Duration
	sendType                             string
	commonRegex                          *regexp.Regexp
	tType, name                          string
	dstConverter                         url.URL
	userConverter, public, passConverter string
	preconvert                           bool
	// hostKey is the expected SSH host key fingerprint of the SFTP server
	// (-hostkey); empty means "not configured".
	hostKey string
	// insecure disables TLS certificate and SSH host key verification (-insecure).
	insecure bool
	// version is set by -version; no other field is valid then.
	version bool
	// maxUploadMB limits one ChemAssistentManager upload request (-maxUpload);
	// 0 means the default, chemAssistentDefaultMaxMB.
	maxUploadMB int64
}

// transferChemAssistent is the -transfer value for uploads to a
// ChemAssistentManager job (POST <dst>/api/jobs/trigger, Bearer token in -pass).
const transferChemAssistent = "chemassistent"

// chemAssistentDefaultMaxMB matches the server default JOB_MAX_UPLOAD_MB.
const chemAssistentDefaultMaxMB = 100

// chemAssistentTriggerPath is appended to a -dst that has no path.
const chemAssistentTriggerPath = "/api/jobs/trigger"

func (m Args) LogString() string {
	commonRegexStr := ""
	if m.sendType == "flat_tar" {
		if m.commonRegex != nil {
			commonRegexStr = "\n commonRegex=" + m.commonRegex.String()
		}
	}
	if m.tType == transferChemAssistent {
		// The API key (-pass) is never logged, only a short prefix of a long key.
		keyInfo := ""
		if r := []rune(m.pass); len(r) >= 12 {
			keyInfo = ",\n key=" + string(r[:8]) + "..."
		}
		max := m.maxUploadMB
		if max <= 0 {
			max = chemAssistentDefaultMaxMB
		}
		return fmt.Sprintf("\n-----------------------------\nLogfile: %s\n-----------------------------\nCMD Args:\n name=%s,\n dst=%s,\n src=%s,\n duration=%d sec.,\n type=%s,\n transfer=%s,\n maxUpload=%d MB%s%s\n-----------------------------\n", LogPath, m.name, m.dst.String(), m.src, int(m.duration.Seconds()), m.sendType, m.tType, max, keyInfo, commonRegexStr)
	}
	first := fmt.Sprintf("\n-----------------------------\nLogfile: %s\n-----------------------------\nCMD Args:\n name=%s,\n dst=%s,\n src=%s,\n duration=%d sec.,\n user=%s,\n type=%s,\n transfer=%s%s\n", LogPath, m.name, m.dst.String(), m.src, int(m.duration.Seconds()), m.user, m.sendType, m.tType, commonRegexStr)
	if m.preconvert {
		first += fmt.Sprintf(" User (Converter dest)=%s\n Public link (Converter dest)=%s\n Converter dest=%s\n", m.userConverter, m.public, m.dstConverter.String())
	}
	return first + "-----------------------------\n"

}

// parseDst parses a destination (-dst or -dstConverter) for the transfer type
// tType. For sftp the scheme is optional ("host:22/path" works) and the port
// defaults to 22; the scheme sftp is normalised to ssh. For webdav the scheme
// must be http or https. For chemassistent the scheme must be http or https as
// well; an empty path or "/" becomes chemAssistentTriggerPath, any other path is
// kept as the full trigger URL without a trailing slash.
func parseDst(raw, tType string) (url.URL, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return url.URL{}, errors.New("destination must not be empty")
	}
	switch tType {
	case "sftp":
		lower := strings.ToLower(raw)
		if !strings.HasPrefix(lower, "ssh://") && !strings.HasPrefix(lower, "sftp://") {
			raw = "ssh://" + raw
		}
		u, err := url.Parse(raw)
		if err != nil {
			return url.URL{}, fmt.Errorf("invalid destination %q: %v", raw, err)
		}
		if u.Hostname() == "" {
			return url.URL{}, fmt.Errorf("destination %q has no host", raw)
		}
		if u.Port() == "" {
			u.Host = net.JoinHostPort(u.Hostname(), "22")
		}
		u.Scheme = "ssh"
		return *u, nil
	case "webdav":
		u, err := url.Parse(raw)
		if err != nil {
			return url.URL{}, fmt.Errorf("invalid destination %q: %v", raw, err)
		}
		if u.Scheme != "http" && u.Scheme != "https" {
			return url.URL{}, fmt.Errorf("webdav destination %q must start with http:// or https://", raw)
		}
		if u.Host == "" {
			return url.URL{}, fmt.Errorf("destination %q has no host", raw)
		}
		return *u, nil
	case transferChemAssistent:
		u, err := url.Parse(raw)
		if err != nil {
			return url.URL{}, fmt.Errorf("invalid destination %q: %v", raw, err)
		}
		if u.Scheme != "http" && u.Scheme != "https" {
			return url.URL{}, fmt.Errorf("chemassistent destination %q must start with http:// or https://", raw)
		}
		if u.Host == "" {
			return url.URL{}, fmt.Errorf("destination %q has no host", raw)
		}
		if u.Path == "" || u.Path == "/" {
			u.Path = chemAssistentTriggerPath
			u.RawPath = ""
		} else if strings.HasSuffix(u.Path, "/") {
			u.Path = strings.TrimRight(u.Path, "/")
			u.RawPath = ""
		}
		return *u, nil
	}
	return url.URL{}, fmt.Errorf("unknown transfer type %q", tType)
}

// GetCmdArgs parses the process command line. It stops the program with
// log.Fatal if the arguments are invalid.
func GetCmdArgs() Args {
	a, err := parseArgs(flag.NewFlagSet(os.Args[0], flag.ExitOnError), os.Args[1:])
	if err != nil {
		log.Fatal(err)
	}
	return a
}

// parseArgs defines all flags on fs, parses argv and validates the result.
func parseArgs(fs *flag.FlagSet, argv []string) (Args, error) {
	var fp, dst, user, pass, crt, durationStr, tType, name string
	var dstConverter, userConverter, passConverter, public string
	var preconvertString string
	var commonRegexStr string
	var hostKey string
	var insecureString string
	var commonRegex *regexp.Regexp
	var sendType string
	var showVersion bool
	var maxUpload int64
	var err error

	fs.StringVar(&fp, "src", "{{ src }}", "Source directory to be watched.")
	fs.StringVar(&name, "name", "{{ name }}", "Name of the EFW instance.")
	fs.StringVar(&dst, "dst", "{{ dst }}", "WebDAV destination URL. If the destination is on the lsdf, the URL should be as follows:\nhttps://os-webdav.lsdf.kit.edu/<OE>/<inst>/projects/<PROJECTNAME>/\n            <OE>-Organisationseinheit, z.B. kit.\n            <inst>-Institut-Name, z.B. ioc, scc, ikp, imk-asf etc.\n            <USERNAME>-User-Name z.B. xy1234, bs_abcd etc.\n            <PROJRCTNAME>-Projekt-Name")
	fs.StringVar(&user, "user", "{{ user }}", "WebDAV or SFTP user")
	fs.StringVar(&pass, "pass", "{{ password }}", "WebDAV or SFTP Password")
	fs.StringVar(&durationStr, "duration", "{{ duration }}", "Duration in seconds, i.e., how long a file must not be changed before sent.")
	fs.StringVar(&crt, "crt", "{{ crt }}", "Path to server TLS certificate. Only needed if the server has a self signed certificate.")
	fs.StringVar(&sendType, "type", "{{ type }}", "Type must be 'file', 'folder', 'tar', 'zip' or 'flat_tar'. The 'file' option means that each file is handled individually, the 'folder' option means that entire folders are transmitted only when all files in them are ready. The option 'tar' and/or 'zip' send a folder zipped, only when all files in a folder are ready. The flat_tar option packs all files with have a common prefix into a tar file in a flat folder hierarchy")
	fs.StringVar(&commonRegexStr, "commonRegex", "{{ common_regex }}", "The common prefix length is only required if the type is flat_tar. This value specifies the number of leading characters that must be the same in order for files to be packed together.")
	fs.StringVar(&tType, "transfer", "{{ tType }}", "Type must be 'webdav', 'sftp' or 'chemassistent'. With chemassistent, -dst is the ChemAssistentManager URL and -pass holds the API key (-user is ignored).")
	fs.StringVar(&dstConverter, "dstConverter", "{{ dst_converter }}", "WebDAV destination URL. for converted files")
	fs.StringVar(&userConverter, "userConverter", "{{ user_converter }}", "WebDAV or SFTP user for converted files")
	fs.StringVar(&passConverter, "passConverter", "{{ password_converter }}", "WebDAV or SFTP Password for converted files")
	fs.StringVar(&preconvertString, "preconvert", "{{ preconvert }}", "[True or 1] to pre-convert the file and store raw at destination.")
	fs.StringVar(&public, "public", "{{ public }}", "The prefix of the publicly accessible link to the raw files. It should end with an /. Only the file name is added to this prefix.")
	fs.StringVar(&hostKey, "hostkey", "{{ hostkey }}", "SFTP only: expected SSH host key fingerprint of the server, e.g. 'ssh-ed25519 255 SHA256:...' (WinSCP format) or 'SHA256:...'.")
	fs.StringVar(&insecureString, "insecure", "{{ insecure }}", "[True or 1] to skip TLS certificate and SSH host key verification. Only for testing or servers you cannot verify otherwise.")
	fs.Int64Var(&maxUpload, "maxUpload", chemAssistentDefaultMaxMB, "chemassistent only: maximum size of one upload request in MB (at least 1). Larger uploads are not sent and stay local. Should match the server limit JOB_MAX_UPLOAD_MB.")
	fs.BoolVar(&showVersion, "version", false, "Print the version and exit.")

	if err = fs.Parse(argv); err != nil {
		return Args{}, err
	}
	if showVersion {
		return Args{version: true}, nil
	}

	duration, err := strconv.Atoi(durationStr)
	if err != nil {
		return Args{}, errors.New("duration must be an integer")
	}
	if duration <= 0 {
		return Args{}, errors.New("duration must be greater than 0")
	}

	if sendType != "file" && sendType != "folder" && sendType != "zip" && sendType != "tar" && sendType != "flat_tar" {
		return Args{}, errors.New("'type' has to be 'file', 'folder', 'tar', 'zip' or flat_tar")
	}

	if sendType == "flat_tar" {
		if commonRegex, err = regexp.Compile(commonRegexStr); err != nil {
			return Args{}, fmt.Errorf("invalid -commonRegex: %v", err)
		}
	}

	if tType != "webdav" && tType != "sftp" && tType != transferChemAssistent {
		return Args{}, errors.New("'transfer' has to be 'sftp', 'webdav' or 'chemassistent'")
	}
	if maxUpload < 1 {
		return Args{}, errors.New("-maxUpload must be at least 1 (MB)")
	}

	if dst == "" || fp == "" {
		return Args{}, errors.New("'dst' and 'src' must not be empty")
	}

	u, err := parseDst(dst, tType)
	if err != nil {
		return Args{}, fmt.Errorf("invalid -dst: %v", err)
	}

	preconvert := isTrue(preconvertString)
	if tType == transferChemAssistent {
		if preconvert {
			return Args{}, errors.New("-preconvert is not supported with -transfer chemassistent")
		}
		if unsetPlaceholder(pass) == "" {
			return Args{}, errors.New("-pass must hold the ChemAssistentManager API key")
		}
		if !strings.HasPrefix(strings.TrimSpace(pass), "cam_") {
			log.Printf("warning: the API key in -pass does not start with \"cam_\"; the server may refuse it")
		}
	}
	dstConverterUrl := u
	if preconvert {
		dstConverterUrl, err = parseDst(dstConverter, tType)
		if err != nil {
			return Args{}, fmt.Errorf("invalid -dstConverter: %v", err)
		}
		if unsetPlaceholder(public) == "" {
			return Args{}, errors.New("'public'-Link prefix must be specified")
		}
	}

	return Args{src: fp, dst: u, user: user, pass: pass, crt: crt,
		duration: time.Duration(duration) * time.Second, sendType: sendType, tType: tType, name: name, commonRegex: commonRegex, preconvert: preconvert,
		dstConverter: dstConverterUrl, public: public,
		userConverter: userConverter, passConverter: passConverter,
		hostKey: unsetPlaceholder(hostKey), insecure: isTrue(unsetPlaceholder(insecureString)),
		maxUploadMB: maxUpload}, nil
}

// unsetPlaceholder returns "" for a value that is still an unrendered
// ShuttleBuilder placeholder (a binary built without ShuttleBuilder keeps the
// flag defaults literally). The delimiters are concatenated so that
// ShuttleBuilder's template pass does not touch this code.
func unsetPlaceholder(v string) string {
	t := strings.TrimSpace(v)
	if strings.HasPrefix(t, "{"+"{") && strings.HasSuffix(t, "}"+"}") {
		return ""
	}
	return v
}

// isTrue reports whether a flag value means "on" ("1", "true", "t"; any case).
func isTrue(v string) bool {
	v = strings.ToLower(strings.TrimSpace(v))
	return v == "1" || v == "true" || v == "t"
}
