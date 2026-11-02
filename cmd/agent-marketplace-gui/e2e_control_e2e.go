//go:build cgo && e2e && (darwin || linux)

package main

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"time"
)

const defaultE2EControlAddress = "127.0.0.1:47623"

func startE2EControl(app *App) (func(), error) {
	address := os.Getenv("AGENT_MARKETPLACE_E2E_CONTROL_ADDR")
	if address == "" {
		address = defaultE2EControlAddress
	}
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	ip := net.ParseIP(host)
	if host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return nil, errors.New("E2E control address must be loopback")
	}
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return nil, err
	}
	mux := http.NewServeMux()
	mux.Handle("/api/", app.handler())
	mux.HandleFunc("/__e2e/bootstrap", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(app.Bootstrap())
	})
	mux.HandleFunc("/__e2e/quit", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.RawQuery != "" {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.WriteHeader(http.StatusAccepted)
		go func() {
			time.Sleep(100 * time.Millisecond)
			app.Quit()
		}()
	})
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		_ = server.Serve(listener)
	}()
	return func() {
		_ = server.Close()
	}, nil
}
