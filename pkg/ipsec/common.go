// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2022-2024 Intel Corporation, or its subsidiaries.
// Copyright (C) 2024 Ericcson AB.

// Package ipsec is the main package of the application
package ipsec

import (
	"strconv"

	"github.com/opiproject/opi-evpn-bridge/pkg/infradb"
	pb "github.com/opiproject/opi-evpn-bridge/pkg/ipsec/gen/go"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (s *Server) createSAName(id *pb.SAIdentifier) (string, error) {
	var proto string
	switch id.Proto {
	case pb.IPSecProtocol_IPSecProtoRSVD:
		proto = "rsvd"
	case pb.IPSecProtocol_IPSecProtoESP:
		proto = "esp"
	case pb.IPSecProtocol_IPSecProtoAH:
		proto = "ah"
	default:
		msg := "Unknown SA proto"
		return "", status.Errorf(codes.InvalidArgument, msg)
	}

	name := id.Src + "-" + id.Dst + "-" + strconv.FormatUint(uint64(id.Spi), 10) + "-" + proto + "-" + strconv.FormatUint(uint64(id.IfId), 10)
	return name, nil
}

func (s *Server) getSA(name string) error {
	_, err := infradb.GetSa(name)
	if err != nil {
		return err
	}
	return nil
}

func (s *Server) createSA(name string, sa *pb.AddSAReq) error {
	// check parameters
	if err := s.validateAddSaReq(sa); err != nil {
		return err
	}

	// translation of pb to domain object
	domainSa, err := infradb.NewSa(name, sa)
	if err != nil {
		return err
	}
	// Note: The status of the object will be generated in infraDB operation not here
	if err := infradb.CreateSa(domainSa); err != nil {
		return err
	}
	return nil
}

func (s *Server) deleteSA(name string) error {
	// Note: The status of the object will be generated in infraDB operation not here
	if err := infradb.DeleteSa(name); err != nil {
		return err
	}
	return nil
}
