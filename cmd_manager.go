package main

import (
	"flag"
	"fmt"
	"log"
	"net/url"
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
}

func (m Args) LogString() string {
	commonRegexStr := ""
	if m.sendType == "flat_tar" {
		commonRegexStr = "\n commonRegex=" + m.commonRegex.String()
	}
	first := fmt.Sprintf("\n-----------------------------\nLogfile: %s\n-----------------------------\nCMD Args:\n name=%s,\n dst=%s,\n src=%s,\n duration=%d sec.,\n user=%s,\n type=%s,\n transfer=%s%s\n", LogPath, args.name, args.dst.String(), args.src, int(args.duration.Seconds()), args.user, args.sendType, args.tType, commonRegexStr)
	if m.preconvert {
		first += fmt.Sprintf(" User (Converter dest)=%s\n Public link (Converter dest)=%s\n Converter dest=%s\n", m.userConverter, m.public, m.dstConverter.String())
	}
	return first + "-----------------------------\n"

}

// GetCmdArgs Get/Parse command line arguments manager
func GetCmdArgs() Args {
	var fp, dst, user, pass, crt, durationStr, tType, name string
	var dstConverter, userConverter, passConverter, public string
	var preconvertString string
	var preconvert bool
	var duration int
	var commonRegexStr string
	var commonRegex *regexp.Regexp
	var sendType string
	var err error

	flag.StringVar(&fp, "src", "{{ src }}", "Source directory to be watched.")
	flag.StringVar(&name, "name", "{{ name }}", "Name of the EFW instance.")
	flag.StringVar(&dst, "dst", "{{ dst }}", "WebDAV destination URL. If the destination is on the lsdf, the URL should be as follows:\nhttps://os-webdav.lsdf.kit.edu/<OE>/<inst>/projects/<PROJECTNAME>/\n            <OE>-Organisationseinheit, z.B. kit.\n            <inst>-Institut-Name, z.B. ioc, scc, ikp, imk-asf etc.\n            <USERNAME>-User-Name z.B. xy1234, bs_abcd etc.\n            <PROJRCTNAME>-Projekt-Name")
	flag.StringVar(&user, "user", "{{ user }}", "WebDAV or SFTP user")
	flag.StringVar(&pass, "pass", "{{ password }}", "WebDAV or SFTP Password")
	flag.StringVar(&durationStr, "duration", "{{ duration }}", "Duration in seconds, i.e., how long a file must not be changed before sent.")
	flag.StringVar(&crt, "crt", "{{ crt }}", "Path to server TLS certificate. Only needed if the server has a self signed certificate.")
	/// Only considered if result are stored in a folder.
	/// If zipped is set the result folder will be transferred as zip file
	flag.StringVar(&sendType, "type", "{{ type }}", "Type must be 'file', 'folder', 'tar', 'zip' or 'flat_tar'. The 'file' option means that each file is handled individually, the 'folder' option means that entire folders are transmitted only when all files in them are ready. The option 'tar' and/or 'zip' send a folder zipped, only when all files in a folder are ready. The flat_tar option packs all files with have a common prefix into a tar file in a flat folder hierarchy")
	flag.StringVar(&commonRegexStr, "commonRegex", "{{ common_regex }}", "The common prefix length is only required if the type is flat_tar. This value specifies the number of leading characters that must be the same in order for files to be packed together.")
	flag.StringVar(&tType, "transfer", "{{ tType }}", "Type must be 'webdav' or 'sftp'.")

	flag.StringVar(&dstConverter, "dstConverter", "{{ dst_converter }}", "WebDAV destination URL. for converted files")
	flag.StringVar(&userConverter, "userConverter", "{{ user_converter }}", "WebDAV or SFTP user for converted files")
	flag.StringVar(&passConverter, "passConverter", "{{ password_converter }}", "WebDAV or SFTP Password for converted files")

	flag.StringVar(&preconvertString, "preconvert", "{{ preconvert }}", "[True or 1] to pre-convert the file and store raw at destination.")
	flag.StringVar(&public, "public", "{{ public }}", "The prefix of the publicly accessible link to the raw files. It should end with an /. Only the file name is added to this prefix.")

	flag.Parse()

	if duration, err = strconv.Atoi(durationStr); err != nil {
		log.Fatal("Duration must be an integer!")
	}

	if sendType != "file" && sendType != "folder" && sendType != "zip" && sendType != "tar" && sendType != "flat_tar" {
		err := "'type' has to be 'file', 'folder', 'tar', 'zip' or flat_tar"
		log.Fatal(err)
	}

	if sendType == "flat_tar" {
		if commonRegex, err = regexp.Compile(commonRegexStr); err != nil {
			log.Fatal("Common prefix length must be an integer if type is flat_tar!")
		}
	}

	if tType != "webdav" && tType != "sftp" {
		err := "'transfer' has to be 'sftp' or 'webdav'"
		log.Fatal(err)
	}

	if dst == "" || fp == "" || sendType == "" {
		err := "'dst' and 'src' must not be empty!"
		log.Fatal(err)
	}

	u, err := url.Parse(dst)
	if (err != nil || u.Scheme == "") && tType == "sftp" {
		u, err = url.Parse("ssh://" + dst)
		if err == nil && !strings.Contains(u.Host, ":") {
			u.Host += ":22"
		}
	}

	if dst == "" || fp == "" || sendType == "" {
		err := "'dst' and 'src' must not be empty!"
		log.Fatal(err)
	}

	preconvertStringL := strings.ToLower(preconvertString)
	preconvert = preconvertStringL == "1" || preconvertStringL == "true" || preconvertStringL == "t"
	var dstConverterUrl *url.URL
	if preconvert {
		dstConverterUrl, err = url.Parse(dstConverter)
		if (err != nil || dstConverterUrl.Scheme == "") && tType == "sftp" {
			dstConverterUrl, err = url.Parse("ssh://" + dstConverter)
			if err == nil && !strings.Contains(dstConverterUrl.Host, ":") {
				dstConverterUrl.Host += ":22"
			}
		}

		if public == "" {
			log.Fatal("'public'-Link prefix must be specified!")
		}
	} else {
		dstConverterUrl = u
	}

	if err != nil {
		log.Fatal(err)
	}

	return Args{src: fp, dst: *u, user: user, pass: pass, crt: crt,
		duration: time.Duration(duration) * time.Second, sendType: sendType, tType: tType, name: name, commonRegex: commonRegex, preconvert: preconvert,
		dstConverter: *dstConverterUrl, public: public,
		userConverter: userConverter, passConverter: passConverter}

}
