package main

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
)

func TestParsePEQAutoEQ(t *testing.T) {
	in := "Preamp: -6.2 dB\r\n" +
		"Filter 1: ON LSC Fc 105 Hz Gain 5.5 dB Q 0.71\n" +
		"Filter 2: ON PK Fc 2,5 kHz Gain -3.1 dB Q 1.40\n" +
		"Filter 3: OFF PK Fc 300 Hz Gain 1 dB Q 2\n" +
		"Filter 4: ON HPQ Fc 30 Hz Q 0.5\n" +
		"Filter: ON PK Fc 1000 Hz Gain 2 dB BW Oct 1\n" +
		"Filter 6: OFF None\n" +
		"# comment\n"
	res, err := parsePEQ(in)
	if err != nil {
		t.Fatal(err)
	}
	if res.Preamp == nil || *res.Preamp != -6.2 {
		t.Fatalf("preamp = %v", res.Preamp)
	}
	if len(res.Filters) != 5 {
		t.Fatalf("got %d filters: %+v", len(res.Filters), res.Filters)
	}
	want := []Filter{
		{true, "LS", 105, 5.5, 0.71},
		{true, "PK", 2500, -3.1, 1.4},
		{false, "PK", 300, 1, 2},
		{true, "HP", 30, 0, 0.5},
		{true, "PK", 1000, 2, 1.414},
	}
	for i, w := range want {
		if res.Filters[i] != w {
			t.Errorf("filter %d = %+v, want %+v", i, res.Filters[i], w)
		}
	}
}

func TestParsePEQErrors(t *testing.T) {
	for _, in := range []string{"", "hello world", "Filter 1: ON XYZ Fc 100 Hz", "Filter 1: ON PK Gain 3 dB"} {
		if _, err := parsePEQ(in); err == nil {
			t.Errorf("expected error for %q", in)
		}
	}
}

func TestExportRoundTrip(t *testing.T) {
	e := EQConfig{Preamp: -4.5, Filters: []Filter{{true, "LS", 100, 4, 0.707}, {false, "PK", 3000, -2, 1.2}, {true, "NO", 50, 0, 10}}}
	res, err := parsePEQ(exportPEQ(e))
	if err != nil {
		t.Fatal(err)
	}
	if *res.Preamp != e.Preamp {
		t.Errorf("preamp %v", *res.Preamp)
	}
	for i := range e.Filters {
		if res.Filters[i] != e.Filters[i] {
			t.Errorf("filter %d = %+v, want %+v", i, res.Filters[i], e.Filters[i])
		}
	}
}

func TestResponseAndAutoPreamp(t *testing.T) {
	// A +6 dB peak at 1 kHz must peak at ~+6 dB there; auto preamp compensates.
	f := Filter{true, "PK", 1000, 6, 1}
	if got := f.coeffs().magnitudeDB(1000); math.Abs(got-6) > 0.01 {
		t.Errorf("PK gain at fc = %.3f", got)
	}
	if got := autoPreamp([]Filter{f}); got > -5.9 || got < -6.2 {
		t.Errorf("autoPreamp = %v", got)
	}
	if got := autoPreamp([]Filter{{true, "PK", 1000, -6, 1}}); got != 0 {
		t.Errorf("cut-only autoPreamp = %v", got)
	}
	ls := Filter{true, "LS", 100, 6, 0.707}
	if got := ls.coeffs().magnitudeDB(20); math.Abs(got-6) > 0.3 {
		t.Errorf("low shelf at 20 Hz = %.2f", got)
	}
	if got := ls.coeffs().magnitudeDB(10000); math.Abs(got) > 0.1 {
		t.Errorf("low shelf at 10 kHz = %.2f", got)
	}
}

func TestValidateEQ(t *testing.T) {
	bad := []Filter{
		{true, "XX", 1000, 0, 1},
		{true, "PK", 5, 0, 1},
		{true, "PK", 1000, 50, 1},
		{true, "PK", 1000, 0, 0},
		{true, "PK", math.NaN(), 0, 1},
	}
	for _, f := range bad {
		e := EQConfig{Filters: []Filter{f}}
		if e.Validate() == nil {
			t.Errorf("expected error for %+v", f)
		}
	}
	e := EQConfig{AutoPreamp: true, Filters: []Filter{{true, "hp", 40, 3, 0.7}, {true, "PK", 100, 6, 1}}}
	if err := e.Validate(); err != nil {
		t.Fatal(err)
	}
	if e.Filters[0].Type != "HP" || e.Filters[0].Gain != 0 || e.Preamp >= 0 {
		t.Errorf("normalisation failed: %+v", e)
	}
	for _, p := range presets {
		pe := EQConfig{Filters: p.Filters}
		if err := pe.Validate(); err != nil {
			t.Errorf("preset %s invalid: %v", p.ID, err)
		}
	}
}

