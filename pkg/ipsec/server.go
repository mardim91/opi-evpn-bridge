// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2023-2024 Intel Corporation, or its subsidiaries.
// Copyright (C) 2024 Ericsson AB.

// Package ipsec is the main package of the application
package ipsec

import (
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"

	pb "github.com/opiproject/opi-evpn-bridge/pkg/ipsec/gen/go"
)

// Server represents the Server object
type Server struct {
	pb.UnimplementedIPUIPSecServer
	tracer trace.Tracer
}

// NewServer creates initialized instance of EVPN server
func NewServer() *Server {
	return &Server{
		tracer: otel.Tracer(""),
	}
}
