/*
Copyright 2016 The GoStor Authors All rights reserved.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package apiserver contains the code that provides a rest.ful API service.
package apiserver

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"

	systemdActivation "github.com/coreos/go-systemd/activation"
	"github.com/docker/go-connections/sockets"
	"github.com/gorilla/mux"
	"github.com/gostor/gotgt/pkg/apiserver/httputils"
	"github.com/gostor/gotgt/pkg/apiserver/router"
	"github.com/gostor/gotgt/pkg/apiserver/router/discovery"
	"github.com/gostor/gotgt/pkg/apiserver/router/lu"
	"github.com/gostor/gotgt/pkg/apiserver/router/target"
	log "github.com/sirupsen/logrus"
	"golang.org/x/net/context"
)

// versionMatcher defines a variable matcher to be parsed by the router
// when a request is about to be served.
const versionMatcher = "/v{version:[0-9.]+}"

// Config provides the configuration for the API server
type Config struct {
	Logging                  bool
	EnableCors               bool
	CorsHeaders              string
	AuthorizationPluginNames []string
	Version                  string
	SocketGroup              string
	TLSConfig                *tls.Config
	Addrs                    []Addr
	APIrouter                string
}

// Addr contains string representation of address and its protocol (tcp, unix...).
type Addr struct {
	Proto string
	Addr  string
}

// Server contains instance details for the server
type Server struct {
	cfg           *Config
	servers       []*HTTPServer
	routers       []router.Router
	routerSwapper *routerSwapper
}

// New returns a new instance of the server based on the specified configuration.
// It allocates resources which will be needed for ServeAPI(ports, unix-sockets).
func New(cfg *Config) (*Server, error) {
	s := &Server{
		cfg: cfg,
	}
	for _, addr := range cfg.Addrs {
		srv, err := s.newServer(addr.Proto, addr.Addr)
		if err != nil {
			return nil, err
		}
		log.Infof("Server created for HTTP on %s (%s)", addr.Proto, addr.Addr)
		s.servers = append(s.servers, srv...)
	}
	return s, nil
}

// Close closes servers and thus stop receiving requests
func (s *Server) Close() {
	for _, srv := range s.servers {
		if err := srv.Close(); err != nil {
			log.Error(err)
		}
	}
}

// serveAPI loops through all initialized servers and spawns goroutine
// with Server method for each. It sets createMux() as Handler also.
func (s *Server) serveAPI() error {
	s.initRouterSwapper()

	var chErrors = make(chan error, len(s.servers))
	for _, srv := range s.servers {
		srv.srv.Handler = s.routerSwapper
		go func(srv *HTTPServer) {
			var err error
			log.Infof("API listen on %s", srv.l.Addr())
			if err = srv.Serve(); err != nil && strings.Contains(err.Error(), "use of closed network connection") {
				err = nil
			}
			chErrors <- err
		}(srv)
	}

	for i := 0; i < len(s.servers); i++ {
		err := <-chErrors
		if err != nil {
			return err
		}
	}

	return nil
}

// HTTPServer contains an instance of http server and the listener.
// srv *http.Server, contains configuration to create a http server and a mux router with all api end points.
// l   net.Listener, is a TCP or Socket listener that dispatches incoming request to the router.
type HTTPServer struct {
	srv *http.Server
	l   net.Listener
}

// Serve starts listening for inbound requests.
func (s *HTTPServer) Serve() error {
	return s.srv.Serve(s.l)
}

// Close closes the HTTPServer from listening for the inbound requests.
func (s *HTTPServer) Close() error {
	return s.l.Close()
}

func (s *Server) initTCPSocket(addr string) (l net.Listener, err error) {
	if s.cfg.TLSConfig == nil || s.cfg.TLSConfig.ClientAuth != tls.RequireAndVerifyClientCert {
		log.Warning("/!\\ DON'T BIND ON ANY IP ADDRESS WITHOUT setting -tlsverify IF YOU DON'T KNOW WHAT YOU'RE DOING /!\\")
	}
	if l, err = sockets.NewTCPSocket(addr, s.cfg.TLSConfig); err != nil {
		return nil, err
	}

	return l, nil
}

func (s *Server) makeHTTPHandler(handler httputils.APIFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// log the handler call
		log.Infof("Calling %s %s", r.Method, r.URL.Path)

		// Define the context that we'll pass around to share info
		// like the docker-request-id.
		//
		// The 'context' will be used for global data that should
		// apply to all requests. Data that is specific to the
		// immediate function being called should still be passed
		// as 'args' on the function call.
		ctx := context.Background()
		handlerFunc := s.handleWithGlobalMiddlewares(handler)

		vars := mux.Vars(r)
		if vars == nil {
			vars = make(map[string]string)
		}

		if err := handlerFunc(ctx, w, r, vars); err != nil {
			log.Errorf("Handler for %s %s returned error: %v", r.Method, r.URL.Path, err)
			httputils.WriteError(w, err)
		}
	}
}

// InitRouters initializes a list of routers for the server.
func (s *Server) InitRouters() {
	s.addRouter(target.NewRouter())
	s.addRouter(lu.NewRouter())
	s.addRouter(discovery.NewRouter())
}

// addRouter adds a new router to the server.
func (s *Server) addRouter(r router.Router) {
	s.routers = append(s.routers, r)
}

// createMux initializes the main router the server uses.
// we keep enableCors just for legacy usage, need to be removed in the future
func (s *Server) createMux() *mux.Router {
	m := mux.NewRouter()

	log.Infof("Registering routers")
	for _, apiRouter := range s.routers {
		for _, r := range apiRouter.Routes() {
			f := s.makeHTTPHandler(r.Handler())

			log.Infof("Registering %s, %s", r.Method(), r.Path())
			m.Path(versionMatcher + r.Path()).Methods(r.Method()).Handler(f)
			m.Path(r.Path()).Methods(r.Method()).Handler(f)
		}
	}

	return m
}

// Wait blocks the server goroutine until it exits.
// It sends an error message if there is any error during
// the API execution.
func (s *Server) Wait(waitChan chan error) {
	if err := s.serveAPI(); err != nil {
		log.Errorf("ServeAPI error: %v", err)
		waitChan <- err
		return
	}
	waitChan <- nil
}

func (s *Server) initRouterSwapper() {
	s.routerSwapper = &routerSwapper{
		router: s.createMux(),
	}
}

func (s *Server) handleWithGlobalMiddlewares(handler httputils.APIFunc) httputils.APIFunc {
	return handler
}

// newServer sets up the required HTTPServers and does protocol specific checking.
// newServer does not set any muxers, you should set it later to Handler field
func (s *Server) newServer(proto, addr string) ([]*HTTPServer, error) {
	var (
		err error
		ls  []net.Listener
	)
	switch proto {
	case "fd":
		ls, err = listenFD(addr, s.cfg.TLSConfig)
		if err != nil {
			return nil, err
		}
	case "tcp":
		l, err := s.initTCPSocket(addr)
		if err != nil {
			return nil, err
		}
		ls = append(ls, l)
	default:
		return nil, fmt.Errorf("Invalid protocol format: %q", proto)
	}
	var res []*HTTPServer
	for _, l := range ls {
		res = append(res, &HTTPServer{
			&http.Server{
				Addr: addr,
			},
			l,
		})
	}
	return res, nil
}

// listenFD returns the specified socket activated files as a slice of
// net.Listeners or all of the activated files if "*" is given.
func listenFD(addr string, tlsConfig *tls.Config) ([]net.Listener, error) {
	var (
		err       error
		listeners []net.Listener
	)
	// socket activation
	if tlsConfig != nil {
		listeners, err = systemdActivation.TLSListeners(tlsConfig)
	} else {
		listeners, err = systemdActivation.Listeners()
	}
	if err != nil {
		return nil, err
	}

	if len(listeners) == 0 {
		return nil, fmt.Errorf("No sockets found")
	}

	// default to all fds just like unix:// and tcp://
	if addr == "" || addr == "*" {
		return listeners, nil
	}

	fdNum, err := strconv.Atoi(addr)
	if err != nil {
		return nil, fmt.Errorf("failed to parse systemd address, should be number: %v", err)
	}
	fdOffset := fdNum - 3
	if len(listeners) < int(fdOffset)+1 {
		return nil, fmt.Errorf("Too few socket activated files passed in")
	}
	if listeners[fdOffset] == nil {
		return nil, fmt.Errorf("failed to listen on systemd activated file at fd %d", fdOffset+3)
	}
	for i, ls := range listeners {
		if i == fdOffset || ls == nil {
			continue
		}
		if err := ls.Close(); err != nil {
			log.Errorf("Failed to close systemd activated file at fd %d: %v", fdOffset+3, err)
		}
	}
	return []net.Listener{listeners[fdOffset]}, nil
}
