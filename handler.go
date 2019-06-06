package main

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/rs/cors"

	mux "github.com/gorilla/mux"

	"github.com/byblix/gopro/mailtips"
	"github.com/byblix/gopro/slack"
	postgres "github.com/byblix/gopro/storage/postgres"
	"github.com/byblix/gopro/upload/exif"
	"github.com/dgrijalva/jwt-go"
	"github.com/sirupsen/logrus"
	"golang.org/x/crypto/acme/autocert"
	"golang.org/x/net/context"
	"golang.org/x/net/http2"
)

// Server -
type Server struct {
	httpsSrv *http.Server
	httpSrv  *http.Server
	certm    *autocert.Manager
	// handlermux http.Handler
}

var jwtKey = []byte("thiskeyiswhat")

// Creates a new server with H2 & HTTPS
func newServer() *Server {
	mux := mux.NewRouter()
	// ? Public endpoints
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooEarly)
		fmt.Fprintln(w, "Nothing to see here :-)")
	}).Methods("GET")
	mux.HandleFunc("/authenticate", generateJWT).Methods("POST")
	mux.HandleFunc("/reauthenticate", isJWTAuth(generateJWT)).Methods("GET")

	// * Private endpoints
	mux.HandleFunc("/secure", isJWTAuth(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("Secure msg from gopro service"))
	})).Methods("GET")

	mux.HandleFunc("/mail/send", isJWTAuth(mailtips.MailHandler)).Methods("POST")
	mux.HandleFunc("/slack/tip", isJWTAuth(slack.PostSlackMsg)).Methods("POST")
	mux.HandleFunc("/exif", isJWTAuth(getExif)).Methods("POST")
	mux.HandleFunc("/media", isJWTAuth(getMedias)).Methods("GET")
	mux.HandleFunc("/media/{id}", isJWTAuth(getMediaByID)).Methods("GET")
	mux.HandleFunc("/media", isJWTAuth(createMedia)).Methods("POST")

	c := cors.New(cors.Options{
		AllowedOrigins:   []string{"http://localhost:4200"},
		AllowedMethods:   []string{"GET", "PUT", "POST", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Content-Type", "X-Requested-By", "Set-Cookie", "user_token", "pro_token"},
		AllowCredentials: true,
	})

	// https://medium.com/weareservian/automagical-https-with-docker-and-go-4953fdaf83d2
	m := autocert.Manager{
		Prompt:     autocert.AcceptTOS,
		HostPolicy: autocert.HostWhitelist(*host),
		Cache:      autocert.DirCache("/certs"),
	}

	httpsSrv := &http.Server{
		ReadTimeout:       5 * time.Second,
		WriteTimeout:      10 * time.Second,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20,
		Addr:              ":https",
		TLSConfig: &tls.Config{
			PreferServerCipherSuites: true,
			CurvePreferences: []tls.CurveID{
				tls.CurveP256,
				tls.X25519,
			},
		},
		Handler: c.Handler(mux),
	}

	// Create server for redirecting HTTP to HTTPS
	httpSrv := &http.Server{
		Addr:         ":http",
		ReadTimeout:  httpsSrv.ReadTimeout,
		WriteTimeout: httpsSrv.WriteTimeout,
		IdleTimeout:  httpsSrv.IdleTimeout,
		Handler:      m.HTTPHandler(nil),
	}

	return &Server{
		httpsSrv: httpsSrv,
		httpSrv:  httpSrv,
		certm:    &m,
	}
}

// JWTCreds for at user to get JWT
type JWTCreds struct {
	Username string `json:"username"`
}

func generateJWT(w http.ResponseWriter, r *http.Request) {
	// w.Header().Set("Content-Type", "text/html")
	if r.Method == "POST" {
		var creds JWTCreds
		if err := decodeJSON(r.Body, creds); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		exp := time.Now().Add(tokenExpirationTime)
		claims := &Claims{
			Username: creds.Username,
			Claims: jwt.StandardClaims{
				ExpiresAt: exp.Unix(),
			},
		}
		token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims.Claims)
		signedToken, err := token.SignedString(jwtKey)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		http.SetCookie(w, &http.Cookie{
			Name:    "pro_token",
			Expires: time.Now().Add(tokenExpirationTime),
			Value:   signedToken,
			Path:    "/",
			// HttpOnly: true,
			Secure: false,
		})

		if err := json.NewEncoder(w).Encode(signedToken); err != nil {
			logrus.Warn(err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
}

func getMediaByID(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		params := mux.Vars(r)
		ctx, cancel := context.WithTimeout(r.Context(), time.Second*5)
		defer cancel()
		val, err := db.GetMediaByID(ctx, params["id"])
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
		}
		if err := json.NewEncoder(w).Encode(val); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}
}

func createMedia(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		r.Header.Set("content-type", "application/json")
		ctx, cancel := context.WithTimeout(r.Context(), time.Second*5)
		defer cancel()
		var media postgres.Media
		if err := json.NewDecoder(r.Body).Decode(&media); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
		}

		id, err := db.CreateMedia(ctx, &media)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		err = json.NewEncoder(w).Encode(id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}
}

// getMedias endpoint: /medias
func getMedias(w http.ResponseWriter, r *http.Request) {
	r.Header.Set("content-type", "application/json")
	ctx, cancel := context.WithTimeout(r.Context(), time.Second*3)
	defer cancel()
	// todo: params
	medias, err := db.GetMedias(ctx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
	if err := json.NewEncoder(w).Encode(medias); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// HandleImage recieves body
func getExif(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "image/*")
	defer r.Body.Close()
	ch := make(chan []byte)
	var wg sync.WaitGroup
	_, cancel := context.WithTimeout(r.Context(), time.Duration(time.Second*10))
	defer cancel()

	exif, err := exif.NewExif(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	go func() {
		wg.Add(1)
		exif.TagExif(&wg, ch)
		wg.Wait()
	}()
	jsonVAL := <-ch
	w.Write(jsonVAL)
}

/**
 * * What is the relationship between media and department?
 * * What should be shown to the user of these ^?
 * ! implement context in all server>db calls
 */

func (s *Server) useHTTP2() error {
	http2Srv := http2.Server{}
	err := http2.ConfigureServer(s.httpsSrv, &http2Srv)
	if err != nil {
		return err
	}
	return nil
}

func decodeJSON(r io.Reader, val JWTCreds) error {
	err := json.NewDecoder(r).Decode(&val)
	if err != nil {
		return err
	}
	return nil
}
