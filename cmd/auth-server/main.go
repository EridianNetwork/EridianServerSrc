package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"eridian/internal/db"
	"eridian/internal/online"
	"eridian/pkg/runlog"
)

func main() {
	if f := runlog.Setup("auth-server"); f != nil {
		defer f.Close()
	}

	host := flag.String("host", "0.0.0.0", "host to listen on")
	port := flag.Int("port", 443, "port to listen on")
	dbPath := flag.String("db", "borderlands.db", "path to sqlite database")
	certFile := flag.String("cert", "cert.pem", "TLS certificate file")
	keyFile := flag.String("key", "key.pem", "TLS key file")
	flag.Parse()

	if err := db.Open(*dbPath); err != nil {
		log.Fatalf("[AUTH] database open: %v", err)
	}
	defer db.Close()

	if err := db.RunMigrations(); err != nil {
		log.Fatalf("[AUTH] database migrate: %v", err)
	}
	if err := db.MigrateAccountTesterFlags(); err != nil {
		log.Fatalf("[AUTH] database migrate account tester flags: %v", err)
	}

	svr := online.NewAuthServer(*host, *port, *certFile, *keyFile)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigCh
		log.Printf("[AUTH] received %v, shutting down...", sig)
		svr.Shutdown()
	}()

	log.Printf("[AUTH] starting auth server on %s:%d", *host, *port)
	if err := svr.Serve(); err != nil {
		log.Fatalf("[AUTH] server error: %v", err)
	}
}
