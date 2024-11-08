// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2022-2024 Intel Corporation, or its subsidiaries.
// Copyright (C) 2024 Ericcson AB.

// Package ipsec is the main package of the application
package ipsec

import (
	"errors"
	"fmt"

	pb "github.com/opiproject/opi-evpn-bridge/pkg/ipsec/gen/go"
	"github.com/opiproject/opi-evpn-bridge/pkg/utils"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (s *Server) validateAddSaReq(in *pb.AddSAReq) error {
	// validate ip addresses
	if err := utils.CheckIPAddress(in.SaId.Src); err != nil {
		msg := fmt.Sprintf("Invalid format of src IP Address: %v", err)
		return status.Errorf(codes.InvalidArgument, msg)
	}

	if err := utils.CheckIPAddress(in.SaId.Dst); err != nil {
		msg := fmt.Sprintf("Invalid format of dst IP Address: %v", err)
		return status.Errorf(codes.InvalidArgument, msg)
	}

	// validate protocol
	if err := checkProtocol(in.SaId.Proto); err != nil {
		msg := fmt.Sprintf("check protocol error %v", err)
		return status.Errorf(codes.Unimplemented, msg)
	}

	// validate ipsec mode
	if err := checkIpsecMode(in.SaData.Mode); err != nil {
		msg := fmt.Sprintf("check ipsec mode error %v", err)
		return status.Errorf(codes.Unimplemented, msg)
	}

	// validate encapsulation
	if err := checkEncap(in.SaData.Encap); err != nil {
		msg := fmt.Sprintf("check encapsulation error %v", err)
		return status.Errorf(codes.Unimplemented, msg)
	}

	return nil
}

func checkProtocol(proto pb.IPSecProtocol) error {
	switch proto {
	case pb.IPSecProtocol_IPSecProtoESP:
		return nil
	default:
		return fmt.Errorf("checkSupportedProtocol(): unsupported or unimplemented protocol %v", proto)
	}
}

func checkIpsecMode(mode pb.IPSecMode) error {
	switch mode {
	case pb.IPSecMode_MODE_TUNNEL:
		return nil
	default:
		return fmt.Errorf("checkIpsecMode(): unsupported or unimplemented ipsec mode %v", mode)
	}
}

func checkEncap(encap pb.Bool) error {
	if encap == pb.Bool_TRUE {
		return errors.New("checkEncap(): unsupported udp encapsulation")
	}

	return nil
}
