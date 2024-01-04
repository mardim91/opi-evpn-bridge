// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2022-2023 Intel Corporation, or its subsidiaries.
// Copyright (c) 2022-2023 Dell Inc, or its subsidiaries.
// Copyright (C) 2023 Nordix Foundation.

// Package svi is the main package of the application
package svi

import (
	"context"
	"fmt"
	"log"
	"reflect"

	"github.com/dgraph-io/badger"
	"github.com/google/uuid"
	"github.com/opiproject/opi-evpn-bridge/pkg/utils"

	pb "github.com/opiproject/opi-api/network/evpn-gw/v1alpha1/gen/go"

	"go.einride.tech/aip/resourceid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

// CreateSvi executes the creation of the Svi
func (s *Server) CreateSvi(ctx context.Context, in *pb.CreateSviRequest) (*pb.Svi, error) {
	// check input correctness
	if err := s.validateCreateSviRequest(in); err != nil {
		fmt.Printf("CreateSvi(): validation failure: %v", err)
		return nil, err
	}
	// see https://google.aip.dev/133#user-specified-ids
	resourceID := resourceid.NewSystemGenerated()
	if in.SviId != "" {
		log.Printf("CreateSvi(): client provided the ID of a resource %v, ignoring the name field %v", in.SviId, in.Svi.Name)
		resourceID = in.SviId
	}
	in.Svi.Name = resourceIDToFullName(resourceID)
	// idempotent API when called with same key, should return same object
	sviObj, err := s.getSvi(in.Svi.Name)
	if err != nil {
		if err != badger.ErrKeyNotFound {
			fmt.Printf("CreateSvi(): Failed to interact with store: %v", err)
			return nil, err
		}
	} else {
		log.Printf("CreateSvi(): Already existing Svi with id %v", in.Svi.Name)
		return sviObj, nil
	}

	// Store the domain object into DB
	response, err := s.createSvi(in.Svi)
	if err != nil {
		log.Printf("CreateSvi(): Svi with id %v, Create Svi to DB failure: %v", in.Svi.Name, err)
		return nil, err
	}
	return response, nil
}

// DeleteSvi deletes a Svi
func (s *Server) DeleteSvi(ctx context.Context, in *pb.DeleteSviRequest) (*emptypb.Empty, error) {
	// check input correctness
	if err := s.validateDeleteSviRequest(in); err != nil {
		fmt.Printf("DeleteSvi(): validation failure: %v", err)
		return nil, err
	}
	// fetch object from the database
	_, err := s.getSvi(in.Name)
	if err != nil {
		if err != badger.ErrKeyNotFound {
			fmt.Printf("Failed to interact with store: %v", err)
			return nil, err
		}
		if !in.AllowMissing {
			err = status.Errorf(codes.NotFound, "unable to find key %s", in.Name)
			fmt.Printf("DeleteSvi(): Svi with id %v: Not Found %v", in.Name, err)
			return nil, err
		}
		return &emptypb.Empty{}, nil
	}

	if err := s.deleteSvi(in.Name); err != nil {
		log.Printf("DeleteSvi(): Svi with id %v, Delete Svi from DB failure: %v", in.Name, err)
		return nil, err
	}

	return &emptypb.Empty{}, nil
}

// UpdateSvi updates a Svi
func (s *Server) UpdateSvi(ctx context.Context, in *pb.UpdateSviRequest) (*pb.Svi, error) {
	// check input correctness
	if err := s.validateUpdateSviRequest(in); err != nil {
		fmt.Printf("UpdateSvi(): validation failure: %v", err)
		return nil, err
	}
	// fetch object from the database
	sviObj, err := s.getSvi(in.Svi.Name)
	if err != nil {
		if err != badger.ErrKeyNotFound {
			fmt.Printf("UpdateSvi(): Failed to interact with store: %v", err)
			return nil, err
		}
		if !in.AllowMissing {
			err = status.Errorf(codes.NotFound, "unable to find key %s", in.Svi.Name)
			fmt.Printf("UpdateSvi(): Svi with id %v: Not Found %v", in.Svi.Name, err)
			return nil, err
		}

		log.Printf("UpdateSvi(): Svi with id %v is not found so it will be created", in.Svi.Name)

		// Store the domain object into DB
		response, err := s.createSvi(in.Svi)
		if err != nil {
			log.Printf("UpdateSvi(): Svi with id %v, Create Svi to DB failure: %v", in.Svi.Name, err)
			return nil, err
		}
		return response, nil
	}

	// We do that because we need to see if the object before and after the application of the mask is equal.
	// If it is the we just return the old object.
	updatedsviObj := utils.ProtoClone(sviObj)

	//Apply updateMask to the current Pb object
	utils.ApplyMaskToStoredPbObject(in.UpdateMask, updatedsviObj, in.Svi)

	// Check if the object before the application of the field mask
	// is different with the one after the application of the field mask
	if reflect.DeepEqual(sviObj, updatedsviObj) {
		return sviObj, nil
	}

	response, err := s.updateSvi(updatedsviObj)
	if err != nil {
		log.Printf("UpdateSvi(): Svi with id %v, Update Svi to DB failure: %v", in.Svi.Name, err)
		return nil, err
	}

	return response, nil
}

// GetSvi gets a Svi
func (s *Server) GetSvi(ctx context.Context, in *pb.GetSviRequest) (*pb.Svi, error) {
	// check input correctness
	if err := s.validateGetSviRequest(in); err != nil {
		fmt.Printf("GetSvi(): validation failure: %v", err)
		return nil, err
	}
	// fetch object from the database
	sviObj, err := s.getSvi(in.Name)
	if err != nil {
		if err != badger.ErrKeyNotFound {
			fmt.Printf("Failed to interact with store: %v", err)
			return nil, err
		}
		err = status.Errorf(codes.NotFound, "unable to find key %s", in.Name)
		fmt.Printf("GetSvi(): Svi with id %v: Not Found %v", in.Name, err)
		return nil, err
	}

	return sviObj, nil
}

// ListSvis lists logical bridges
func (s *Server) ListSvis(_ context.Context, in *pb.ListSvisRequest) (*pb.ListSvisResponse, error) {
	// check required fields
	if err := s.validateListSvisRequest(in); err != nil {
		fmt.Printf("ListSvis(): validation failure: %v", err)
		return nil, err
	}
	// fetch pagination from the database, calculate size and offset
	size, offset, err := utils.ExtractPagination(in.PageSize, in.PageToken, s.Pagination)
	if err != nil {
		return nil, err
	}
	// fetch object from the database
	Blobarray := []*pb.Svi{}
	// Dimitris: ListHelper is a  go map that helps on retrieving the objects
	// from DB by name. The reason that we need it is because the DB doesn't support any
	// List() function to retrieve all the Svi objects in one operation by using a prefix as key and not
	// the full name. The prefix can be: "//network.opiproject.org/svis"
	// In a replay scenario the List must be filled again as it will be out of sync with the DB status.
	for key := range s.ListHelper {
		sviObj, err := s.getSvi(key)
		if err != nil {
			if err != badger.ErrKeyNotFound {
				fmt.Printf("Failed to interact with store: %v", err)
				return nil, err
			}
			err := status.Errorf(codes.NotFound, "unable to find key %s", key)
			fmt.Printf("ListSvis(): Svi with id %v: Not Found %v", key, err)
			return nil, err
		}
		Blobarray = append(Blobarray, sviObj)
	}
	// sort is needed, since MAP is unsorted in golang, and we might get different results
	sortSvis(Blobarray)
	log.Printf("Limiting result len(%d) to [%d:%d]", len(Blobarray), offset, size)
	Blobarray, hasMoreElements := utils.LimitPagination(Blobarray, offset, size)
	token := ""
	if hasMoreElements {
		token = uuid.New().String()
		s.Pagination[token] = offset + size
	}
	return &pb.ListSvisResponse{Svis: Blobarray, NextPageToken: token}, nil
}
