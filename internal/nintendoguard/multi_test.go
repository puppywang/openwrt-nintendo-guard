package nintendoguard

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/miekg/dns"
)

func TestLegacyMigration(t *testing.T) {
	for _, mode := range []string{"off", "updates", "all"} {
		var c Config
		if err := json.Unmarshal([]byte(`{"mode":"`+mode+`","mac":"02:AA:BB:CC:DD:01"}`), &c); err != nil {
			t.Fatal(err)
		}
		c, err := c.Validate()
		if err != nil || len(c.Devices) != 1 || c.Devices[0].MAC != "02:aa:bb:cc:dd:01" || c.Devices[0].Mode != mode || c.Upstream != "127.0.0.1:53" {
			t.Fatal(c, err)
		}
		b, _ := json.Marshal(c)
		var again Config
		json.Unmarshal(b, &again)
		again, err = again.Validate()
		if err != nil || !reflect.DeepEqual(c, again) || strings.Contains(string(b), `"mode":"`+mode+`","mac"`) {
			t.Fatal(string(b), err)
		}
	}
	c, err := (Config{Mode: "off"}).Validate()
	if err != nil || c.Devices == nil || len(c.Devices) != 0 {
		t.Fatal(c, err)
	}
}

func TestMultipleValidation(t *testing.T) {
	base := Device{Name: "客厅 Switch 2", MAC: "02:aa:bb:cc:dd:01", Mode: "updates"}
	for _, devices := range [][]Device{
		{base, {MAC: "02:AA:BB:CC:DD:01", Mode: "all"}},
		{{MAC: base.MAC, Mode: "invalid"}},
		{{MAC: "ff:ff:ff:ff:ff:ff", Mode: "off"}},
		{{MAC: base.MAC, Mode: "updates", Name: strings.Repeat("台", 65)}},
		{{MAC: base.MAC, Mode: "updates", Name: "line\nbreak"}},
	} {
		if _, err := (Config{Devices: devices}).Validate(); err == nil {
			t.Fatalf("accepted %#v", devices)
		}
	}
	if _, err := (Config{Mode: "all", Devices: []Device{base}}).Validate(); err == nil {
		t.Fatal("ambiguous legacy fields accepted")
	}
	devices := make([]Device, MaxDevices)
	for i := range devices {
		devices[i] = Device{Name: strings.Repeat("台", 64), MAC: fmt.Sprintf("02:00:00:00:00:%02x", i), Mode: "updates"}
	}
	c, err := (Config{Devices: devices}).Validate()
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(c)
	if len(b) >= ConfigInputLimit {
		t.Fatal("maximum configuration exceeds API limit")
	}
	if _, err := (Config{Devices: append(devices, base)}).Validate(); err == nil {
		t.Fatal("device limit ignored")
	}
	c.Devices[0].Name = "changed"
	if devices[0].Name == "changed" {
		t.Fatal("validation aliased caller's slice")
	}
}

func TestMixedPoliciesAndChanges(t *testing.T) {
	u := Device{Name: "A", MAC: "02:aa:bb:cc:dd:01", Mode: "updates"}
	a := Device{Name: "B", MAC: "02:aa:bb:cc:dd:02", Mode: "all"}
	o := Device{Name: "C", MAC: "02:aa:bb:cc:dd:03", Mode: "off"}
	c, err := (Config{Devices: []Device{u, a, o}}).Validate()
	if err != nil {
		t.Fatal(err)
	}
	rules := c.Rules()
	if strings.Contains(rules, o.MAC) || !strings.Contains(rules, "ether saddr "+a.MAC+" counter reject") || strings.Contains(rules, "ether saddr "+u.MAC+" counter reject") {
		t.Fatal(rules)
	}
	renamed := c
	renamed.Devices = []Device{o, a, u}
	renamed.Devices[2].Name = "renamed"
	if c.Fingerprint() != renamed.Fingerprint() || len(changedDevices(c, renamed)) != 0 {
		t.Fatal("rename/reorder changes network policy")
	}
	renamed.Devices[1].Mode = "off"
	if c.Fingerprint() == renamed.Fingerprint() || !reflect.DeepEqual(changedDevices(c, renamed), map[string]bool{a.MAC: true}) {
		t.Fatal("wrong affected devices")
	}
	if !reflect.DeepEqual(changedDevices(c, Config{Devices: []Device{u, o}}), map[string]bool{a.MAC: true}) {
		t.Fatal("removed device not cleaned")
	}
	if len(changedDevices(Config{}, Config{Devices: []Device{o}})) != 0 {
		t.Fatal("adding disabled device reconnects it")
	}
	if (Config{Devices: []Device{o}}).Active() {
		t.Fatal("disabled configuration active")
	}
	if strings.Contains((Config{Devices: []Device{o}}).Rules(), "hook") {
		t.Fatal("disabled configuration installs hooks")
	}
}

func TestMixedDNSListeners(t *testing.T) {
	s := &Service{config: Config{Devices: []Device{{Mode: "updates"}, {Mode: "all"}}}}
	for _, tc := range []struct {
		mode, host string
		code       int
	}{
		{"updates", "sun.p01.lp1.d4c.srv.nintendo.net.", dns.RcodeNameError},
		{"all", "atum.p01.lp1.d4c.srv.nintendo.net.", dns.RcodeNameError},
		{"all", "example.org.", dns.RcodeNameError},
	} {
		q := new(dns.Msg)
		q.SetQuestion(tc.host, dns.TypeA)
		w := new(response)
		s.dnsMode(tc.mode, w, q)
		if w.msg.Rcode != tc.code {
			t.Fatal(tc, w.msg)
		}
	}
	s.config.Devices = []Device{{Mode: "updates"}}
	q := new(dns.Msg)
	q.SetQuestion("example.org.", dns.TypeA)
	w := new(response)
	s.dnsMode("all", w, q)
	if w.msg.Rcode != dns.RcodeRefused {
		t.Fatal("unused listener still active")
	}
}
