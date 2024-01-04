// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2022-2023 Dell Inc, or its subsidiaries.
// Copyright (C) 2023 Nordix Foundation.

// Package svi is the main package of the application
package svi

import (
	"context"
	"log"
	"net"
	"sort"
	"testing"

	//"github.com/philippgille/gokv/gomap"
	"go.einride.tech/aip/resourcename"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/proto"

	pb "github.com/opiproject/opi-api/network/evpn-gw/v1alpha1/gen/go"
	pc "github.com/opiproject/opi-api/network/opinetcommon/v1alpha1/gen/go"
	//"github.com/opiproject/opi-evpn-bridge/pkg/utils"
	"github.com/opiproject/opi-evpn-bridge/pkg/utils/mocks"
	"github.com/opiproject/opi-evpn-bridge/pkg/infradb"
)

func sortSvis(svis []*pb.Svi) {
	sort.Slice(svis, func(i int, j int) bool {
		return svis[i].Name < svis[j].Name
	})
}

func (s *Server) createSvi(svi *pb.Svi) (*pb.Svi, error) {
	// check parameters
	if err := s.validateSviSpec(svi); err != nil {
		return nil, err
	}

	// translation of pb to domain object
	domainSvi := infradb.NewSvi(svi)
	// Note: The status of the object will be generated in infraDB operation not here
	if err := infradb.CreateSvi(domainSvi); err != nil {
		return nil, err
	}
	s.ListHelper[svi.Name] = false
	return domainSvi.ToPb(), nil
}

func (s *Server) deleteSvi(name string) error {

	// Note: The status of the object will be generated in infraDB operation not here
	if err := infradb.DeleteSvi(name); err != nil {
		return err
	}

	delete(s.ListHelper, name)
	return nil
}

func (s *Server) getSvi(name string) (*pb.Svi, error) {
	domainSvi, err := infradb.GetSvi(name)
	if err != nil {
		return nil, err
	}
	return domainSvi.ToPb(), nil
}

func (s *Server) updateSvi(svi *pb.Svi) (*pb.Svi, error) {
	// check parameters
	if err := s.validateSviSpec(svi); err != nil {
		return nil, err
	}

	// translation of pb to domain object
	domainSvi := infradb.NewSvi(svi)
	// Note: The status of the object will be generated in infraDB operation not here
	if err := infradb.UpdateSvi(domainSvi); err != nil {
		return nil, err
	}
	return domainSvi.ToPb(), nil
}

func resourceIDToFullName(resourceID string) string {
	return resourcename.Join(
		"//network.opiproject.org/",
		"svis", resourceID,
	)
}

// TODO: move all of this to a common place
const (
	tenantbridgeName = "br-tenant"
)

var (
	testLogicalBridgeID   = "opi-bridge9"
	testLogicalBridgeName = resourceIDToFullName(testLogicalBridgeID)
	testLogicalBridge     = pb.LogicalBridge{
		Spec: &pb.LogicalBridgeSpec{
			Vni:    proto.Uint32(11),
			VlanId: 22,
			VtepIpPrefix: &pc.IPPrefix{
				Addr: &pc.IPAddress{
					Af: pc.IpAf_IP_AF_INET,
					V4OrV6: &pc.IPAddress_V4Addr{
						V4Addr: 167772162,
					},
				},
				Len: 24,
			},
		},
	}
	testLogicalBridgeWithStatus = pb.LogicalBridge{
		Name: testLogicalBridgeName,
		Spec: testLogicalBridge.Spec,
		Status: &pb.LogicalBridgeStatus{
			OperStatus: pb.LBOperStatus_LB_OPER_STATUS_UP,
		},
	}

	testVrfID   = "opi-vrf8"
	testVrfName = resourceIDToFullName(testVrfID)
	testVrf     = pb.Vrf{
		Spec: &pb.VrfSpec{
			Vni: proto.Uint32(1000),
			LoopbackIpPrefix: &pc.IPPrefix{
				// Addr: &pc.IPAddress{
				// 	Af: pc.IpAf_IP_AF_INET,
				// 	V4OrV6: &pc.IPAddress_V4Addr{
				// 		V4Addr: 167772162,
				// 	},
				// },
				Len: 24,
			},
			VtepIpPrefix: &pc.IPPrefix{
				Addr: &pc.IPAddress{
					Af: pc.IpAf_IP_AF_INET,
					V4OrV6: &pc.IPAddress_V4Addr{
						V4Addr: 167772162,
					},
				},
				Len: 24,
			},
		},
	}
	testVrfWithStatus = pb.Vrf{
		Name: testVrfName,
		Spec: testVrf.Spec,
		Status: &pb.VrfStatus{
			LocalAs: 4,
		},
	}
)

type testEnv struct {
	mockNetlink *mocks.Netlink
	mockFrr     *mocks.Frr
	opi         *Server
	conn        *grpc.ClientConn
}

func (e *testEnv) Close() {
	err := e.conn.Close()
	if err != nil {
		log.Fatal(err)
	}
}

func newTestEnv(ctx context.Context, t *testing.T) *testEnv {
	//store := gomap.NewStore(gomap.Options{Codec: utils.ProtoCodec{}})
	env := &testEnv{}
	env.mockNetlink = mocks.NewNetlink(t)
	env.mockFrr = mocks.NewFrr(t)
	//env.opi = NewServerWithArgs(env.mockNetlink, env.mockFrr, store)
	env.opi = NewServer()
	conn, err := grpc.DialContext(ctx,
		"",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(dialer(env.opi)))
	if err != nil {
		log.Fatal(err)
	}
	env.conn = conn
	return env
}

func dialer(opi *Server) func(context.Context, string) (net.Conn, error) {
	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer()

	pb.RegisterSviServiceServer(server, opi)

	go func() {
		if err := server.Serve(listener); err != nil {
			log.Fatal(err)
		}
	}()

	return func(context.Context, string) (net.Conn, error) {
		return listener.Dial()
	}
}
