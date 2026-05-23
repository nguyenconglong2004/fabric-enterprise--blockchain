package main

import (
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"

	"raft-order-service/internal/orchestrator"
	webembed "raft-order-service/web"
)

func main() {
	listenAddr := flag.String("addr", ":8080", "HTTP listen address")
	dev := flag.Bool("dev", false, "dev mode: proxy static files to Vite dev server")
	staticProxy := flag.String("static-proxy", "http://localhost:5173", "Vite dev server URL (only used with --dev)")
	flag.Parse()

	bus := orchestrator.NewEventBus()
	mgr := orchestrator.NewNodeManager(bus)

	mux := http.NewServeMux()
	orchestrator.RegisterRoutes(mux, mgr, bus)

	if *dev {
		target, err := url.Parse(*staticProxy)
		if err != nil {
			log.Fatalf("invalid static-proxy URL: %v", err)
		}
		proxy := httputil.NewSingleHostReverseProxy(target)
		mux.Handle("/", proxy)
		log.Printf("Dev mode: proxying static files to %s", *staticProxy)
	} else {
		sub, err := fs.Sub(webembed.Dist, "dist")
		if err != nil {
			log.Fatalf("embed sub failed: %v", err)
		}
		mux.Handle("/", http.FileServer(http.FS(sub)))
	}

	fmt.Printf("Raft Orchestrator listening on http://localhost%s\n", *listenAddr)
	if err := http.ListenAndServe(*listenAddr, mux); err != nil {
		log.Fatal(err)
	}
}