func TestRenderCamilla(t *testing.T) {
	cfg := defaultConfig()
	eq := EQConfig{Enabled: true, Preamp: -3, Filters: []Filter{{true, "PK", 1000, 3, 1}, {false, "HS", 8000, 2, 0.7}, {true, "LP", 18000, 0, 0.7}}}
	b, err := renderCamilla(cfg, eq)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Devices struct {
			Samplerate int `json:"samplerate"`
			Capture    map[string]any
			Playback   map[string]any
		}
		Filters map[string]struct {
			Type       string
			Parameters map[string]any
		}
		Pipeline []struct{ Names []string }
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Devices.Capture["device"] != loopbackCapture || doc.Devices.Playback["device"] != cfg.OutputDevice {
		t.Errorf("devices: %+v", doc.Devices)
	}
	if got := strings.Join(doc.Pipeline[0].Names, ","); got != "preamp,eq01,eq03" {
		t.Errorf("pipeline = %s", got)
	}
	if p := doc.Filters["eq01"].Parameters; p["type"] != "Peaking" || p["gain"] != 3.0 {
		t.Errorf("eq01 = %+v", p)
	}
	if _, ok := doc.Filters["eq03"].Parameters["gain"]; ok {
		t.Error("lowpass must not have a gain parameter")
	}
	if doc.Filters["preamp"].Parameters["gain"] != -3.0 {
		t.Error("preamp not applied")
	}

	eq.Enabled = false
	b, _ = renderCamilla(cfg, eq)
	json.Unmarshal(b, &doc)
	if len(doc.Pipeline[0].Names) != 1 || doc.Filters["preamp"].Parameters["gain"] != 0.0 {
		t.Errorf("disabled EQ must be a 0 dB passthrough: %s", b)
	}
}

func TestConfigValidateAndArgs(t *testing.T) {
	c := defaultConfig()
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	args := strings.Join(librespotArgs(c), " ")
	for _, want := range []string{"--initial-volume 100", "--bitrate 320", "--device " + loopbackPlayback, "--autoplay on", "--disable-audio-cache"} {
		if !strings.Contains(args, want) {
			t.Errorf("args missing %q: %s", want, args)
		}
	}
	for _, mut := range []func(*Config){
		func(c *Config) { c.Name = "" },
		func(c *Config) { c.Name = "bad\nname" },
		func(c *Config) { c.Bitrate = 128 },
		func(c *Config) { c.InitialVolume = 101 },
		func(c *Config) { c.OutputDevice = "hw:0; rm -rf /" },
		func(c *Config) { c.VolumeCtrl = "loud" },
	} {
		c := defaultConfig()
		mut(&c)
		if c.Validate() == nil {
			t.Errorf("expected validation error for %+v", c)
		}
	}
}

func TestCardID(t *testing.T) {
	for in, want := range map[string]string{"hw:CARD=sndrpihifiberry,DEV=0": "sndrpihifiberry", "hw:Device,0": "Device", "default": ""} {
		if got := cardID(in); got != want {
			t.Errorf("cardID(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPassword(t *testing.T) {
	h, err := hashPassword("secret123")
	if err != nil {
		t.Fatal(err)
	}
	if !verifyPassword(h, "secret123") || verifyPassword(h, "secret124") || verifyPassword("garbage", "x") {
		t.Error("password verification broken")
	}
}

func TestBoardSelection(t *testing.T) {
	bootDir = t.TempDir()
	if currentBoard() != "" {
		t.Fatal("expected unknown board without dac.txt")
	}
	for _, b := range boards {
		if err := setBoard(b.ID); err != nil {
			t.Fatal(err)
		}
		if got := currentBoard(); got != b.ID {
			t.Fatalf("got %q want %q", got, b.ID)
		}
	}
	if err := setBoard("evil\ndtoverlay=x"); err == nil {
		t.Fatal("unknown board accepted")
	}
}

func TestSoloistArgsKeepKeyAndVolume(t *testing.T) {
	cfg := defaultConfig()
	cfg.Name = "Kitchen"
	cfg.InitialVolume = 100
	args := soloistArgs(cfg, "secret-key")
	want := map[string]string{"-n": "Kitchen", "-k": "secret-key", "-i": "100"}
	for i := 0; i+1 < len(args); i += 2 {
		if v, ok := want[args[i]]; ok {
			if args[i+1] != v {
				t.Fatalf("%s = %q, want %q", args[i], args[i+1], v)
			}
			delete(want, args[i])
		}
	}
	if len(want) != 0 {
		t.Fatalf("missing flags: %v in %v", want, args)
	}
	if got := soloistPairArgs(cfg, "k"); got[0] != "--pair" {
		t.Fatalf("pair args must start with --pair, got %v", got)
	}
}
