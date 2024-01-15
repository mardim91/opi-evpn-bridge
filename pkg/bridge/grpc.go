// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2022-2023 Intel Corporation, or its subsidiaries.
// Copyright (c) 2022-2023 Dell Inc, or its subsidiaries.
// Copyright (C) 2023 Nordix Foundation.

// Package bridge is the main package of the application
package bridge

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
	"github.com/opiproject/opi-evpn-bridge/pkg/infradb"
)

// CreateLogicalBridge executes the creation of the LogicalBridge
func (s *Server) CreateLogicalBridge(ctx context.Context, in *pb.CreateLogicalBridgeRequest) (*pb.LogicalBridge, error) {
	// check input correctness
	if err := s.validateCreateLogicalBridgeRequest(in); err != nil {
		fmt.Printf("CreateLogicalBridge(): validation failure: %v", err)
		return nil, err
	}

	// see https://google.aip.dev/133#user-specified-ids
	resourceID := resourceid.NewSystemGenerated()
	if in.LogicalBridgeId != "" {
		log.Printf("CreateLogicalBridge(): client provided the ID of a resource %v, ignoring the name field %v", in.LogicalBridgeId, in.LogicalBridge.Name)
		resourceID = in.LogicalBridgeId
	}
	in.LogicalBridge.Name = resourceIDToFullName(resourceID)
	// idempotent API when called with same key, should return same object
	lbObj, err := s.getLogicalBridge(in.LogicalBridge.Name)
	if err != nil {
		if err != infradb.ErrKeyNotFound {
			fmt.Printf("CreateLogicalBridge(): Failed to interact with store: %v", err)
			return nil, err
		}
	} else {
		log.Printf("CreateLogicalBridge(): Already existing LogicalBridge with id %v", in.LogicalBridge.Name)
		return lbObj, nil
	}

	// Store the domain object into DB
	response, err := s.createLogicalBridge(in.LogicalBridge)
	if err != nil {
		log.Printf("CreateLogicalBridge(): LogicalBridge with id %v, Create Logical Bridge to DB failure: %v", in.LogicalBridge.Name, err)
		return nil, err
	}
	return response, nil
}

// DeleteLogicalBridge deletes a LogicalBridge
func (s *Server) DeleteLogicalBridge(ctx context.Context, in *pb.DeleteLogicalBridgeRequest) (*emptypb.Empty, error) {
	// check input correctness
	if err := s.validateDeleteLogicalBridgeRequest(in); err != nil {
		fmt.Printf("DeleteLogicalBridge(): validation failure: %v", err)
		return nil, err
	}
	// fetch object from the database
	_, err := s.getLogicalBridge(in.Name)
	if err != nil {
		if err != infradb.ErrKeyNotFound {
			fmt.Printf("Failed to interact with store: %v", err)
			return nil, err
		}
		if !in.AllowMissing {
			err = status.Errorf(codes.NotFound, "unable to find key %s", in.Name)
			fmt.Printf("DeleteLogicalBridge(): LogicalBridge with id %v: Not Found %v", in.Name, err)
			return nil, err
		}
		return &emptypb.Empty{}, nil
	}

	if err := s.deleteLogicalBridge(in.Name); err != nil {
		log.Printf("DeleteLogicalBridge(): LogicalBridge with id %v, Delete Logical Bridge from DB failure: %v", in.Name, err)
		return nil, err
	}

	return &emptypb.Empty{}, nil
}

