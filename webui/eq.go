package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
)

const (
	sampleRate = 44100
	maxFilters = 32
)

// Filter types follow the REW / Equalizer APO / AutoEQ naming.
//
//	PK peaking, LS low shelf, HS high shelf, LP low pass, HP high pass, NO notch
type Filter struct {
	Enabled bool    `json:"enabled"`
	Type    string  `json:"type"`
	Freq    float64 `json:"freq"`
	Gain    float64 `json:"gain"`
	Q       float64 `json:"q"`
}

type EQConfig struct {
	Enabled    bool     `json:"enabled"`
	Preset     string   `json:"preset"`
	Preamp     float64  `json:"preamp"`
	AutoPreamp bool     `json:"auto_preamp"`
	Filters    []Filter `json:"filters"`
}

var filterTypes = []string{"PK", "LS", "HS", "LP", "HP", "NO"}

func (f Filter) hasGain() bool { return f.Type == "PK" || f.Type == "LS" || f.Type == "HS" }

func (e *EQConfig) Validate() error {
	if e.Filters == nil {
		e.Filters = []Filter{}
	}
	if len(e.Filters) > maxFilters {
		return fmt.Errorf("at most %d filters are supported", maxFilters)
	}
	if math.IsNaN(e.Preamp) || e.Preamp < -40 || e.Preamp > 20 {
		return errors.New("preamp must be between -40 and +20 dB")
	}
	if len(e.Preset) > 64 {
		return errors.New("preset name too long")
	}
	for i := range e.Filters {
		f := &e.Filters[i]
		f.Type = strings.ToUpper(f.Type)
		if !contains(filterTypes, f.Type) {
			return fmt.Errorf("filter %d: unknown type %q", i+1, f.Type)
		}
		if !(f.Freq >= 10 && f.Freq < sampleRate/2-50) {
			return fmt.Errorf("filter %d: frequency must be 10-22000 Hz", i+1)
		}
		if !(f.Q >= 0.05 && f.Q <= 30) {
			return fmt.Errorf("filter %d: Q must be 0.05-30", i+1)
		}
		if !(f.Gain >= -30 && f.Gain <= 30) {
			return fmt.Errorf("filter %d: gain must be -30..+30 dB", i+1)
		}
		if !f.hasGain() {
			f.Gain = 0
		}
	}
	if e.AutoPreamp {
		e.Preamp = autoPreamp(e.Filters)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Frequency response (RBJ audio EQ cookbook, same formulas as CamillaDSP)

type biquad struct{ b0, b1, b2, a0, a1, a2 float64 }

func (f Filter) coeffs() biquad {
	w0 := 2 * math.Pi * f.Freq / sampleRate
	cw, sw := math.Cos(w0), math.Sin(w0)
	alpha := sw / (2 * f.Q)
	A := math.Pow(10, f.Gain/40)
	switch f.Type {
	case "PK":
		return biquad{1 + alpha*A, -2 * cw, 1 - alpha*A, 1 + alpha/A, -2 * cw, 1 - alpha/A}
	case "LS":
		sa := 2 * math.Sqrt(A) * alpha
		return biquad{
			A * ((A + 1) - (A-1)*cw + sa), 2 * A * ((A - 1) - (A+1)*cw), A * ((A + 1) - (A-1)*cw - sa),
			(A + 1) + (A-1)*cw + sa, -2 * ((A - 1) + (A+1)*cw), (A + 1) + (A-1)*cw - sa}
	case "HS":
		sa := 2 * math.Sqrt(A) * alpha
		return biquad{
			A * ((A + 1) + (A-1)*cw + sa), -2 * A * ((A - 1) + (A+1)*cw), A * ((A + 1) + (A-1)*cw - sa),
			(A + 1) - (A-1)*cw + sa, 2 * ((A - 1) - (A+1)*cw), (A + 1) - (A-1)*cw - sa}
	case "LP":
		return biquad{(1 - cw) / 2, 1 - cw, (1 - cw) / 2, 1 + alpha, -2 * cw, 1 - alpha}
	case "HP":
		return biquad{(1 + cw) / 2, -(1 + cw), (1 + cw) / 2, 1 + alpha, -2 * cw, 1 - alpha}
	case "NO":
		return biquad{1, -2 * cw, 1, 1 + alpha, -2 * cw, 1 - alpha}
	}
	return biquad{1, 0, 0, 1, 0, 0}
}

func (c biquad) magnitudeDB(freq float64) float64 {
	w := 2 * math.Pi * freq / sampleRate
	// |H(e^jw)| with z^-1 = e^-jw
	nr := c.b0 + c.b1*math.Cos(w) + c.b2*math.Cos(2*w)
	ni := -c.b1*math.Sin(w) - c.b2*math.Sin(2*w)
	dr := c.a0 + c.a1*math.Cos(w) + c.a2*math.Cos(2*w)
	di := -c.a1*math.Sin(w) - c.a2*math.Sin(2*w)
	return 10 * math.Log10((nr*nr+ni*ni)/(dr*dr+di*di))
}

// responsePeak returns the maximum combined gain (dB) of all enabled filters.
func responsePeak(filters []Filter) float64 {
	var cs []biquad
	for _, f := range filters {
		if f.Enabled {
			cs = append(cs, f.coeffs())
		}
	}
	peak := math.Inf(-1)
	const points = 400
	for i := 0; i < points; i++ {
		freq := 20 * math.Pow(1000, float64(i)/(points-1)) // 20 Hz .. 20 kHz
		sum := 0.0
		for _, c := range cs {
			sum += c.magnitudeDB(freq)
		}
		peak = math.Max(peak, sum)
	}
	return peak
}

// autoPreamp returns the negative gain needed to keep the EQ from clipping.
func autoPreamp(filters []Filter) float64 {
	p := responsePeak(filters)
	if p <= 0 {
		return 0
	}
	return -math.Ceil(p*10) / 10
}

// ---------------------------------------------------------------------------
// Presets

type Preset struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Filters []Filter `json:"filters"`
}

func pk(f, g, q float64) Filter { return Filter{true, "PK", f, g, q} }
func ls(f, g float64) Filter    { return Filter{true, "LS", f, g, 0.707} }
func hs(f, g float64) Filter    { return Filter{true, "HS", f, g, 0.707} }
func hp(f float64) Filter       { return Filter{true, "HP", f, 0, 0.707} }

var presets = []Preset{
	{"flat", "Flat", []Filter{}},
	{"bass_boost", "Bass boost", []Filter{ls(105, 6)}},
	{"bass_reduce", "Bass reducer", []Filter{ls(105, -6)}},
	{"treble_boost", "Treble boost", []Filter{hs(8000, 5)}},
	{"treble_reduce", "Treble reducer", []Filter{hs(8000, -5)}},
	{"loudness", "Loudness", []Filter{ls(90, 5), hs(10000, 3.5)}},
	{"vocal", "Vocal / podcast", []Filter{ls(120, -2), pk(1500, 2, 1), pk(3000, 3, 1), hs(10000, -1)}},
	{"rock", "Rock", []Filter{pk(60, 4, 0.9), pk(250, -1.5, 1), pk(1000, -1, 1), pk(4000, 2.5, 1), hs(10000, 3)}},
	{"pop", "Pop", []Filter{pk(60, -1, 1), pk(250, 2, 1), pk(1000, 3, 1), pk(4000, 1, 1), hs(10000, -1)}},
	{"jazz", "Jazz", []Filter{ls(100, 3), pk(500, -1, 1), pk(2000, 1.5, 1), hs(8000, 2)}},
	{"classical", "Classical", []Filter{ls(100, 2.5), pk(1000, -0.5, 0.7), hs(8000, 2)}},
	{"electronic", "Electronic", []Filter{ls(60, 5), pk(500, -1.5, 1), pk(3000, 1, 1), hs(12000, 3.5)}},
	{"small_speakers", "Small speakers", []Filter{hp(50), ls(150, 4), hs(10000, 2)}},
	{"late_night", "Late night", []Filter{ls(100, -4), pk(3000, 1.5, 1), hs(10000, -2)}},
	{"graphic10", "10-band graphic (flat start)", graphic10()},
}

func graphic10() []Filter {
	var fs []Filter
	for _, f := range []float64{31, 62, 125, 250, 500, 1000, 2000, 4000, 8000, 16000} {
		fs = append(fs, pk(f, 0, 1.41))
	}
	return fs
}

// ---------------------------------------------------------------------------
// PEQ import / export (AutoEQ "ParametricEQ.txt", REW, Equalizer APO)
//
//   Preamp: -6.2 dB
//   Filter 1: ON PK Fc 105 Hz Gain -2.4 dB Q 0.70
//   Filter: ON LSC Fc 105 Hz Gain 5.5 dB Q 0.71

var typeAliases = map[string]string{
	"PK": "PK", "PEQ": "PK", "MODAL": "PK",
	"LS": "LS", "LSC": "LS", "LSQ": "LS", "LOWSHELF": "LS",
	"HS": "HS", "HSC": "HS", "HSQ": "HS", "HIGHSHELF": "HS",
	"LP": "LP", "LPQ": "LP", "LOWPASS": "LP",
	"HP": "HP", "HPQ": "HP", "HIGHPASS": "HP",
	"NO": "NO", "NOTCH": "NO",
}

type ParseResult struct {
	Preamp   *float64 `json:"preamp"`
	Filters  []Filter `json:"filters"`
	Warnings []string `json:"warnings"`
}

func parseNum(s string) (float64, error) {
	return strconv.ParseFloat(strings.ReplaceAll(s, ",", "."), 64)
}

func parsePEQ(text string) (ParseResult, error) {
	res := ParseResult{Filters: []Filter{}, Warnings: []string{}}
	for n, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || line[0] == '#' || line[0] == ';' || strings.HasPrefix(line, "//") {
			continue
		}
		key, rest, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key = strings.ToLower(strings.TrimSpace(key))
		tok := strings.Fields(rest)
		switch {
		case key == "preamp":
			if len(tok) == 0 {
				return res, fmt.Errorf("line %d: missing preamp value", n+1)
			}
			v, err := parseNum(tok[0])
			if err != nil {
				return res, fmt.Errorf("line %d: bad preamp value", n+1)
			}
			res.Preamp = &v
		case strings.HasPrefix(key, "filter"):
			f, skip, err := parseFilterTokens(tok)
			if err != nil {
				return res, fmt.Errorf("line %d: %v", n+1, err)
			}
			if !skip {
				res.Filters = append(res.Filters, f)
			}
		case key == "channel":
			if len(tok) > 0 && !strings.EqualFold(tok[0], "all") {
				res.Warnings = append(res.Warnings, "per-channel sections are applied to both channels")
			}
		default:
			res.Warnings = append(res.Warnings, fmt.Sprintf("line %d ignored: %q", n+1, line))
		}
	}
	if len(res.Filters) > maxFilters {
		return res, fmt.Errorf("%d filters found, at most %d supported", len(res.Filters), maxFilters)
	}
	if len(res.Filters) == 0 && res.Preamp == nil {
		return res, errors.New("no filters found (expected lines like \"Filter 1: ON PK Fc 100 Hz Gain -3 dB Q 1.0\")")
	}
	return res, nil
}

