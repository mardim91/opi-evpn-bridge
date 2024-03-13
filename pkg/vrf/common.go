// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2022-2023 Dell Inc, or its subsidiaries.
// Copyright (C) 2023 Nordix Foundation.

// Package vrf is the main package of the application
package vrf

import (
	"context"
	"crypto/rand"
	"fmt"
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
	"github.com/opiproject/opi-evpn-bridge/pkg/infradb"
	"github.com/opiproject/opi-evpn-bridge/pkg/utils/mocks"
)

func sortVrfs(vrfs []*pb.Vrf) {
	sort.Slice(vrfs, func(i int, j int) bool {
		return vrfs[i].Name < vrfs[j].Name
	})
}

func (s *Server) createVrf(vrf *pb.Vrf) (*pb.Vrf, error) {
	// check parameters
	if err := s.validateVrfSpec(vrf); err != nil {
		return nil, err
	}

	// translation of pb to domain object
	domainVrf := infradb.NewVrf(vrf)
	// Note: The status of the object will be generated in infraDB operation not here
	if err := infradb.CreateVrf(domainVrf); err != nil {
		return nil, err
	}
	return domainVrf.ToPb(), nil
}

func (s *Server) deleteVrf(name string) error {

	// Note: The status of the object will be generated in infraDB operation not here
	if err := infradb.DeleteVrf(name); err != nil {
		return err
	}
	return nil
}

func (s *Server) getVrf(name string) (*pb.Vrf, error) {
	domainVrf, err := infradb.GetVrf(name)
	if err != nil {
		return nil, err
	}
	return domainVrf.ToPb(), nil
}

func (s *Server) getAllVrfs() ([]*pb.Vrf, error) {
	vrfs := []*pb.Vrf{}
	domainVrfs, err := infradb.GetAllVrfs()
	if err != nil {
		return nil, err
	}

	for _, domainVrf := range domainVrfs {
		vrfs = append(vrfs, domainVrf.ToPb())
	}
	return vrfs, nil
}

func (s *Server) updateVrf(vrf *pb.Vrf) (*pb.Vrf, error) {
	// check parameters
	if err := s.validateVrfSpec(vrf); err != nil {
		return nil, err
	}

	// translation of pb to domain object
	domainVrf := infradb.NewVrf(vrf)
	// Note: The status of the object will be generated in infraDB operation not here
	if err := infradb.UpdateVrf(domainVrf); err != nil {
		return nil, err
	}
	return domainVrf.ToPb(), nil
}

func resourceIDToFullName(resourceID string) string {
	return resourcename.Join(
		"//network.opiproject.org/",
		"vrfs", resourceID,
	)
}

func generateRandMAC() ([]byte, error) {
	buf := make([]byte, 6)
	if _, err := rand.Read(buf); err != nil {
		return nil, fmt.Errorf("unable to retrieve 6 rnd bytes: %s", err)
	}

	// Set locally administered addresses bit and reset multicast bit
	buf[0] = (buf[0] | 0x02) & 0xfe

	return buf, nil
}

func checkTobeDeletedStatus(vrf *pb.Vrf) error {
	if vrf.Status.OperStatus == pb.VRFOperStatus_VRF_OPER_STATUS_TO_BE_DELETED {
		return fmt.Errorf("VRF %s in to be deleted status", vrf.Name)
	}

	return nil
}

// TODO: move all of this to a common place

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

	pb.RegisterVrfServiceServer(server, opi)

	go func() {
		if err := server.Serve(listener); err != nil {
			log.Fatal(err)
		}
	}()

	return func(context.Context, string) (net.Conn, error) {
		return listener.Dial()
	}
}
