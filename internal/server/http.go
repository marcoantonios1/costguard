package server

import (
	"context"
	"net/http"
	"time"
)

type Server struct {
	httpServer *http.Server
}

type Deps struct {
	Addr string
	Handler http.Handler
}

func NewServer(d Deps) *Server {
	s := &http.Server{
		Addr:              d.Addr,
		Handler:           d.Handler,
		ReadHeaderTimeout: 10 * time.Second,
	}
	return &Server{httpServer: s}
}

func (s *Server) ListenAndServe() error {
	return s.httpServer.ListenAndServe()
}

func (s *Server) Shutdown(ctx context.Context) error {
	return s.httpServer.Shutdown(ctx)
}