// UpdateLogicalBridge updates a LogicalBridge
func (s *Server) UpdateLogicalBridge(ctx context.Context, in *pb.UpdateLogicalBridgeRequest) (*pb.LogicalBridge, error) {
	// check input correctness
	if err := s.validateUpdateLogicalBridgeRequest(in); err != nil {
		fmt.Printf("UpdateLogicalBridge(): validation failure: %v", err)
		return nil, err
	}

	// fetch object from the database
	lbObj, err := s.getLogicalBridge(in.LogicalBridge.Name)
	if err != nil {
		if err != badger.ErrKeyNotFound {
			fmt.Printf("UpdateLogicalBridge(): Failed to interact with store: %v", err)
			return nil, err
		}
		if !in.AllowMissing {
			err = status.Errorf(codes.NotFound, "unable to find key %s", in.LogicalBridge.Name)
			fmt.Printf("UpdateLogicalBridge(): LogicalBridge with id %v: Not Found %v", in.LogicalBridge.Name, err)
			return nil, err
		}

		log.Printf("UpdateLogicalBridge(): Logical Bridge with id %v is not found so it will be created", in.LogicalBridge.Name)

		// Store the domain object into DB
		response, err := s.createLogicalBridge(in.LogicalBridge)
		if err != nil {
			log.Printf("UpdateLogicalBridge(): LogicalBridge with id %v, Create Logical Bridge to DB failure: %v", in.LogicalBridge.Name, err)
			return nil, err
		}
		return response, nil
	}

	// We do that because we need to see if the object before and after the application of the mask is equal.
	// If it is the we just return the old object.
	updatedlbObj := utils.ProtoClone(lbObj)

	//Apply updateMask to the current Pb object
	utils.ApplyMaskToStoredPbObject(in.UpdateMask, updatedlbObj, in.LogicalBridge)

	// Check if the object before the application of the field mask
	// is different with the one after the application of the field mask
	if reflect.DeepEqual(lbObj, updatedlbObj) {
		return lbObj, nil
	}

	response, err := s.updateLogicalBridge(updatedlbObj)
	if err != nil {
		log.Printf("UpdateLogicalBridge(): LogicalBridge with id %v, Update Logical Bridge to DB failure: %v", in.LogicalBridge.Name, err)
		return nil, err
	}

	return response, nil
}

// GetLogicalBridge gets a LogicalBridge
func (s *Server) GetLogicalBridge(ctx context.Context, in *pb.GetLogicalBridgeRequest) (*pb.LogicalBridge, error) {
	// check input correctness
	if err := s.validateGetLogicalBridgeRequest(in); err != nil {
		fmt.Printf("GetLogicalBridge(): validation failure: %v", err)
		return nil, err
	}
	// fetch object from the database
	lbObj, err := s.getLogicalBridge(in.Name)
	if err != nil {
		if err != badger.ErrKeyNotFound {
			fmt.Printf("Failed to interact with store: %v", err)
			return nil, err
		}
		err = status.Errorf(codes.NotFound, "unable to find key %s", in.Name)
		fmt.Printf("GetLogicalBridge(): LogicalBridge with id %v: Not Found %v", in.Name, err)
		return nil, err
	}

	return lbObj, nil
}

// ListLogicalBridges lists logical bridges
func (s *Server) ListLogicalBridges(_ context.Context, in *pb.ListLogicalBridgesRequest) (*pb.ListLogicalBridgesResponse, error) {
	// check input correctness
	if err := s.validateListLogicalBridgesRequest(in); err != nil {
		fmt.Printf("ListLogicalBridges(): validation failure: %v", err)
		return nil, err
	}
	// fetch pagination from the database, calculate size and offset
	size, offset, err := utils.ExtractPagination(in.PageSize, in.PageToken, s.Pagination)
	if err != nil {
		return nil, err
	}
	// fetch object from the database
	Blobarray := []*pb.LogicalBridge{}
	// Dimitris: ListHelper is a  go map that helps on retrieving the objects
	// from DB by name. The reason that we need it is because the DB doesn't support any
	// List() function to retrieve all the LB objects in one operation by using a prefix as key and not
	// the full name. The prefix can be: "//network.opiproject.org/bridges"
	// In a replay scenario the List must be filled again as it will be out of sync with the DB status.
	for key := range s.ListHelper {
		lbObj, err := s.getLogicalBridge(key)
		if err != nil {
			if err != badger.ErrKeyNotFound {
				fmt.Printf("Failed to interact with store: %v", err)
				return nil, err
			}
			err := status.Errorf(codes.NotFound, "unable to find key %s", key)
			fmt.Printf("ListLogicalBridges(): LogicalBridge with id %v: Not Found %v", key, err)
			return nil, err
		}
		Blobarray = append(Blobarray, lbObj)
	}
	// sort is needed, since MAP is unsorted in golang, and we might get different results
	sortLogicalBridges(Blobarray)
	log.Printf("Limiting result len(%d) to [%d:%d]", len(Blobarray), offset, size)
	Blobarray, hasMoreElements := utils.LimitPagination(Blobarray, offset, size)
	token := ""
	if hasMoreElements {
		token = uuid.New().String()
		s.Pagination[token] = offset + size
	}
	return &pb.ListLogicalBridgesResponse{LogicalBridges: Blobarray, NextPageToken: token}, nil
}
