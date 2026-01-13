package grpcserver

import (
	"net"
	"time"

	"github.com/cicconee/cbsaga/internal/platform/logging"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"
)

type Server struct {
	grpcServer *grpc.Server
	lis        net.Listener
}

type Options struct {
	Addr                string
	GracefulStopTimeout time.Duration
}

func New(opts Options, log *logging.Logger, register func(s *grpc.Server)) (*Server, error) {
	lis, err := net.Listen("tcp", opts.Addr)
	if err != nil {
		return nil, err
	}

	gs := grpc.NewServer()

	hs := health.NewServer()
	hs.SetServingStatus("", grpc_health_v1.HealthCheckResponse_SERVING)
	grpc_health_v1.RegisterHealthServer(gs, hs)

	reflection.Register(gs)

	if register != nil {
		register(gs)
	}

	log.Info("gRPC server created", "addr", opts.Addr)

	return &Server{
		grpcServer: gs,
		lis:        lis,
	}, nil
}

func (s *Server) Serve(log *logging.Logger) error {
	log.Info("gRPC server starting")
	return s.grpcServer.Serve(s.lis)
}

func (s *Server) GracefulStop(log *logging.Logger) {
	log.Info("gRPC server graceful stopping")
	s.grpcServer.GracefulStop()
	log.Info("gRPC server stopped")
}
