/*
 Copyright 2023 The Kapacity Authors.

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

package grpc

import (
	"context"
	"fmt"
	"net"

	"google.golang.org/grpc"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

// Server is a leader election runnable gRPC server.
type Server interface {
	manager.Runnable
	manager.LeaderElectionRunnable
	// ServiceRegistrar returns the gRPC service registrar of the server.
	ServiceRegistrar() grpc.ServiceRegistrar
}

// NewServer creates a new Server with the given bind address and gRPC server options.
func NewServer(addr string, opts ...grpc.ServerOption) Server {
	return &server{
		bindAddress: addr,
		grpcServer:  grpc.NewServer(opts...),
	}
}

type server struct {
	bindAddress string
	grpcServer  *grpc.Server
}

func (s *server) Start(context.Context) error {
	ln, err := net.Listen("tcp", s.bindAddress)
	if err != nil {
		return fmt.Errorf("failed to listening on %s: %v", s.bindAddress, err)
	}
	if err := s.grpcServer.Serve(ln); err != nil {
		return fmt.Errorf("failed to serve gRPC: %v", err)
	}
	return nil
}

func (*server) NeedLeaderElection() bool {
	return false
}

func (s *server) ServiceRegistrar() grpc.ServiceRegistrar {
	return s.grpcServer
}
