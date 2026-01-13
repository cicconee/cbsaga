package api

import (
	"context"

	orchestratorv1 "github.com/cicconee/cbsaga/gen/orchestrator/v1"
	"github.com/cicconee/cbsaga/internal/orchestrator/app"
	"github.com/cicconee/cbsaga/internal/platform/logging"
	"google.golang.org/grpc"
)

func Register(gs *grpc.Server, svc *app.Service, log *logging.Logger) {
	orchestratorv1.RegisterOrchestratorServiceServer(gs, NewHandler(svc, log))

	gs.RegisterService(&grpc.ServiceDesc{
		ServiceName: "cbsaga.orchestrator.v1.DevTools",
		HandlerType: (any)(nil),
		Methods: []grpc.MethodDesc{
			{
				MethodName: "Ping",
				Handler: func(_ any, ctx context.Context, dec func(any) error, _ grpc.UnaryServerInterceptor) (any, error) {
					return &struct{ Ok bool }{Ok: true}, nil
				},
			},
		},
		Streams:  []grpc.StreamDesc{},
		Metadata: "devtools",
	}, nil)
}