func parseFilterTokens(tok []string) (f Filter, skip bool, err error) {
	if len(tok) < 2 {
		return f, true, nil
	}
	f.Enabled = strings.EqualFold(tok[0], "ON")
	t := strings.ToUpper(tok[1])
	if t == "NONE" {
		return f, true, nil
	}
	typ, ok := typeAliases[t]
	if !ok {
		return f, false, fmt.Errorf("unsupported filter type %q", tok[1])
	}
	f.Type = typ
	f.Q = 0.707
	var bwOct float64
	for i := 2; i < len(tok); i++ {
		next := func() (float64, error) {
			if i+1 >= len(tok) {
				return 0, fmt.Errorf("missing value after %s", tok[i])
			}
			i++
			return parseNum(tok[i])
		}
		switch strings.ToUpper(tok[i]) {
		case "FC":
			if f.Freq, err = next(); err != nil {
				return
			}
			if i+1 < len(tok) && strings.EqualFold(tok[i+1], "kHz") {
				f.Freq *= 1000
			}
		case "GAIN":
			if f.Gain, err = next(); err != nil {
				return
			}
		case "Q":
			if f.Q, err = next(); err != nil {
				return
			}
		case "BW":
			if i+1 < len(tok) && strings.EqualFold(tok[i+1], "OCT") {
				i++
			}
			if bwOct, err = next(); err != nil {
				return
			}
		}
	}
	if bwOct > 0 {
		p := math.Pow(2, bwOct)
		f.Q = math.Sqrt(p) / (p - 1)
	}
	if f.Freq == 0 {
		return f, false, errors.New("missing Fc")
	}
	f.Q = math.Round(f.Q*1000) / 1000
	return f, false, nil
}

