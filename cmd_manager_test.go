package main

import "testing"

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
