package provider

import "testing"

func TestApplyOutbound(t *testing.T) {
	p := Provider{Name: "Relay", Anthropic: "https://relay.example"}
	if err := ApplyOutbound(&p, "chat"); err == nil {
		t.Fatal("chat without a chat URL was accepted")
	}
	if p.Outbound != "" {
		t.Fatalf("a refused value was stored: %q", p.Outbound)
	}
	if err := ApplyOutbound(&p, "nope"); err == nil {
		t.Fatal("an unknown API was accepted")
	}
	if err := ApplyOutbound(&p, "anthropic"); err != nil {
		t.Fatal(err)
	}
	if p.Outbound != string(Anthropic) {
		t.Fatalf("outbound %q", p.Outbound)
	}
	if err := ApplyOutbound(&p, "openai"); err == nil {
		t.Fatal("openai without a chat URL was accepted")
	}
	if err := ApplyOutbound(&p, "auto"); err != nil || p.Outbound != "" {
		t.Fatalf("auto: %v outbound %q", err, p.Outbound)
	}
}

func TestLeavesAsIgnoresASubscription(t *testing.T) {
	p := Provider{ID: "claude", Name: "Claude", Anthropic: "https://api.anthropic.com", Outbound: string(Anthropic), Account: &Account{Agent: "claude", User: "a"}}
	if _, ok := p.LeavesAs("claude-sonnet-4-5"); ok {
		t.Fatal("a subscription account took the outbound pin")
	}
}

func TestLeavesAsUsesTheModelAPIOverThePin(t *testing.T) {
	detectHome(t)
	if err := Save(Provider{ID: "relay", Name: "Relay", Key: "k", Chat: "https://relay.example/v1", Anthropic: "https://relay.example", Outbound: string(Anthropic), Models: []string{"m"}}); err != nil {
		t.Fatal(err)
	}
	p, err := Find("relay")
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := p.LeavesAs("m"); !ok || got != Anthropic {
		t.Fatalf("pin: %q %v", got, ok)
	}
	api := string(Chat)
	if err := SetModelPrefs("relay", map[string]ModelPref{"m": {API: &api}}); err != nil {
		t.Fatal(err)
	}
	p, _ = Find("relay")
	if got, ok := p.LeavesAs("m"); !ok || got != Chat {
		t.Fatalf("model API: %q %v", got, ok)
	}
}

func TestNormalizeDropsOutboundWithoutAURL(t *testing.T) {
	p := normalize(Provider{ID: "relay", Name: "Relay", Key: "k", Anthropic: "https://relay.example", Outbound: "chat"})
	if p.Outbound != "" {
		t.Fatalf("chat with no chat URL stayed %q", p.Outbound)
	}
	p = normalize(Provider{ID: "relay", Name: "Relay", Key: "k", Anthropic: "https://relay.example", Outbound: "openai"})
	if p.Outbound != "" {
		t.Fatalf("openai with no chat URL stayed %q", p.Outbound)
	}
	p = normalize(Provider{ID: "relay", Name: "Relay", Key: "k", Chat: "https://relay.example/v1", Outbound: "auto"})
	if p.Outbound != "" {
		t.Fatalf("auto stayed %q", p.Outbound)
	}
	p = normalize(Provider{ID: "relay", Name: "Relay", Key: "k", Chat: "https://relay.example/v1", Outbound: "openai"})
	if p.Outbound != string(Chat) {
		t.Fatalf("openai: %q", p.Outbound)
	}
}
