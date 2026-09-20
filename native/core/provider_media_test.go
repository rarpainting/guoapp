package core

import "testing"

func TestPreferredProviderURLKeepsTheSuccessfulHost(t *testing.T) {
	d := &Downloader{cfg: Config{HuangguoAIURL: "https://configured.example"}, providerHosts: map[string]string{
		sourceHuangguoAI: "https://preferred.example",
	}}
	got := d.preferredProviderURL("https://huangguoai.com/api/videos")
	if want := "https://preferred.example/api/videos"; got != want {
		t.Fatalf("preferred URL = %q, want %q", got, want)
	}
}
