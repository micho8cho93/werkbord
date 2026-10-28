package device

import (
	"strings"
	"testing"
)

func TestSetupRequestValidation(t *testing.T) {
	code := strings.Repeat("ab", 32)
	ok := SetupRequest{Controller: " https://home.tailnet.ts.net:8443/ ", Name: " Laptop ", Code: " " + code + " "}
	if err := ok.Validate(); err != nil {
		t.Fatal(err)
	}
	if ok.Controller != "https://home.tailnet.ts.net:8443" || ok.Name != "Laptop" || ok.Code != code {
		t.Fatalf("not normalised: %+v", ok)
	}
	if err := (&SetupRequest{Controller: "https://home.tailnet.ts.net", Runner: true, RunnerCode: "runner-code"}).Validate(); err != nil {
		t.Fatal("a code may be omitted when a saved credential is reused:", err)
	}
	for name, r := range map[string]SetupRequest{
		"loopback controller":  {Controller: "http://127.0.0.1:8080", Code: code},
		"localhost":            {Controller: "http://localhost:8080", Code: code},
		"not a url":            {Controller: "home", Code: code},
		"path in controller":   {Controller: "https://home.tailnet.ts.net/api", Code: code},
		"credentials in url":   {Controller: "https://u:p@home.tailnet.ts.net", Code: code},
		"short code":           {Controller: "https://home.tailnet.ts.net", Code: "abcd"},
		"non-hex code":         {Controller: "https://home.tailnet.ts.net", Code: strings.Repeat("zz", 32)},
		"newline in name":      {Controller: "https://home.tailnet.ts.net", Code: code, Name: "a\nb"},
		"long name":            {Controller: "https://home.tailnet.ts.net", Code: code, Name: strings.Repeat("n", 121)},
		"runner code, no role": {Controller: "https://home.tailnet.ts.net", Code: code, RunnerCode: "abc"},
		"huge runner code":     {Controller: "https://home.tailnet.ts.net", Code: code, Runner: true, RunnerCode: strings.Repeat("a", 4097)},
	} {
		if err := r.Validate(); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}