func exportPEQ(e EQConfig) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Preamp: %.1f dB\n", e.Preamp)
	for i, f := range e.Filters {
		state := "ON"
		if !f.Enabled {
			state = "OFF"
		}
		typ := map[string]string{"LS": "LSC", "HS": "HSC", "LP": "LPQ", "HP": "HPQ"}[f.Type]
		if typ == "" {
			typ = f.Type
		}
		fmt.Fprintf(&b, "Filter %d: %s %s Fc %g Hz Gain %.1f dB Q %.3f\n", i+1, state, typ, f.Freq, f.Gain, f.Q)
	}
	return b.String()
}

// ---------------------------------------------------------------------------
// CamillaDSP config. YAML is a superset of JSON, so we emit JSON: no quoting
// or injection issues with user supplied values.

// effectiveEQ returns the live (applied but unsaved) EQ if present, else the saved one.
func effectiveEQ(cfg Config) EQConfig {
	if b, err := os.ReadFile(liveEQPath()); err == nil {
		var e EQConfig
		if json.Unmarshal(b, &e) == nil && e.Validate() == nil {
			return e
		}
	}
	return cfg.EQ
}

func renderCamilla(cfg Config, eq EQConfig) ([]byte, error) {
	type m = map[string]any
	filters := m{}
	var names []string
	gain := 0.0
	if eq.Enabled {
		gain = eq.Preamp
	}
	filters["preamp"] = m{"type": "Gain", "parameters": m{"gain": gain, "scale": "dB"}}
	names = append(names, "preamp")
	if eq.Enabled {
		cdType := map[string]string{"PK": "Peaking", "LS": "Lowshelf", "HS": "Highshelf", "LP": "Lowpass", "HP": "Highpass", "NO": "Notch"}
		for i, f := range eq.Filters {
			if !f.Enabled {
				continue
			}
			p := m{"type": cdType[f.Type], "freq": f.Freq, "q": f.Q}
			if f.hasGain() {
				p["gain"] = f.Gain
			}
			name := fmt.Sprintf("eq%02d", i+1)
			filters[name] = m{"type": "Biquad", "parameters": p}
			names = append(names, name)
		}
	}
	doc := m{
		"devices": m{
			"samplerate":         sampleRate,
			"chunksize":          1024,
			"target_level":       2048,
			"enable_rate_adjust": true,
			"adjust_period":      10,
			"capture": m{
				"type": "Alsa", "channels": 2, "device": loopbackCapture, "format": "S32_LE",
			},
			"playback": m{
				"type": "Alsa", "channels": 2, "device": cfg.OutputDevice, "format": cfg.OutputFormat,
			},
		},
		"filters": filters,
		"pipeline": []m{{
			"type": "Filter", "channels": []int{0, 1}, "names": names,
		}},
	}
	return json.MarshalIndent(doc, "", "  ")
}
