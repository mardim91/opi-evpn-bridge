// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2022-2023 Dell Inc, or its subsidiaries.
// Copyright (C) 2023 Nordix Foundation.

// Package bridge is the main package of the application
package bridge

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

	pb "github.com/opiproject/opi-api/network/evpn-gw/v1alpha1/gen/go"
	//"github.com/opiproject/opi-evpn-bridge/pkg/utils"
	"github.com/opiproject/opi-evpn-bridge/pkg/utils/mocks"
	"github.com/opiproject/opi-evpn-bridge/pkg/infradb"
)

func sortLogicalBridges(bridges []*pb.LogicalBridge) {
	sort.Slice(bridges, func(i int, j int) bool {
		return bridges[i].Name < bridges[j].Name
	})
}

func (s *Server) createLogicalBridge(lb *pb.LogicalBridge) (*pb.LogicalBridge, error) {
	// check parameters
	if err := s.validateLogicalBridgeSpec(lb); err != nil {
		return nil, err
	}

	// translation of pb to domain object
	domainLB := infradb.NewBridge(lb)
	// Note: The status of the object will be generated in infraDB operation not here
	if err := infradb.CreateLB(domainLB); err != nil {
		return nil, err
	}
	s.ListHelper[lb.Name] = false
	return domainLB.ToPb(), nil
}

func (s *Server) deleteLogicalBridge(name string) error {

	// Note: The status of the object will be generated in infraDB operation not here
	if err := infradb.DeleteLB(name); err != nil {
		return err
	}

	delete(s.ListHelper, name)
	return nil
}

func (s *Server) getLogicalBridge(name string) (*pb.LogicalBridge, error) {
	domainLB, err := infradb.GetLB(name)
	if err != nil {
		return nil, err
	}
	return domainLB.ToPb(), nil
}

func (s *Server) updateLogicalBridge(lb *pb.LogicalBridge) (*pb.LogicalBridge, error) {
	// check parameters
	if err := s.validateLogicalBridgeSpec(lb); err != nil {
		return nil, err
	}

	// translation of pb to domain object
	domainLB := infradb.NewBridge(lb)
	// Note: The status of the object will be generated in infraDB operation not here
	if err := infradb.UpdateLB(domainLB); err != nil {
		return nil, err
	}
	return domainLB.ToPb(), nil
}

func resourceIDToFullName(resourceID string) string {
	return resourcename.Join(
		"//network.opiproject.org/",
		"bridges", resourceID,
	)
}

// TODO: move all of this to a common place
const (
	tenantbridgeName = "br-tenant"
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

	pb.RegisterLogicalBridgeServiceServer(server, opi)

	go func() {
		if err := server.Serve(listener); err != nil {
			log.Fatal(err)
		}
	}()

	return func(context.Context, string) (net.Conn, error) {
		return listener.Dial()
	}
}
