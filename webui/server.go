package main

import (
	"bufio"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"
)

//go:embed web
var webFS embed.FS

var bootTime = time.Now()

type server struct {
	mu  sync.Mutex
	cfg Config

	authMu   sync.Mutex
	authOK   [32]byte // sha256 of the last verified password (avoids slow PBKDF2 per request)
	authHash string
}

func serve(addr string) error {
	s := &server{cfg: loadConfig()}
	static, _ := fs.Sub(webFS, "web")

	mux := http.NewServeMux()
	mux.Handle("GET /", http.FileServerFS(static))
	mux.HandleFunc("GET /api/status", s.handleStatus)
	mux.HandleFunc("GET /api/config", s.handleGetConfig)
	mux.HandleFunc("GET /api/presets", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, presets) })
	mux.HandleFunc("GET /api/cards", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, soundCards()) })
	mux.HandleFunc("GET /api/eq/export", s.handleExport)
	mux.HandleFunc("GET /api/logs", s.handleLogs)
	mux.HandleFunc("POST /api/spotify", s.handleSpotify)
	mux.HandleFunc("POST /api/eq", s.handleEQ)
	mux.HandleFunc("POST /api/eq/parse", s.handleParse)
	mux.HandleFunc("GET /api/board", s.handleGetBoard)
	mux.HandleFunc("POST /api/board", s.handleSetBoard)
	mux.HandleFunc("POST /api/output", s.handleOutput)
	mux.HandleFunc("POST /api/password", s.handlePassword)
	mux.HandleFunc("POST /api/reboot", s.handleReboot)

	srv := &http.Server{
		Addr:              addr,
		Handler:           s.secure(mux),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    16 << 10,
	}
	log.Printf("spotifypi %s listening on %s", version, addr)
	return srv.ListenAndServe()
}

// secure adds security headers, optional password auth and CSRF protection.
func (s *server) secure(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Content-Security-Policy", "default-src 'self'; img-src 'self' https://i.scdn.co data:; style-src 'self'; script-src 'self'; frame-ancestors 'none'")
		h.Set("Cache-Control", "no-store")

		if !s.checkAuth(r) {
			h.Set("WWW-Authenticate", `Basic realm="SpotifyPi", charset="UTF-8"`)
			http.Error(w, "authentication required", http.StatusUnauthorized)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			// JSON-only + same-origin: plain cross-site forms cannot reach the API.
			if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
				http.Error(w, "expected application/json", http.StatusUnsupportedMediaType)
				return
			}
			if o := r.Header.Get("Origin"); o != "" {
				if u, err := url.Parse(o); err != nil || u.Host != r.Host {
					http.Error(w, "cross-origin request rejected", http.StatusForbidden)
					return
				}
			}
			r.Body = http.MaxBytesReader(w, r.Body, 256<<10)
		}
		next.ServeHTTP(w, r)
	})
}

func (s *server) checkAuth(r *http.Request) bool {
	s.mu.Lock()
	stored := s.cfg.PasswordHash
	s.mu.Unlock()
	if stored == "" {
		return true
	}
	_, pw, ok := r.BasicAuth()
	if !ok {
		return false
	}
	sum := sha256.Sum256([]byte(pw))
	s.authMu.Lock()
	defer s.authMu.Unlock()
	if s.authHash == stored && subtle.ConstantTimeCompare(sum[:], s.authOK[:]) == 1 {
		return true
	}
	if verifyPassword(stored, pw) {
		s.authOK, s.authHash = sum, stored
		return true
	}
	return false
}

const pbkdf2Iter = 60000

func hashPassword(pw string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key, err := pbkdf2.Key(sha256.New, pw, salt, pbkdf2Iter, 32)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("pbkdf2-sha256$%d$%s$%s", pbkdf2Iter,
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key)), nil
}

