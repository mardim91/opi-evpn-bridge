// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2023-2024 Intel Corporation, or its subsidiaries.
// Copyright (C) 2024 Ericsson AB.

// Package ipsec is the main package of the application
package ipsec

import (
	"context"
	"fmt"
	"log"

	"github.com/opiproject/opi-evpn-bridge/pkg/infradb"
	pb "github.com/opiproject/opi-evpn-bridge/pkg/ipsec/gen/go"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// AddSA executes the addition of the SA to the SAD.
// This function does install a single SA for a single protocol in one direction.
func (s *Server) AddSA(_ context.Context, in *pb.AddSAReq) (*pb.AddSAResp, error) {
	// Generate new SA id
	name, err := s.createSAName(in.SaId)
	if err != nil {
		log.Printf("AddSA(): Name creation failure: %v", err)
		return &pb.AddSAResp{Stat: pb.Status_FAILED}, err
	}

	err = s.getSA(name)
	if err != nil {
		if err != infradb.ErrKeyNotFound {
			log.Printf("AddSA(): Failed to interact with store: %v", err)
			return &pb.AddSAResp{Stat: pb.Status_FAILED}, err
		}
	} else {
		err := fmt.Errorf("AddSA(): SA with id %v already exists", in.SaId)
		return &pb.AddSAResp{Stat: pb.Status_FAILED}, err
	}

	// Store the domain object into DB
	err = s.createSA(name, in)
	if err != nil {
		log.Printf("AddSA(): SA with id %v, Add SA to DB failure: %v", in.SaId, err)
		if e, ok := status.FromError(err); ok {
			switch e.Code() {
			case codes.InvalidArgument:
				return &pb.AddSAResp{Stat: pb.Status_INVALID_ARG}, err
			case codes.Unimplemented:
				return &pb.AddSAResp{Stat: pb.Status_NOT_SUPPORTED}, err
			default:
				return &pb.AddSAResp{Stat: pb.Status_FAILED}, err
			}
		}
	}

	return &pb.AddSAResp{Stat: pb.Status_SUCCESS}, nil
}

// DeleteSA deletes a previously installed SA from the SAD
func (s *Server) DeleteSA(_ context.Context, in *pb.DeleteSAReq) (*pb.DeleteSAResp, error) {
	// Generate the SA id
	name, err := s.createSAName(in.SaId)
	if err != nil {
		log.Printf("DeleteSA(): Name creation failure: %v", err)
		return &pb.DeleteSAResp{Stat: pb.Status_FAILED}, err
	}

	// fetch object from the database
	err = s.getSA(name)
	if err != nil {
		if err != infradb.ErrKeyNotFound {
			log.Printf("DeleteSA(): Failed to interact with store: %v", err)
			return &pb.DeleteSAResp{Stat: pb.Status_FAILED}, err
		}
		err = status.Errorf(codes.NotFound, "unable to find key %v", in.SaId)
		log.Printf("DeleteSA(): SA with id %s not found", name)
		return &pb.DeleteSAResp{Stat: pb.Status_NOT_FOUND}, err
	}

	if err := s.deleteSA(name); err != nil {
		log.Printf("DeleteSA(): SA with id %v, Delete SA from DB failure: %v", in.SaId, err)
		return &pb.DeleteSAResp{Stat: pb.Status_FAILED}, err
	}
	return &pb.DeleteSAResp{Stat: pb.Status_SUCCESS}, nil
}

// Do we need to implement the below functions ?
// func GetFeatures
// func GetSPI
