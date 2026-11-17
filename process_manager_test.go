package main

import (
	"regexp"
	"testing"
)

func TestApplyRegex(t *testing.T) {
	cases := []struct {
		name      string
		regex     string
		file      string
		wantGroup []string
		wantJoin  string
	}{
		{"no match", `^zzz_(\w)`, "a_1.txt", []string{"a_1.txt_archive"}, "a_1.txt_archive"},
		{"match without groups returns the whole match", `\d+`, "file12.txt", []string{"12"}, "12"},
		{"one group", `^(\w)_`, "a_1.txt", []string{"a"}, "a"},
		{"several groups are joined with ___", `^(\w)_(\d)`, "a_1.txt", []string{"a", "1"}, "a___1"},
		{"empty group falls back to _archive", `^(x*)a`, "a_1.txt", []string{""}, "a_1.txt_archive"},
		{"match equal to whole name falls back to _archive", `^a_1\.txt$`, "a_1.txt", []string{"a_1.txt"}, "a_1.txt_archive"},
		{"group equal to whole name falls back to _archive", `^(a_1\.txt)$`, "a_1.txt", []string{"a_1.txt"}, "a_1.txt_archive"},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			m := ProcessManager{args: &Args{commonRegex: regexp.MustCompile(c.regex)}}
			groups, joined := m.applyRegex(c.file)
			if !AreEqual(groups, c.wantGroup) {
				t.Errorf("groups = %q, want %q", groups, c.wantGroup)
			}
			if joined != c.wantJoin {
				t.Errorf("joined = %q, want %q", joined, c.wantJoin)
			}
		})
	}
}