func verifyPassword(stored, pw string) bool {
	parts := strings.Split(stored, "$")
	if len(parts) != 4 || parts[0] != "pbkdf2-sha256" {
		return false
	}
	iter, err := strconv.Atoi(parts[1])
	if err != nil || iter < 1 {
		return false
	}
	salt, err1 := base64.RawStdEncoding.DecodeString(parts[2])
	want, err2 := base64.RawStdEncoding.DecodeString(parts[3])
	if err1 != nil || err2 != nil {
		return false
	}
	got, err := pbkdf2.Key(sha256.New, pw, salt, iter, len(want))
	return err == nil && subtle.ConstantTimeCompare(got, want) == 1
}

// ---------------------------------------------------------------------------

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}

func readJSON(r *http.Request, v any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("invalid request: %w", err)
	}
	return nil
}

func (s *server) snapshot() Config {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.cfg
	c.EQ.Filters = append([]Filter{}, s.cfg.EQ.Filters...)
	return c
}

// update applies fn to a copy of the config, persists it and swaps it in.
func (s *server) update(fn func(*Config) error) (Config, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.cfg
	c.EQ.Filters = append([]Filter{}, s.cfg.EQ.Filters...)
	if err := fn(&c); err != nil {
		return c, err
	}
	if err := saveConfig(c); err != nil {
		return c, err
	}
	s.cfg = c
	return c, nil
}

type publicConfig struct {
	Config
	HasPassword bool      `json:"has_password"`
	LiveEQ      *EQConfig `json:"live_eq,omitempty"`
}

func (s *server) handleGetConfig(w http.ResponseWriter, r *http.Request) {
	c := s.snapshot()
	pc := publicConfig{Config: c, HasPassword: c.PasswordHash != ""}
	pc.PasswordHash = ""
	if _, err := os.Stat(liveEQPath()); err == nil {
		e := effectiveEQ(c)
		pc.LiveEQ = &e
	}
	writeJSON(w, pc)
}

func (s *server) handleStatus(w http.ResponseWriter, r *http.Request) {
	_, liveErr := os.Stat(liveEQPath())
	writeJSON(w, map[string]any{
		"version":          version,
		"hostname":         hostname(),
		"ip":               ipAddrs(),
		"uptime_s":         int(time.Since(bootTime).Seconds()),
		"system_uptime_s":  systemUptime(),
		"librespot":        servicePID("librespot") != 0,
		"camilladsp":       servicePID("camilladsp") != 0,
		"now_playing":      readNowPlaying(),
		"eq_unsaved":       liveErr == nil,
		"settings_mounted": isMountpoint(dataDir),
		"cpu_temp_c":       cpuTemp(),
	})
}

func (s *server) handleLogs(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("service")
	switch name {
	case "librespot", "camilladsp", "spotifypi":
	default:
		writeErr(w, http.StatusBadRequest, errors.New("unknown service"))
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	io.WriteString(w, tailFile(logPath(name), 16<<10))
}

type spotifyReq struct {
	Name          string `json:"name"`
	Bitrate       int    `json:"bitrate"`
	InitialVolume int    `json:"initial_volume"`
	VolumeCtrl    string `json:"volume_ctrl"`
	Normalisation bool   `json:"normalisation"`
	Autoplay      bool   `json:"autoplay"`
	DeviceType    string `json:"device_type"`
}

func (s *server) handleSpotify(w http.ResponseWriter, r *http.Request) {
	var req spotifyReq
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	_, err := s.update(func(c *Config) error {
		c.Name, c.Bitrate, c.InitialVolume = req.Name, req.Bitrate, req.InitialVolume
		c.VolumeCtrl, c.Normalisation, c.Autoplay, c.DeviceType = req.VolumeCtrl, req.Normalisation, req.Autoplay, req.DeviceType
		return c.Validate()
	})
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if err := restartService("librespot"); err != nil {
		log.Printf("restart librespot: %v", err)
	}
	writeJSON(w, map[string]string{"status": "saved, Spotify Connect restarting"})
}

type eqReq struct {
	EQ      EQConfig `json:"eq"`
	Persist bool     `json:"persist"`
}

// handleEQ applies the EQ live (RAM only) or, with persist, saves it to the
// SD card. Live changes allow experimenting without any SD card writes.
func (s *server) handleEQ(w http.ResponseWriter, r *http.Request) {
	var req eqReq
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if err := req.EQ.Validate(); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	var cfg Config
	if req.Persist {
		var err error
		cfg, err = s.update(func(c *Config) error { c.EQ = req.EQ; return nil })
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		os.Remove(liveEQPath())
	} else if cfg = s.snapshot(); reflect.DeepEqual(req.EQ, cfg.EQ) {
		os.Remove(liveEQPath()) // back to the saved state
	} else {
		b, _ := json.Marshal(req.EQ)
		if err := writeFileAtomic(liveEQPath(), b, 0o644); err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
	}
	if err := reloadCamilla(cfg); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, map[string]any{"status": "applied", "saved": req.Persist, "eq": req.EQ})
}

