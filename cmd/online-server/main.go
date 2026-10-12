package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

		"eridian/internal/db"
	"eridian/internal/online"
	"eridian/internal/online/services"
	"eridian/pkg/runlog"
)

func main() {
	if f := runlog.Setup("online-server"); f != nil {
		defer f.Close()
	}

	host := flag.String("host", "0.0.0.0", "host to listen on")
	port := flag.Int("port", 5055, "port to listen on")
	maxConns := flag.Int("max-conns", 100, "maximum concurrent connections")
	dbPath := flag.String("db", "borderlands.db", "path to sqlite database")
	flag.Parse()

	// Initialize database
	if err := db.Open(*dbPath); err != nil {
		log.Fatalf("[ONLINE] database open: %v", err)
	}
	defer db.Close()

	if err := db.RunMigrations(); err != nil {
		log.Fatalf("[ONLINE] database migrate: %v", err)
	}
	if err := db.MigrateAccountTesterFlags(); err != nil {
		log.Fatalf("[ONLINE] database migrate account tester flags: %v", err)
	}

	// Create online server
	svr := online.NewServer(*host, *port, *maxConns)

	// Register all service handlers
	services.RegisterCharacterService(svr)
	services.RegisterInventoryService(svr)
	services.RegisterLobbyService(svr)
	services.RegisterChatService(svr)
	services.RegisterFriendService(svr)

	// Handle graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigCh
		log.Printf("[ONLINE] received %v, shutting down...", sig)
		svr.Shutdown()
	}()

	log.Printf("[ONLINE] starting server on %s:%d", *host, *port)
	if err := svr.Serve(); err != nil {
		log.Fatalf("[ONLINE] server error: %v", err)
	}
}
