package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"codeberg.org/reiver/go-erorr"
	"codeberg.org/reiver/go-field"

	"protocosm-space-mirror/cfg"
	"protocosm-space-mirror/srv/internalhttp"
	"protocosm-space-mirror/srv/log"
	"protocosm-space-mirror/srv/publichttp"

	_ "protocosm-space-mirror/www/private"
)

// webservers runs the two HTTP listeners of the space-mirror:
//
//	• the public listener — what the network (space hosts) calls (notifications, did.json), and
//	• the internal listener — what our own services call (reads, changes, status, admin).
//
// Both speak plain HTTP. Something in front of the public listener terminates TLS.
// The internal listener must only be bound to a private address.
func webservers() {
	log := logsrv.Begin()
	defer log.End()

	var servers []*http.Server
	{
		var publicTcpAddress string = fmt.Sprintf(":%d", cfg.HTTPPublicTcpPort())
		log.Debug(field.String("public-tcp-addr", publicTcpAddress))

		servers = append(servers, &http.Server{
			Addr:              publicTcpAddress,
			Handler:           &publichttpsrv.Mux,
			IdleTimeout:       cfg.HTTPIdleTimeOut(),
			ReadHeaderTimeout: cfg.HTTPHeaderReadTimeOut(),
		})
	}
	{
		var internalTcpAddress string = cfg.HTTPInternalTcpAddress()
		log.Debug(field.String("internal-tcp-addr", internalTcpAddress))

		servers = append(servers, &http.Server{
			Addr:              internalTcpAddress,
			Handler:           &internalhttpsrv.Mux,
			IdleTimeout:       cfg.HTTPIdleTimeOut(),
			ReadHeaderTimeout: cfg.HTTPHeaderReadTimeOut(),
		})
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-stop
		log.Inform(field.S("shutting down..."))

		ctx, cancel := context.WithTimeout(context.Background(), cfg.HTTPShutdownTimeOut())
		defer cancel()

		for _, server := range servers {
			err := server.Shutdown(ctx)
			if nil != err {
				log.Error(
					field.S("error during shutdown"),
					field.E(err),
					field.String("tcp-addr", server.Addr),
				)
			}
		}
	}()

	var waitGroup sync.WaitGroup
	for _, server := range servers {
		waitGroup.Add(1)
		go func(server *http.Server) {
			defer waitGroup.Done()
			_webserver(server)
		}(server)
	}
	waitGroup.Wait()
}

func _webserver(server *http.Server) {
	log := logsrv.Begin()
	defer log.End()

	if nil == server {
		log.Alert(field.S("nil HTTP server"))
		return
	}

	err := server.ListenAndServe()
	if nil != err && !erorr.Is(err, http.ErrServerClosed) {
		log.Alert(
			field.S("HTTP server error"),
			field.E(err),
			field.String("tcp-addr", server.Addr),
		)
	}
}