func (s *server) handleParse(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Text string `json:"text"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	res, err := parsePEQ(req.Text)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, res)
}

func (s *server) handleExport(w http.ResponseWriter, r *http.Request) {
	e := effectiveEQ(s.snapshot())
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="spotifypi-peq.txt"`)
	fmt.Fprint(w, exportPEQ(e))
}

func (s *server) handleOutput(w http.ResponseWriter, r *http.Request) {
	var req struct {
		OutputDevice string `json:"output_device"`
		OutputFormat string `json:"output_format"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	_, err := s.update(func(c *Config) error {
		c.OutputDevice, c.OutputFormat = req.OutputDevice, req.OutputFormat
		return c.Validate()
	})
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	restartService("camilladsp")
	writeJSON(w, map[string]string{"status": "saved, audio output restarting"})
}

func (s *server) handlePassword(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Password string `json:"password"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if req.Password != "" && len(req.Password) < 6 {
		writeErr(w, http.StatusBadRequest, errors.New("password must be at least 6 characters"))
		return
	}
	hash := ""
	if req.Password != "" {
		var err error
		if hash, err = hashPassword(req.Password); err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
	}
	if _, err := s.update(func(c *Config) error { c.PasswordHash = hash; return nil }); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if hash == "" {
		writeJSON(w, map[string]string{"status": "password removed"})
	} else {
		writeJSON(w, map[string]string{"status": "password set (user name: anything)"})
	}
}

func (s *server) handleReboot(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]string{"status": "rebooting"})
	go func() {
		time.Sleep(500 * time.Millisecond)
		exec.Command("/sbin/reboot").Run()
	}()
}

// ---------------------------------------------------------------------------
// System info helpers

type soundCard struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Device string `json:"device"`
}

func soundCards() []soundCard {
	cards := []soundCard{}
	f, err := os.Open("/proc/asound/cards")
	if err != nil {
		return cards
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		// " 0 [sndrpihifiberry]: RPi-simple - snd_rpi_hifiberry_dac"
		line := sc.Text()
		lb, rb := strings.Index(line, "["), strings.Index(line, "]")
		if lb < 0 || rb < lb {
			continue
		}
		id := strings.TrimSpace(line[lb+1 : rb])
		if id == "Loopback" {
			continue
		}
		name := strings.TrimSpace(strings.TrimPrefix(line[rb+1:], ":"))
		cards = append(cards, soundCard{ID: id, Name: name, Device: "hw:CARD=" + id + ",DEV=0"})
	}
	return cards
}

func hostname() string { h, _ := os.Hostname(); return h }

func ipAddrs() []string {
	ips := []string{}
	addrs, _ := net.InterfaceAddrs()
	for _, a := range addrs {
		if ipn, ok := a.(*net.IPNet); ok && !ipn.IP.IsLoopback() && ipn.IP.To4() != nil {
			ips = append(ips, ipn.IP.String())
		}
	}
	return ips
}

func systemUptime() int {
	b, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return 0
	}
	f, _ := strconv.ParseFloat(strings.Fields(string(b))[0], 64)
	return int(f)
}

func cpuTemp() float64 {
	b, err := os.ReadFile("/sys/class/thermal/thermal_zone0/temp")
	if err != nil {
		return 0
	}
	v, _ := strconv.Atoi(strings.TrimSpace(string(b)))
	return float64(v) / 1000
}
