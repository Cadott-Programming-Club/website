package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"mime"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/a-h/templ"

	"website/assets"
	"website/pages"
	"website/store"
)

func main() {
	contentDir := os.Getenv("CONTENT_DIR")
	if contentDir == "" {
		contentDir = "content"
	}
	dbPath := os.Getenv("DB_PATH")
	if dbPath == "" {
		dbPath = "club.db"
	}
	st, err := store.Open(context.Background(), dbPath)
	if err != nil {
		fatal(err)
	}
	defer st.Close()
	if len(os.Args) > 1 && os.Args[1] == "import" {
		if err := importData(context.Background(), st, os.Stdin); err != nil {
			fatal(err)
		}
		return
	}
	loc, err := time.LoadLocation("America/Chicago")
	if err != nil {
		fatal(err)
	}
	a, err := setupAuth()
	if err != nil {
		fatal(err)
	}
	mime.AddExtensionType(".wasm", "application/wasm")
	mime.AddExtensionType(".pck", "application/octet-stream")

	mux := http.NewServeMux()
	setupAssetsRoutes(mux)
	routes(mux, contentDir)
	mux.HandleFunc("GET /auth/login", a.login)
	mux.HandleFunc("GET /auth/callback", a.callback)
	mux.HandleFunc("POST /auth/logout", a.logout)
	(&admin{store: st, loc: loc, advisors: parseAdvisors(os.Getenv("ADVISORS"))}).routes(mux, a)

	ln, err := listen()
	if err != nil {
		fatal(err)
	}
	srv := &http.Server{
		Handler:           logRequests(mux),
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		log.Printf("listening on http://localhost:%d", ln.Addr().(*net.TCPAddr).Port)
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			fatal(err)
		}
	}()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()
	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdown); err != nil {
		log.Print(err)
	}
}

func routes(mux *http.ServeMux, contentDir string) {
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) })
	mux.Handle("GET /{$}", page(pages.Home(facebookURL)))
	mux.Handle("GET /students/{$}", page(pages.Students(facebookURL)))
	mux.Handle("GET /2026-27/{$}", page(pages.Lessons(facebookURL, lessons)))
	mux.Handle("GET /games/{$}", page(pages.Games(facebookURL, games)))
	mux.HandleFunc("GET /2026-27/{slug}/{$}", func(w http.ResponseWriter, r *http.Request) {
		for i, l := range lessons {
			if l.Slug != r.PathValue("slug") {
				continue
			}
			var prev, next *pages.Lesson
			if i > 0 {
				prev = &lessons[i-1]
			}
			if i < len(lessons)-1 {
				next = &lessons[i+1]
			}
			render(w, r, pages.LessonPage(facebookURL, l, prev, next))
			return
		}
		http.NotFound(w, r)
	})
	mux.HandleFunc("GET /games/{slug}/{$}", func(w http.ResponseWriter, r *http.Request) {
		for _, g := range games {
			if g.Slug == r.PathValue("slug") {
				render(w, r, pages.GamePage(facebookURL, g))
				return
			}
		}
		http.NotFound(w, r)
	})
	mux.Handle("GET /2026-27/{slug}/slides.html", contentFiles(contentDir))
	mux.Handle("GET /games/{slug}/play/", contentFiles(contentDir))
	mux.Handle("GET /setup", setupScript(contentDir))
	mux.HandleFunc("GET /robots.txt", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("User-agent: *\nDisallow: /admin\nDisallow: /auth/\n"))
	})
	mux.Handle("GET /", page(pages.NotFound(facebookURL), http.StatusNotFound))
}

func page(c templ.Component, status ...int) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if len(status) > 0 {
			w.WriteHeader(status[0])
		}
		render(w, r, c)
	})
}

func render(w http.ResponseWriter, r *http.Request, c templ.Component) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := c.Render(r.Context(), w); err != nil {
		log.Print(err)
	}
}

