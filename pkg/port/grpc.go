// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2022-2023 Intel Corporation, or its subsidiaries.
// Copyright (c) 2022-2023 Dell Inc, or its subsidiaries.
// Copyright (C) 2023 Nordix Foundation.

// Package port is the main package of the application
package port

import (
	"context"
	"fmt"
	"log"
	"reflect"

	"github.com/dgraph-io/badger"
	"github.com/google/uuid"
	"github.com/opiproject/opi-evpn-bridge/pkg/utils"

	//pb "github.com/opiproject/opi-api/network/evpn-gw/v1alpha1/gen/go"
	pb "github.com/mardim91/opi-api/network/evpn-gw/v1alpha1/gen/go"

	"go.einride.tech/aip/resourceid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

// CreateBridgePort executes the creation of the port
func (s *Server) CreateBridgePort(ctx context.Context, in *pb.CreateBridgePortRequest) (*pb.BridgePort, error) {
	// check input correctness
	if err := s.validateCreateBridgePortRequest(in); err != nil {
		fmt.Printf("CreateBridgePort(): validation failure: %v", err)
		return nil, err
	}
	// see https://google.aip.dev/133#user-specified-ids
	resourceID := resourceid.NewSystemGenerated()
	if in.BridgePortId != "" {
		log.Printf("CreateBridgePort(): client provided the ID of a resource %v, ignoring the name field %v", in.BridgePortId, in.BridgePort.Name)
		resourceID = in.BridgePortId
	}
	in.BridgePort.Name = resourceIDToFullName(resourceID)
	// idempotent API when called with same key, should return same object
	bpObj, err := s.getBridgePort(in.BridgePort.Name)
	if err != nil {
		if err != badger.ErrKeyNotFound {
			fmt.Printf("CreateBridgePort(): Failed to interact with store: %v", err)
			return nil, err
		}
	} else {
		log.Printf("CreateBridgePort(): Already existing BridgePort with id %v", in.BridgePort.Name)
		return bpObj, nil
	}
	// Store the domain object into DB
	response, err := s.createBridgePort(in.BridgePort)
	if err != nil {
		log.Printf("CreateBridgePort(): BridgePort with id %v, Create Bridge Port to DB failure: %v", in.BridgePort.Name, err)
		return nil, err
	}
	return response, nil
}

// DeleteBridgePort deletes a port
func (s *Server) DeleteBridgePort(ctx context.Context, in *pb.DeleteBridgePortRequest) (*emptypb.Empty, error) {
	// check input correctness
	if err := s.validateDeleteBridgePortRequest(in); err != nil {
		fmt.Printf("DeleteBridgePort(): validation failure: %v", err)
		return nil, err
	}
	// fetch object from the database
	_, err := s.getBridgePort(in.Name)
	if err != nil {
		if err != badger.ErrKeyNotFound {
			fmt.Printf("Failed to interact with store: %v", err)
			return nil, err
		}
		if !in.AllowMissing {
			err = status.Errorf(codes.NotFound, "unable to find key %s", in.Name)
			fmt.Printf("DeleteBridgePort(): BridgePort with id %v: Not Found %v", in.Name, err)
			return nil, err
		}
		return &emptypb.Empty{}, nil
	}

	if err := s.deleteBridgePort(in.Name); err != nil {
		log.Printf("DeleteBridgePort(): BridgePort with id %v, Delete Bridge Port from DB failure: %v", in.Name, err)
		return nil, err
	}
	return &emptypb.Empty{}, nil
}

// UpdateBridgePort updates an Nvme Subsystem
func (s *Server) UpdateBridgePort(ctx context.Context, in *pb.UpdateBridgePortRequest) (*pb.BridgePort, error) {
	// check input correctness
	if err := s.validateUpdateBridgePortRequest(in); err != nil {
		fmt.Printf("UpdateBridgePort(): validation failure: %v", err)
		return nil, err
	}
	// fetch object from the
	bpObj, err := s.getBridgePort(in.BridgePort.Name)
	if err != nil {
		if err != badger.ErrKeyNotFound {
			fmt.Printf("UpdateBridgePort(): Failed to interact with store: %v", err)
			return nil, err
		}
		if !in.AllowMissing {
			err = status.Errorf(codes.NotFound, "unable to find key %s", in.BridgePort.Name)
			fmt.Printf("UpdateBridgePort(): BridgePort with id %v: Not Found %v", in.BridgePort.Name, err)
			return nil, err
		}

		log.Printf("UpdateBridgePort(): Bridge Port with id %v is not found so it will be created", in.BridgePort.Name)

		// Store the domain object into DB
		response, err := s.createBridgePort(in.BridgePort)
		if err != nil {
			log.Printf("UpdateBridgePort(): BridgePort with id %v, Create Bridge Port to DB failure: %v", in.BridgePort.Name, err)
			return nil, err
		}
		return response, nil
	}

	// We do that because we need to see if the object before and after the application of the mask is equal.
	// If it is the we just return the old object.
	updatedbpObj := utils.ProtoClone(bpObj)

	//Apply updateMask to the current Pb object
	utils.ApplyMaskToStoredPbObject(in.UpdateMask, updatedbpObj, in.BridgePort)

	// Check if the object before the application of the field mask
	// is different with the one after the application of the field mask
	if reflect.DeepEqual(bpObj, updatedbpObj) {
		return bpObj, nil
	}

	response, err := s.updateBridgePort(updatedbpObj)
	if err != nil {
		log.Printf("UpdateBridgePort(): BridgePort with id %v, Update Bridge Port to DB failure: %v", in.BridgePort.Name, err)
		return nil, err
	}

	return response, nil
}

// GetBridgePort gets an BridgePort
func (s *Server) GetBridgePort(ctx context.Context, in *pb.GetBridgePortRequest) (*pb.BridgePort, error) {
	// check input correctness
	if err := s.validateGetBridgePortRequest(in); err != nil {
		fmt.Printf("GetBridgePort(): validation failure: %v", err)
		return nil, err
	}
	// fetch object from the database
	bpObj, err := s.getBridgePort(in.Name)
	if err != nil {
		if err != badger.ErrKeyNotFound {
			fmt.Printf("Failed to interact with store: %v", err)
			return nil, err
		}
		err = status.Errorf(codes.NotFound, "unable to find key %s", in.Name)
		fmt.Printf("GetBridgePort(): BridgePort with id %v: Not Found %v", in.Name, err)
		return nil, err
	}

	return bpObj, nil
}

// ListBridgePorts lists logical bridges
func (s *Server) ListBridgePorts(_ context.Context, in *pb.ListBridgePortsRequest) (*pb.ListBridgePortsResponse, error) {
	// check required fields
	if err := s.validateListBridgePortsRequest(in); err != nil {
		fmt.Printf("ListBridgePorts(): validation failure: %v", err)
		return nil, err
	}
	// fetch pagination from the database, calculate size and offset
	size, offset, err := utils.ExtractPagination(in.PageSize, in.PageToken, s.Pagination)
	if err != nil {
		return nil, err
	}
	// fetch object from the database
	Blobarray := []*pb.BridgePort{}
	// Dimitris: ListHelper is a  go map that helps on retrieving the objects
	// from DB by name. The reason that we need it is because the DB doesn't support any
	// List() function to retrieve all the BP objects in one operation by using a prefix as key and not
	// the full name. The prefix can be: "//network.opiproject.org/ports"
	// In a replay scenario the List must be filled again as it will be out of sync with the DB status.
	for key := range s.ListHelper {
		bpObj, err := s.getBridgePort(key)
		if err != nil {
			if err != badger.ErrKeyNotFound {
				fmt.Printf("Failed to interact with store: %v", err)
				return nil, err
			}
			err := status.Errorf(codes.NotFound, "unable to find key %s", key)
			fmt.Printf("ListBridgePorts(): BridgePort with id %v: Not Found %v", key, err)
			return nil, err
		}
		Blobarray = append(Blobarray, bpObj)
	}
	// sort is needed, since MAP is unsorted in golang, and we might get different results
	sortBridgePorts(Blobarray)
	log.Printf("Limiting result len(%d) to [%d:%d]", len(Blobarray), offset, size)
	Blobarray, hasMoreElements := utils.LimitPagination(Blobarray, offset, size)
	token := ""
	if hasMoreElements {
		token = uuid.New().String()
		s.Pagination[token] = offset + size
	}
	return &pb.ListBridgePortsResponse{BridgePorts: Blobarray, NextPageToken: token}, nil
}
