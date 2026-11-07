// Binny is a small, single-user file host: a file explorer in the browser for the files in one
// folder, with tags, a trash and upload from link.
//
//	BINNY_PASSWORD=... [BINNY_DATA_DIR=data] binny [-host 127.0.0.1] [-port 5000]
package main

import (
	"cmp"
	"errors"
	"flag"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/cavecomputing/binny/internal/auth"
	"github.com/cavecomputing/binny/internal/db"
	"github.com/cavecomputing/binny/internal/downloader"
	"github.com/cavecomputing/binny/internal/server"
	"github.com/cavecomputing/binny/internal/storage"
	"github.com/cavecomputing/binny/web"
)

func main() {
	host := flag.String("host", "127.0.0.1", "the address to listen on")
	port := flag.Int("port", 5000, "the port to listen on")
	flag.Parse()
	if err := run(net.JoinHostPort(*host, strconv.Itoa(*port))); err != nil {
		slog.Error(err.Error())
		os.Exit(1)
	}
}

func run(addr string) error {
	password := os.Getenv("BINNY_PASSWORD")
	if password == "" {
		return errors.New("set BINNY_PASSWORD to the password that signs devices in")
	}
	dataDir := cmp.Or(os.Getenv("BINNY_DATA_DIR"), "data")
	if err := os.MkdirAll(filepath.Join(dataDir, "files"), 0o777); err != nil {
		return err
	}
	disk, err := storage.OpenDisk(filepath.Join(dataDir, "files"))
	if err != nil {
		return err
	}
	store, err := db.Open(filepath.Join(dataDir, "binny.db"), disk)
	if err != nil {
		return err
	}
	secret, err := store.SecretKey()
	if err != nil {
		return err
	}
	// The startup scan runs in the background, so a big library doesn't hold up the first page.
	go func() {
		if err := store.IndexTree(""); err != nil {
			slog.Error("Indexing data/files/ failed", "err", err)
			return
		}
		slog.Info("Indexed data/files/")
	}()

	login := auth.New(secret, password, web.Page("login.html"))
	handler := server.New(disk, store, login, downloader.New(disk, store))
	slog.Info("Binny is listening on http://" + addr)
	srv := &http.Server{Addr: addr, Handler: handler, ReadHeaderTimeout: 30 * time.Second}
	return srv.ListenAndServe()
}