func contentFiles(dir string) http.Handler {
	files := http.FileServer(http.Dir(dir))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := filepath.Join(dir, filepath.FromSlash(filepath.Clean("/"+r.URL.Path)))
		if strings.HasSuffix(r.URL.Path, "/") {
			path = filepath.Join(path, "index.html")
		}
		if strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			if _, err := os.Stat(path + ".gz"); err == nil {
				if ct := mime.TypeByExtension(filepath.Ext(path)); ct != "" {
					w.Header().Set("Content-Type", ct)
				}
				w.Header().Set("Content-Encoding", "gzip")
				w.Header().Set("Vary", "Accept-Encoding")
				w.Header().Set("Cache-Control", "no-cache")
				http.ServeFile(w, r, path+".gz")
				return
			}
		}
		w.Header().Set("Cache-Control", "no-cache")
		files.ServeHTTP(w, r)
	})
}

func setupScript(dir string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		http.ServeFile(w, r, filepath.Join(dir, "setup.sh"))
	})
}

func setupAuth() (*auth, error) {
	allowed := strings.Split(strings.ToLower(strings.ReplaceAll(os.Getenv("ALLOWED_EMAILS"), " ", "")), ",")
	if isDevelopment() && os.Getenv("OIDC_CLIENT_ID") == "" {
		log.Print("development: attendance pages open without login as dev@localhost")
		return &auth{devUser: "dev@localhost", allowed: []string{"dev@localhost"}}, nil
	}
	issuer := os.Getenv("OIDC_ISSUER")
	if issuer == "" {
		issuer = "https://id.cadottprogrammingclub.com"
	}
	clientID := os.Getenv("OIDC_CLIENT_ID")
	if clientID == "" || os.Getenv("BASE_URL") == "" || allowed[0] == "" {
		return nil, errors.New("OIDC_CLIENT_ID, BASE_URL and ALLOWED_EMAILS must be set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return newAuth(ctx, issuer, clientID, os.Getenv("BASE_URL"), os.Getenv("SESSION_SECRET"), allowed, !isDevelopment())
}

func parseAdvisors(s string) map[string]string {
	out := map[string]string{}
	for _, pair := range strings.Split(s, ",") {
		email, name, ok := strings.Cut(pair, "=")
		if ok {
			out[strings.ToLower(strings.TrimSpace(email))] = strings.TrimSpace(name)
		}
	}
	return out
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}

func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		if !strings.HasPrefix(r.URL.Path, "/assets/") && !strings.Contains(r.URL.Path, "/play/") {
			log.Printf("%s %s %s", r.Method, r.URL.Path, time.Since(start).Round(time.Millisecond))
		}
	})
}

func listen() (net.Listener, error) {
	if env := os.Getenv("PORT"); env != "" {
		port, err := strconv.Atoi(env)
		if err != nil {
			return nil, fmt.Errorf("invalid PORT %q", env)
		}
		return net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(port))
	}
	for port := 8090; port < 8100; port++ {
		if ln, err := net.Listen("tcp", ":"+strconv.Itoa(port)); err == nil {
			return ln, nil
		}
	}
	return nil, errors.New("no free port found from 8090 upwards")
}

func setupAssetsRoutes(mux *http.ServeMux) {
	files := assetFiles()
	mux.Handle("GET /assets/", http.StripPrefix("/assets/", cacheControl(files)))
	mux.Handle("GET /favicon.ico", cacheControl(files))
}

func assetFiles() http.Handler {
	if isDevelopment() {
		return http.FileServer(http.Dir("./assets"))
	}
	return http.FileServer(http.FS(assets.Assets))
}

func cacheControl(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")
		switch {
		case isDevelopment():
			w.Header().Set("Cache-Control", "no-store")
		case strings.HasPrefix(path, "js/shadcn-templ-"):
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		case strings.HasSuffix(path, ".jpg"), strings.HasSuffix(path, ".png"), path == "favicon.ico":
			w.Header().Set("Cache-Control", "public, max-age=86400")
		default:
			w.Header().Set("Cache-Control", "no-cache")
		}
		next.ServeHTTP(w, r)
	})
}

func isDevelopment() bool {
	return os.Getenv("SHADCN_TEMPL_DEV") == "true" || os.Getenv("TEMPL_DEV_MODE") == "true"
}
