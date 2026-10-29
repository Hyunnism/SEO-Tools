package serve

import (
	"crypto/subtle"
	"fmt"
	"net"
	"net/http"
	"strings"

	"seo-boost/internal/config"
	"seo-boost/internal/store"
)

// Server mengelola endpoint HTTP untuk dashboard web dan API REST lokal.
type Server struct {
	cfg         *config.Config
	st          *store.Store
	jobs        *JobManager
	runner      Runner
	token       string
	requireAuth bool
	mux         *http.ServeMux
}

// NewServer membuat instance Server baru beserta rute dan middleware.
func NewServer(cfg *config.Config, st *store.Store, runner Runner, token string, requireAuth bool) *Server {
	s := &Server{
		cfg:         cfg,
		st:          st,
		jobs:        NewJobManager(),
		runner:      runner,
		token:       token,
		requireAuth: requireAuth,
		mux:         http.NewServeMux(),
	}
	s.registerRoutes()
	return s
}

// Handler mengembalikan http.Handler lengkap yang dibungkus middleware keamanan.
func (s *Server) Handler() http.Handler {
	return s.securityHeaders(s.authMiddleware(s.mux))
}

// IsLoopback memeriksa apakah alamat jaringan yang diberikan mengarah ke loopback/localhost.
func IsLoopback(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}
	if host == "" || host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	if ip != nil {
		return ip.IsLoopback()
	}
	return false
}

// ValidateAddr memverifikasi apakah kombinasi binding address dan token aman.
// Bind non-loopback wajib memiliki token autentikasi.
func ValidateAddr(addr string, token string, requireAuth bool) error {
	if !IsLoopback(addr) && (!requireAuth || token == "") {
		return fmt.Errorf("alamat non-loopback %q wajib menggunakan token autentikasi demi keamanan", addr)
	}
	return nil
}

func (s *Server) registerRoutes() {
	// Dashboard Web
	s.mux.HandleFunc("GET /{$}", s.handleDashboard)

	// API REST
	s.mux.HandleFunc("GET /api/sites", s.handleAPISites)
	s.mux.HandleFunc("GET /api/sites/{name}/pages", s.handleAPISitePages)
	s.mux.HandleFunc("GET /api/sites/{name}/gaps", s.handleAPISiteGaps)
	s.mux.HandleFunc("GET /api/sites/{name}/runs", s.handleAPISiteRuns)
	s.mux.HandleFunc("GET /api/sites/{name}/runs/{id}", s.handleAPISiteRunDetail)
	s.mux.HandleFunc("POST /api/sites/{name}/run", s.handleAPISiteTriggerRun)
	s.mux.HandleFunc("GET /api/runs/{id}", s.handleAPIRunProgress)
}

func (s *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("X-XSS-Protection", "1; mode=block")
		next.ServeHTTP(w, r)
	})
}

func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.requireAuth || s.token == "" {
			next.ServeHTTP(w, r)
			return
		}

		// Ambil token dari berbagai kemungkinan sumber (query, header, cookie)
		clientToken := ""

		// 1. Query parameter (?token=... atau ?t=...)
		if qToken := r.URL.Query().Get("token"); qToken != "" {
			clientToken = qToken
		} else if qT := r.URL.Query().Get("t"); qT != "" {
			clientToken = qT
		}

		// 2. Header Authorization Bearer
		if clientToken == "" {
			authHeader := r.Header.Get("Authorization")
			if strings.HasPrefix(strings.ToLower(authHeader), "bearer ") {
				clientToken = strings.TrimSpace(authHeader[7:])
			}
		}

		// 3. Header X-Auth-Token
		if clientToken == "" {
			clientToken = r.Header.Get("X-Auth-Token")
		}

		// 4. Cookie seo_boost_token
		if clientToken == "" {
			if cookie, err := r.Cookie("seo_boost_token"); err == nil {
				clientToken = cookie.Value
			}
		}

		// Bandingkan secara constant-time untuk mencegah timing attack
		if subtle.ConstantTimeCompare([]byte(clientToken), []byte(s.token)) != 1 {
			if strings.HasPrefix(r.URL.Path, "/api/") {
				writeJSON(w, http.StatusUnauthorized, map[string]string{
					"error": "unauthorized: token autentikasi tidak valid atau belum diberikan",
				})
			} else {
				http.Error(w, "[ERR] 401 Unauthorized: Akses ditolak. Berikan parameter ?token=...", http.StatusUnauthorized)
			}
			return
		}

		// Bila berhasil via query param, set cookie agar request berikutnya otomatis lolos
		if r.URL.Query().Get("token") != "" || r.URL.Query().Get("t") != "" {
			http.SetCookie(w, &http.Cookie{
				Name:     "seo_boost_token",
				Value:    s.token,
				Path:     "/",
				HttpOnly: true,
				SameSite: http.SameSiteLaxMode,
			})
		}

		next.ServeHTTP(w, r)
	})
}
