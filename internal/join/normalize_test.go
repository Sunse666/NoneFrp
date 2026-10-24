package join

import "testing"

func TestNormalizeRoom(t *testing.T) {
	cases := map[string]string{
		"7fcrfu":                        "7fcrfu",
		" 7FCRFU ":                      "7fcrfu",
		"7fcrfu.mc.example.com":         "7fcrfu",
		"7fcrfu.mc.example.com:25565":   "7fcrfu",
		"7fcrfu.mc.example.com:25565/":  "7fcrfu",
		"https://7fcrfu.mc.example.com": "7fcrfu",
		"":                              "",
	}
	for input, want := range cases {
		if got := NormalizeRoom(input); got != want {
			t.Fatalf("NormalizeRoom(%q) = %q, want %q", input, got, want)
		}
	}
}
