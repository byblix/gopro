package main

import (
	"flag"

	postgres "github.com/byblix/gopro/storage/postgres"
	"github.com/joho/godotenv"
	"github.com/sirupsen/logrus"
)

var db postgres.Service

var log = newLogger()

func newLogger() *logrus.Logger {
	logger := logrus.StandardLogger()
	logrus.SetLevel(logrus.DebugLevel)
	logrus.SetFormatter(&logrus.TextFormatter{
		ForceColors:    true,
		FullTimestamp:  true,
		DisableSorting: true,
	})
	return logger
}

var (
	local = flag.Bool("local", false, "Do you want to run go run *.go?")
	host  = flag.String("host", "", "What host are you using?")
	// ? not yet in use
	production = flag.Bool("production", false, "Is it production?")
)

func init() {
	// type go run *.go -local
	flag.Parse()
	if *local {
		log.Info("Running locally")
		if err := godotenv.Load(); err != nil {
			log.Fatalln(err)
		}
	}
}

func main() {
	svc, err := postgres.NewPQ()
	if err != nil {
		log.Fatalf("POSTGRESQL err: %s", err)
	}
	db = svc
	defer svc.Close()
	s := newServer()

	// Serve on localhost with localhost certs if no host provided
	if *host == "" {
		s.httpsSrv.Addr = "localhost:8085"
		log.Info("Serving on http://localhost:8085")
		// if err := s.httpsSrv.ListenAndServeTLS("./certs/insecure_cert.pem", "./certs/insecure_key.pem"); err != nil {
		if err := s.httpsSrv.ListenAndServe(); err != nil {
			log.Fatal(err)
		}
	}

	if err := s.useHTTP2(); err != nil {
		log.Warnf("Error with HTTP2 %s", err)
	}

	// Start a reg. HTTP on a new thread
	go func() {
		log.Info("Running http server")
		if err := s.httpSrv.ListenAndServe(); err != nil {
			log.Fatal(err)
		}
	}()

	// Set TLS cert
	s.httpsSrv.TLSConfig.GetCertificate = s.certm.GetCertificate
	log.Info("Serving on https, authenticating for https://", *host)
	if err := s.httpsSrv.ListenAndServeTLS("", ""); err != nil {
		log.Fatal(err)
	}
}
