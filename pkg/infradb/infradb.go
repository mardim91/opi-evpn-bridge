// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2023 Nordix Foundation.

package infradb

import (
	"errors"
	"fmt"
	"log"
	"sync"

	"github.com/opiproject/opi-evpn-bridge/pkg/infradb/common"
	"github.com/opiproject/opi-evpn-bridge/pkg/infradb/subscriber_framework/event_bus"
	"github.com/opiproject/opi-evpn-bridge/pkg/infradb/task_manager"
	"github.com/opiproject/opi-evpn-bridge/pkg/storage"
	"github.com/philippgille/gokv"
)

var infradb *InfraDB
var globalLock sync.Mutex

type InfraDB struct {
	client gokv.Store
}

var (
	ErrKeyNotFound           = errors.New("key not found")
	ErrComponentNotFound     = errors.New("component not found")
	ErrVrfNotFound           = errors.New("the referenced VRF has not been found")
	ErrLogicalBridgeNotFound = errors.New("the referenced Logical Bridge has not been found")
	ErrVrfNotEmpty           = errors.New("the VRF is not empty")
	ErrLogicalBridgeNotEmpty = errors.New("the LogicalBridge is not empty")
	// Add more error constants as needed
)

func NewInfraDB(address string, dbtype string) error {
	store, err := storage.NewStore(dbtype, address)
	if err != nil {
		log.Fatal(err)
		return err
	}

	infradb = &InfraDB{
		client: store.GetClient(),
	}
	return nil
}
func Close() error {
	return infradb.client.Close()
}
func CreateLB(lb *LogicalBridge) error {
	// TODO: Add checks to see if the VNI is already in use by another L2VPN
	globalLock.Lock()
	defer globalLock.Unlock()

	subscribers := event_bus.EBus.GetSubscribers("logical-bridge")
	if subscribers == nil {
		fmt.Println("CreateLB(): No subscribers for Logical Bridge objects")
	}

	fmt.Printf("CreateLB(): Create Logical Bridge: %+v\n", lb)

	err := infradb.client.Set(lb.Name, lb)
	if err != nil {
		log.Fatal(err)
		return err
	}

	// Add the New Created Logical Bridge to the "lbs" map
	lbs := make(map[string]bool)
	_, err = infradb.client.Get("lbs", &lbs)
	if err != nil {
		log.Fatal(err)
		return err
	}
	// The reason that we use a map and not a list is
	// because in the delete case we can delete the LB from the
	// map by just using the name. No need to iterate the whole list until
	// we find the LB and then delete it.
	lbs[lb.Name] = false
	err = infradb.client.Set("lbs", &lbs)
	if err != nil {
		log.Fatal(err)
		return err
	}

	task_manager.TaskMan.CreateTask(lb.Name, "logical-bridge", lb.ResourceVersion, subscribers)

	return nil
}
func DeleteLB(Name string) error {
	globalLock.Lock()
	defer globalLock.Unlock()

	subscribers := event_bus.EBus.GetSubscribers("logical-bridge")
	if subscribers == nil {
		fmt.Println("DeleteLB(): No subscribers for Logical Bridge objects")
	}

	lb := LogicalBridge{}
	found, err := infradb.client.Get(Name, &lb)
	if found != true {
		return ErrKeyNotFound
	}

	if lb.Svi != "" {
		log.Fatalf("DeleteLB(): Can not delete Logical Bridge %+v. Associated with SVI interfaces", lb.Name)
		return ErrLogicalBridgeNotEmpty
	}

	if len(lb.BridgePorts) != 0 || len(lb.MacTable) != 0 {
		log.Fatalf("DeleteLB(): Can not delete Logical Bridge %+v. Associated with Bridge Ports", lb.Name)
		return ErrVrfNotEmpty
	}

	for i := range subscribers {
		lb.Status.Components[i].CompStatus = common.COMP_STATUS_PENDING
	}
	lb.ResourceVersion = generateVersion()
	lb.Status.LBOperStatus = LB_OPER_STATUS_TO_BE_DELETED

	err = infradb.client.Set(lb.Name, lb)
	if err != nil {
		return err
	}

	task_manager.TaskMan.CreateTask(lb.Name, "logical-bridge", lb.ResourceVersion, subscribers)

	return nil
}
func GetLB(Name string) (*LogicalBridge, error) {
	globalLock.Lock()
	defer globalLock.Unlock()

	lb := LogicalBridge{}
	found, err := infradb.client.Get(Name, &lb)

	if !found {
		return &lb, ErrKeyNotFound
	}
	return &lb, err
}

// GetAllLogicalBridges returns a map of Logical Bridges from the DB
func GetAllLBs() ([]*LogicalBridge, error) {
	globalLock.Lock()
	defer globalLock.Unlock()

	lbs := []*LogicalBridge{}
	lbsMap := make(map[string]bool)
	found, err := infradb.client.Get("lbs", &lbsMap)

	if err != nil {
		log.Fatal(err)
		return nil, err
	}

	if !found {
		fmt.Println("GetAllLogicalBridges(): No Logical Bridges have been found")
		return nil, ErrKeyNotFound
	}

	for key := range lbsMap {
		lb := &LogicalBridge{}
		found, err := infradb.client.Get(key, lb)

		if err != nil {
			fmt.Printf("GetAllLogicalBridges(): Failed to get the Logical Bridge %s from store: %v", key, err)
			return nil, err
		}

		if !found {
			fmt.Printf("GetAllLogicalBridges():Logical Bridge %s not found", key)
			return nil, ErrKeyNotFound
		}
		lbs = append(lbs, lb)
	}

	return lbs, nil
}

func UpdateLB(lb *LogicalBridge) error {
	globalLock.Lock()
	defer globalLock.Unlock()

	subscribers := event_bus.EBus.GetSubscribers("logical-bridge")
	if subscribers == nil {
		fmt.Println("UpdateLB(): No subscribers for Logical Bridge objects")
	}

	err := infradb.client.Set(lb.Name, lb)
	if err != nil {
		log.Fatal(err)
		return err
	}

	task_manager.TaskMan.CreateTask(lb.Name, "logical-bridge", lb.ResourceVersion, subscribers)

	return nil
}

// UpdateLBStatus updates the status of Logical Bridge object based on the component report
func UpdateLBStatus(Name string, resourceVersion string, notificationId string, lbMeta *LogicalBridgeMetadata, component common.Component) error {
	globalLock.Lock()
	defer globalLock.Unlock()

	var lastCompSuccsess bool

	// When we get an error from an operation to the Database then we just return it. The
	// Task manager will just expire the task and retry.
	lb := LogicalBridge{}
	found, err := infradb.client.Get(Name, &lb)
	if err != nil {
		log.Fatal(err)
		return err
	}

	if !found {
		// No Logical Bridge object has been found in the database so we will instruct TaskManager to drop the Task that is related with this status update.
		task_manager.TaskMan.StatusUpdated(Name, "logical-bridge", lb.ResourceVersion, notificationId, true, &component)
		fmt.Printf("UpdateLBStatus(): No Logical Bridge object has been found in DB with Name %s\n", Name)
		return nil
	}

	if lb.ResourceVersion != resourceVersion {
		// Logical Bridge object in the database with different resourceVersion so we will instruct TaskManager to drop the Task that is related with this status update.
		task_manager.TaskMan.StatusUpdated(lb.Name, "logical-bridge", lb.ResourceVersion, notificationId, true, &component)
		fmt.Printf("UpdateLBStatus(): Invalid resourceVersion %s for Logical Bridge %+v\n", resourceVersion, lb)
		return nil
	}

	lbComponents := lb.Status.Components
	for i, comp := range lbComponents {
		compCounter := i + 1
		if comp.Name == component.Name {
			lb.Status.Components[i] = component

			if compCounter == len(lbComponents) && lb.Status.Components[i].CompStatus == common.COMP_STATUS_SUCCESS {
				lastCompSuccsess = true
			}

			break
		}
	}

	// Parse the Metadata that has been sent from the Component
	if lbMeta != nil {
		lb.Metadata = lbMeta
	}

	// Is it ok to delete an object before we update the last component status to success ?
	if lastCompSuccsess {
		if lb.Status.LBOperStatus == LB_OPER_STATUS_TO_BE_DELETED {
			err = infradb.client.Delete(lb.Name)
			if err != nil {
				log.Fatal(err)
				return err
			}

			lbs := make(map[string]bool)
			found, err = infradb.client.Get("lbs", &lbs)
			if err != nil {
				log.Fatal(err)
				return err
			}
			if !found {
				fmt.Println("UpdateLBStatus(): No Logical Bridges have been found")
				return ErrKeyNotFound
			}

			delete(lbs, lb.Name)
			err = infradb.client.Set("lbs", &lbs)
			if err != nil {
				log.Fatal(err)
				return err
			}

			fmt.Printf("UpdateLBStatus(): Logical Bridge %s has been deleted\n", Name)
		} else {
			lb.Status.LBOperStatus = LB_OPER_STATUS_UP
			err = infradb.client.Set(lb.Name, lb)
			if err != nil {
				log.Fatal(err)
				return err
			}
			fmt.Printf("UpdateLBStatus(): Logical Bridge %s has been updated: %+v\n", Name, lb)
		}
	} else {
		err = infradb.client.Set(lb.Name, lb)
		if err != nil {
			log.Fatal(err)
			return err
		}
		fmt.Printf("UpdateLBStatus(): Logical Bridge %s has been updated: %+v\n", Name, lb)
	}

	task_manager.TaskMan.StatusUpdated(lb.Name, "logical-bridge", lb.ResourceVersion, notificationId, false, &component)

	return nil
}

func CreateBP(bp *BridgePort) error {
	globalLock.Lock()
	defer globalLock.Unlock()

	subscribers := event_bus.EBus.GetSubscribers("bridge-port")
	if subscribers == nil {
		fmt.Println("CreateBP(): No subscribers for Bridge Port objects")
	}

	// Dimitris: Do I need to add here a check for MAC uniquness in of BP ?
	// The way to do this is to create a MAP of MACs and store it in the DB
	// and then check if MAC exist in this MAP everytime a new BP gets created.

	fmt.Printf("CreateBP(): Create Bridge Port: %+v\n", bp)

	// If Transparent Trunk then all the Logical Bridges are included by default
	if bp.TransparentTrunk {
		lbs := make(map[string]bool)
		found, err := infradb.client.Get("lbs", &lbs)
		if err != nil {
			log.Fatal(err)
			return err
		}
		if !found {
			fmt.Println("CreateBP(): No Logical Bridges have been found")
			return ErrKeyNotFound
		}

		for lbName := range lbs {
			bp.Spec.LogicalBridges = append(bp.Spec.LogicalBridges, lbName)
		}
	}

	// Get Logical Bridge infraDB objects
	// Fill up the Vlans list of the infraDB Bridge Port object
	// Add Bridge Port reference and save the Logical Bridge object back to DB
	for _, lbName := range bp.Spec.LogicalBridges {
		lb := LogicalBridge{}
		found, err := infradb.client.Get(lbName, &lb)
		if err != nil {
			log.Fatal(err)
			return err
		}
		if !found {
			log.Fatalf("CreateBP(): The Logical Bridge with name %+v has not been found\n", lbName)
			return ErrLogicalBridgeNotFound
		}
		bp.Vlans = append(bp.Vlans, &lb.Spec.VlanId)

		// Store Bridge Port reference to the Logical Bridge object
		lb.AddBridgePort(bp.Name, bp.Spec.MacAddress.String())

		// Save Logical Bridge object back to DB
		err = infradb.client.Set(lb.Name, lb)
		if err != nil {
			log.Fatal(err)
			return err
		}
	}

	// Store Bridge Port object to Database
	err := infradb.client.Set(bp.Name, bp)
	if err != nil {
		log.Fatal(err)
		return err
	}

	// Add the New Created Bridge Port to the "bps" map
	bps := make(map[string]bool)
	_, err = infradb.client.Get("bps", &bps)
	if err != nil {
		log.Fatal(err)
		return err
	}
	// The reason that we use a map and not a list is
	// because in the delete case we can delete the Bridge Port from the
	// map by just using the name. No need to iterate the whole list until
	// we find the Bridge port and then delete it.
	bps[bp.Name] = false
	err = infradb.client.Set("bps", &bps)
	if err != nil {
		log.Fatal(err)
		return err
	}

	task_manager.TaskMan.CreateTask(bp.Name, "bridge-port", bp.ResourceVersion, subscribers)

	return nil
}

func DeleteBP(Name string) error {
	globalLock.Lock()
	defer globalLock.Unlock()

	subscribers := event_bus.EBus.GetSubscribers("bridge-port")
	if subscribers == nil {
		fmt.Println("DeleteBP(): No subscribers for Bridge Port objects")
	}

	bp := BridgePort{}
	found, err := infradb.client.Get(Name, &bp)
	if found != true {
		return ErrKeyNotFound
	}

	for i := range subscribers {
		bp.Status.Components[i].CompStatus = common.COMP_STATUS_PENDING
	}
	bp.ResourceVersion = generateVersion()
	bp.Status.BPOperStatus = BP_OPER_STATUS_TO_BE_DELETED

	err = infradb.client.Set(bp.Name, bp)
	if err != nil {
		return err
	}

	task_manager.TaskMan.CreateTask(bp.Name, "bridge-port", bp.ResourceVersion, subscribers)

	return nil
}

func GetBP(Name string) (*BridgePort, error) {
	globalLock.Lock()
	defer globalLock.Unlock()

	bp := BridgePort{}
	found, err := infradb.client.Get(Name, &bp)

	if !found {
		return &bp, ErrKeyNotFound
	}
	return &bp, err
}

// GetAllBPs returns a map of Bridge Ports from the DB
func GetAllBPs() ([]*BridgePort, error) {
	globalLock.Lock()
	defer globalLock.Unlock()

	bps := []*BridgePort{}
	bpsMap := make(map[string]bool)
	found, err := infradb.client.Get("bps", &bpsMap)

	if err != nil {
		log.Fatal(err)
		return nil, err
	}

	if !found {
		fmt.Println("GetAllBPs(): No Bridge Ports have been found")
		return nil, ErrKeyNotFound
	}

	for key := range bpsMap {
		bp := &BridgePort{}
		found, err := infradb.client.Get(key, bp)

		if err != nil {
			fmt.Printf("GetAllBPs(): Failed to get the Bridge Port %s from store: %v", key, err)
			return nil, err
		}

		if !found {
			fmt.Printf("GetAllBPs(): Bridge Port %s not found", key)
			return nil, ErrKeyNotFound
		}
		bps = append(bps, bp)
	}

	return bps, nil
}

func UpdateBP(bp *BridgePort) error {
	// Note: The update functions for all the objects need to be revisited
	// The implementaation currently is not correct but due to low priority
	// will be refactored in the future.
	globalLock.Lock()
	defer globalLock.Unlock()

	subscribers := event_bus.EBus.GetSubscribers("bridge-port")
	if subscribers == nil {
		fmt.Println("UpdateBP(): No subscribers for Bridge Port objects")
	}

	err := infradb.client.Set(bp.Name, bp)
	if err != nil {
		log.Fatal(err)
		return err
	}

	task_manager.TaskMan.CreateTask(bp.Name, "bridge-port", bp.ResourceVersion, subscribers)

	return nil
}

// UpdateBPStatus updates the status of Bridge Port object based on the component report
func UpdateBPStatus(Name string, resourceVersion string, notificationId string, bpMeta *BridgePortMetadata, component common.Component) error {
	globalLock.Lock()
	defer globalLock.Unlock()

	var lastCompSuccsess bool

	// When we get an error from an operation to the Database then we just return it. The
	// Task manager will just expire the task and retry.
	bp := BridgePort{}
	found, err := infradb.client.Get(Name, &bp)
	if err != nil {
		log.Fatal(err)
		return err
	}

	if !found {
		// No Bridge Port object has been found in the database so we will instruct TaskManager to drop the Task that is related with this status update.
		task_manager.TaskMan.StatusUpdated(Name, "bridge-port", bp.ResourceVersion, notificationId, true, &component)
		fmt.Printf("UpdateBPStatus(): No Bridge Port object has been found in DB with Name %s\n", Name)
		return nil
	}

	if bp.ResourceVersion != resourceVersion {
		// Bridge Port object in the database with different resourceVersion so we will instruct TaskManager to drop the Task that is related with this status update.
		task_manager.TaskMan.StatusUpdated(bp.Name, "bridge-port", bp.ResourceVersion, notificationId, true, &component)
		fmt.Printf("UpdateBPStatus(): Invalid resourceVersion %s for Bridge Port %+v\n", resourceVersion, bp)
		return nil
	}

	bpComponents := bp.Status.Components
	for i, comp := range bpComponents {
		compCounter := i + 1
		if comp.Name == component.Name {
			bp.Status.Components[i] = component

			if compCounter == len(bpComponents) && bp.Status.Components[i].CompStatus == common.COMP_STATUS_SUCCESS {
				lastCompSuccsess = true
			}

			break
		}
	}

	// Parse the Metadata that has been sent from the Component
	if bpMeta != nil {
		if bpMeta.VPort != "" {
			bp.Metadata.VPort = bpMeta.VPort
		}
	}

	// Is it ok to delete an object before we update the last component status to success ?
	// Take care of deleting the references to the LB  objects after the BP has been successfully deleted
	if lastCompSuccsess {
		if bp.Status.BPOperStatus == SVI_OPER_STATUS_TO_BE_DELETED {
			// Delete the references from Logical Bridge objects
			for _, lbName := range bp.Spec.LogicalBridges {
				lb := LogicalBridge{}
				_, err := infradb.client.Get(lbName, &lb)
				if err != nil {
					log.Fatal(err)
					return err
				}

				// Store Bridge Port reference to the Logical Bridge object
				lb.DeleteBridgePort(bp.Name, bp.Spec.MacAddress.String())

				// Save Logical Bridge object back to DB
				err = infradb.client.Set(lb.Name, lb)
				if err != nil {
					log.Fatal(err)
					return err
				}
			}

			// Delete the Bridge Port object from the DB
			err = infradb.client.Delete(bp.Name)
			if err != nil {
				log.Fatal(err)
				return err
			}

			// Delete the Bridge Port from the bps map
			bps := make(map[string]bool)
			found, err = infradb.client.Get("bps", &bps)
			if err != nil {
				log.Fatal(err)
				return err
			}
			if !found {
				fmt.Println("UpdateBPStatus(): No Bridge Ports have been found")
				return ErrKeyNotFound
			}

			delete(bps, bp.Name)
			err = infradb.client.Set("bps", &bps)
			if err != nil {
				log.Fatal(err)
				return err
			}

			fmt.Printf("UpdateBPStatus(): Bridge Port %s has been deleted\n", Name)
		} else {
			bp.Status.BPOperStatus = BP_OPER_STATUS_UP
			err = infradb.client.Set(bp.Name, bp)
			if err != nil {
				log.Fatal(err)
				return err
			}
			fmt.Printf("UpdateBPStatus(): Bridge Port %s has been updated: %+v\n", Name, bp)
		}
	} else {

		err = infradb.client.Set(bp.Name, bp)
		if err != nil {
			log.Fatal(err)
			return err
		}
		fmt.Printf("UpdateBPStatus(): Bridge Port %s has been updated: %+v\n", Name, bp)
	}

	task_manager.TaskMan.StatusUpdated(bp.Name, "bridge-port", bp.ResourceVersion, notificationId, false, &component)

	return nil
}

func CreateVrf(vrf *Vrf) error {
	//TODO: Add checks to see if the vni is already in use by another L3VPN
	globalLock.Lock()
	defer globalLock.Unlock()

	subscribers := event_bus.EBus.GetSubscribers("vrf")
	if subscribers == nil {
		fmt.Println("CreateVrf(): No subscribers for Vrf objects")
	}

	fmt.Printf("CreateVrf(): Create Vrf: %+v\n", vrf)

	err := infradb.client.Set(vrf.Name, vrf)
	if err != nil {
		log.Fatal(err)
		return err
	}

	// Add the New Created VRF to the "vrfs" map
	vrfs := make(map[string]bool)
	_, err = infradb.client.Get("vrfs", &vrfs)
	if err != nil {
		log.Fatal(err)
		return err
	}
	// The reason that we use a map and not a list is
	// because in the delete case we can delete the vrf from the
	// map by just using the name. No need to iterate the whole list until
	// we find the vrf and then delete it.
	vrfs[vrf.Name] = false
	err = infradb.client.Set("vrfs", &vrfs)
	if err != nil {
		log.Fatal(err)
		return err
	}

	task_manager.TaskMan.CreateTask(vrf.Name, "vrf", vrf.ResourceVersion, subscribers)

	return nil
}

func DeleteVrf(Name string) error {
	globalLock.Lock()
	defer globalLock.Unlock()

	subscribers := event_bus.EBus.GetSubscribers("vrf")
	if subscribers == nil {
		fmt.Println("DeleteVrf(): No subscribers for Vrf objects")
	}

	vrf := Vrf{}
	found, err := infradb.client.Get(Name, &vrf)
	if found != true {
		return ErrKeyNotFound
	}

	if len(vrf.Svis) != 0 {
		log.Fatalf("DeleteVrf(): Can not delete VRF %+v. Associated with SVI interfaces", vrf.Name)
		return ErrVrfNotEmpty
	}

	for i := range subscribers {
		vrf.Status.Components[i].CompStatus = common.COMP_STATUS_PENDING
	}
	vrf.ResourceVersion = generateVersion()
	vrf.Status.VrfOperStatus = VRF_OPER_STATUS_TO_BE_DELETED

	err = infradb.client.Set(vrf.Name, vrf)
	if err != nil {
		return err
	}

	task_manager.TaskMan.CreateTask(vrf.Name, "vrf", vrf.ResourceVersion, subscribers)

	return nil
}
func GetVrf(Name string) (*Vrf, error) {
	globalLock.Lock()
	defer globalLock.Unlock()

	vrf := Vrf{}
	found, err := infradb.client.Get(Name, &vrf)

	if !found {
		return &vrf, ErrKeyNotFound
	}
	return &vrf, err
}

// GetAllVrfs returns a map of VRFs from the DB
func GetAllVrfs() ([]*Vrf, error) {
	globalLock.Lock()
	defer globalLock.Unlock()

	vrfs := []*Vrf{}
	vrfsMap := make(map[string]bool)
	found, err := infradb.client.Get("vrfs", &vrfsMap)

	if err != nil {
		log.Fatal(err)
		return nil, err
	}

	if !found {
		fmt.Println("GetAllVrfs(): No VRFs have been found")
		return nil, ErrKeyNotFound
	}

	for key := range vrfsMap {
		vrf := &Vrf{}
		found, err := infradb.client.Get(key, vrf)

		if err != nil {
			fmt.Printf("GetAllVrfs(): Failed to get the VRF %s from store: %v", key, err)
			return nil, err
		}

		if !found {
			fmt.Printf("GetAllVrfs(): VRF %s not found", key)
			return nil, ErrKeyNotFound
		}
		vrfs = append(vrfs, vrf)
	}

	return vrfs, nil
}

func UpdateVrf(vrf *Vrf) error {
	globalLock.Lock()
	defer globalLock.Unlock()

	subscribers := event_bus.EBus.GetSubscribers("vrf")
	if subscribers == nil {
		fmt.Println("CreateVrf(): No subscribers for Vrf objects")
	}

	err := infradb.client.Set(vrf.Name, vrf)
	if err != nil {
		log.Fatal(err)
		return err
	}

	task_manager.TaskMan.CreateTask(vrf.Name, "vrf", vrf.ResourceVersion, subscribers)

	return nil
}

// UpdateVrfStatus updates the status of VRF object based on the component report
func UpdateVrfStatus(Name string, resourceVersion string, notificationId string, vrfMeta *VrfMetadata, component common.Component) error {
	globalLock.Lock()
	defer globalLock.Unlock()

	var lastCompSuccsess bool

	// When we get an error from an operation to the Database then we just return it. The
	// Task manager will just expire the task and retry.
	vrf := Vrf{}
	found, err := infradb.client.Get(Name, &vrf)
	if err != nil {
		log.Fatal(err)
		return err
	}

	if !found {
		// No VRF object has been found in the database so we will instruct TaskManager to drop the Task that is related with this status update.
		task_manager.TaskMan.StatusUpdated(Name, "vrf", vrf.ResourceVersion, notificationId, true, &component)
		fmt.Printf("UpdateVrfStatus(): No VRF object has been found in DB with Name %s\n", Name)
		return nil
	}

	if vrf.ResourceVersion != resourceVersion {
		// VRF object in the database with different resourceVersion so we will instruct TaskManager to drop the Task that is related with this status update.
		task_manager.TaskMan.StatusUpdated(vrf.Name, "vrf", vrf.ResourceVersion, notificationId, true, &component)
		fmt.Printf("UpdateVrfStatus(): Invalid resourceVersion %s for VRF %+v\n", resourceVersion, vrf)
		return nil
	}

	vrfComponents := vrf.Status.Components
	for i, comp := range vrfComponents {
		compCounter := i + 1
		if comp.Name == component.Name {
			vrf.Status.Components[i] = component

			if compCounter == len(vrfComponents) && vrf.Status.Components[i].CompStatus == common.COMP_STATUS_SUCCESS {
				lastCompSuccsess = true
			}

			break
		}
	}

	// Parse the Metadata that has been sent from the Component
	if vrfMeta != nil {
		if len(vrfMeta.RoutingTable) > 0 {
			vrf.Metadata.RoutingTable = vrfMeta.RoutingTable
		}
	}

	// Is it ok to delete an object before we update the last component status to success ?
	if lastCompSuccsess {
		if vrf.Status.VrfOperStatus == VRF_OPER_STATUS_TO_BE_DELETED {
			err = infradb.client.Delete(vrf.Name)
			if err != nil {
				log.Fatal(err)
				return err
			}

			vrfs := make(map[string]bool)
			found, err = infradb.client.Get("vrfs", &vrfs)
			if err != nil {
				log.Fatal(err)
				return err
			}
			if !found {
				fmt.Println("UpdateVrfStatus(): No VRFs have been found")
				return ErrKeyNotFound
			}

			delete(vrfs, vrf.Name)
			err = infradb.client.Set("vrfs", &vrfs)
			if err != nil {
				log.Fatal(err)
				return err
			}

			fmt.Printf("UpdateVrfStatus(): VRF %s has been deleted\n", Name)
		} else {
			vrf.Status.VrfOperStatus = VRF_OPER_STATUS_UP
			err = infradb.client.Set(vrf.Name, vrf)
			if err != nil {
				log.Fatal(err)
				return err
			}
			fmt.Printf("UpdateVrfStatus(): VRF %s has been updated: %+v\n", Name, vrf)
		}
	} else {
		err = infradb.client.Set(vrf.Name, vrf)
		if err != nil {
			log.Fatal(err)
			return err
		}
		fmt.Printf("UpdateVrfStatus(): VRF %s has been updated: %+v\n", Name, vrf)
	}

	task_manager.TaskMan.StatusUpdated(vrf.Name, "vrf", vrf.ResourceVersion, notificationId, false, &component)

	return nil
}

func CreateSvi(svi *Svi) error {
	globalLock.Lock()
	defer globalLock.Unlock()

	subscribers := event_bus.EBus.GetSubscribers("svi")
	if subscribers == nil {
		fmt.Println("CreateSvi(): No subscribers for SVI objects")
	}

	fmt.Printf("CreateSvi(): Create SVI: %+v\n", svi)

	// Checking if the VRF exists
	vrf := Vrf{}
	found, err := infradb.client.Get(svi.Spec.Vrf, &vrf)
	if err != nil {
		log.Fatal(err)
		return err
	}
	if !found {
		log.Fatalf("CreateSvi(): The VRF with name %+v has not been found\n", svi.Spec.Vrf)
		return ErrVrfNotFound
	}

	// Checking if the Logical Bridge exists
	lb := LogicalBridge{}
	found, err = infradb.client.Get(svi.Spec.LogicalBridge, &lb)
	if err != nil {
		log.Fatal(err)
		return err
	}
	if !found {
		log.Fatalf("CreateSvi(): The Logical Bridge with name %+v has not been found\n", svi.Spec.LogicalBridge)
		return ErrVrfNotFound
	}

	// Store svi reference to the VRF object
	if err := vrf.AddSvi(svi.Name); err != nil {
		log.Fatal(err)
		return err
	}

	err = infradb.client.Set(vrf.Name, vrf)
	if err != nil {
		log.Fatal(err)
		return err
	}

	// Store svi reference to the Logical Bridge object
	if err := lb.AddSvi(svi.Name); err != nil {
		log.Fatal(err)
		return err
	}

	err = infradb.client.Set(lb.Name, lb)
	if err != nil {
		log.Fatal(err)
		return err
	}

	// Store SVI object to Database
	err = infradb.client.Set(svi.Name, svi)
	if err != nil {
		log.Fatal(err)
		return err
	}

	// Add the New Created SVI to the "svis" map
	svis := make(map[string]bool)
	_, err = infradb.client.Get("svis", &svis)
	if err != nil {
		log.Fatal(err)
		return err
	}
	// The reason that we use a map and not a list is
	// because in the delete case we can delete the SVI from the
	// map by just using the name. No need to iterate the whole list until
	// we find the SVI and then delete it.
	svis[svi.Name] = false
	err = infradb.client.Set("svis", &svis)
	if err != nil {
		log.Fatal(err)
		return err
	}

	task_manager.TaskMan.CreateTask(svi.Name, "svi", svi.ResourceVersion, subscribers)

	return nil
}
func DeleteSvi(Name string) error {
	globalLock.Lock()
	defer globalLock.Unlock()

	subscribers := event_bus.EBus.GetSubscribers("svi")
	if subscribers == nil {
		fmt.Println("DeleteSvi(): No subscribers for SVI objects")
	}

	svi := Svi{}
	found, err := infradb.client.Get(Name, &svi)
	if found != true {
		return ErrKeyNotFound
	}

	for i := range subscribers {
		svi.Status.Components[i].CompStatus = common.COMP_STATUS_PENDING
	}
	svi.ResourceVersion = generateVersion()
	svi.Status.SviOperStatus = SVI_OPER_STATUS_TO_BE_DELETED

	err = infradb.client.Set(svi.Name, svi)
	if err != nil {
		return err
	}

	task_manager.TaskMan.CreateTask(svi.Name, "svi", svi.ResourceVersion, subscribers)

	return nil
}

func GetSvi(Name string) (*Svi, error) {
	globalLock.Lock()
	defer globalLock.Unlock()

	svi := Svi{}
	found, err := infradb.client.Get(Name, &svi)

	if !found {
		return &svi, ErrKeyNotFound
	}
	return &svi, err
}

// GetAllSvis returns a map of Svis from the DB
func GetAllSvis() ([]*Svi, error) {
	globalLock.Lock()
	defer globalLock.Unlock()

	svis := []*Svi{}
	svisMap := make(map[string]bool)
	found, err := infradb.client.Get("svis", &svisMap)

	if err != nil {
		log.Fatal(err)
		return nil, err
	}

	if !found {
		fmt.Println("GetAllSvis(): No Svis have been found")
		return nil, ErrKeyNotFound
	}

	for key := range svisMap {
		svi := &Svi{}
		found, err := infradb.client.Get(key, svi)

		if err != nil {
			fmt.Printf("GetAllSvis(): Failed to get the SVI %s from store: %v", key, err)
			return nil, err
		}

		if !found {
			fmt.Printf("GetAllSvis(): SVI %s not found", key)
			return nil, ErrKeyNotFound
		}
		svis = append(svis, svi)
	}

	return svis, nil
}

func UpdateSvi(svi *Svi) error {
	globalLock.Lock()
	defer globalLock.Unlock()

	subscribers := event_bus.EBus.GetSubscribers("svi")
	if subscribers == nil {
		fmt.Println("UpdateSvi(): No subscribers for SVI objects")
	}

	err := infradb.client.Set(svi.Name, svi)
	if err != nil {
		log.Fatal(err)
		return err
	}

	task_manager.TaskMan.CreateTask(svi.Name, "svi", svi.ResourceVersion, subscribers)

	return nil
}

// UpdateSviStatus updates the status of SVI object based on the component report
func UpdateSviStatus(Name string, resourceVersion string, notificationId string, sviMeta *SviMetadata, component common.Component) error {
	globalLock.Lock()
	defer globalLock.Unlock()

	var lastCompSuccsess bool

	// When we get an error from an operation to the Database then we just return it. The
	// Task manager will just expire the task and retry.
	svi := Svi{}
	found, err := infradb.client.Get(Name, &svi)
	if err != nil {
		log.Fatal(err)
		return err
	}

	if !found {
		// No Svi object has been found in the database so we will instruct TaskManager to drop the Task that is related with this status update.
		task_manager.TaskMan.StatusUpdated(Name, "svi", svi.ResourceVersion, notificationId, true, &component)
		fmt.Printf("UpdateSviStatus(): No SVI object has been found in DB with Name %s\n", Name)
		return nil
	}

	if svi.ResourceVersion != resourceVersion {
		// Svi object in the database with different resourceVersion so we will instruct TaskManager to drop the Task that is related with this status update.
		task_manager.TaskMan.StatusUpdated(svi.Name, "svi", svi.ResourceVersion, notificationId, true, &component)
		fmt.Printf("UpdateSviStatus(): Invalid resourceVersion %s for SVI %+v\n", resourceVersion, svi)
		return nil
	}

	sviComponents := svi.Status.Components
	for i, comp := range sviComponents {
		compCounter := i + 1
		if comp.Name == component.Name {
			svi.Status.Components[i] = component

			if compCounter == len(sviComponents) && svi.Status.Components[i].CompStatus == common.COMP_STATUS_SUCCESS {
				lastCompSuccsess = true
			}

			break
		}
	}

	// Parse the Metadata that has been sent from the Component
	if sviMeta != nil {
		svi.Metadata = sviMeta
	}

	// Is it ok to delete an object before we update the last component status to success ?
	// Take care of deleting the references to the LB and VRF objects after the SVI has been successfully deleted
	if lastCompSuccsess {
		if svi.Status.SviOperStatus == SVI_OPER_STATUS_TO_BE_DELETED {
			// Delete the references from VRF and Logical Bridge objects

			// Get the dependent VRF object
			vrf := Vrf{}
			_, err := infradb.client.Get(svi.Spec.Vrf, &vrf)
			if err != nil {
				log.Fatal(err)
				return err
			}

			// Get the dependent Logical Bridge object
			lb := LogicalBridge{}
			_, err = infradb.client.Get(svi.Spec.LogicalBridge, &lb)
			if err != nil {
				log.Fatal(err)
				return err
			}

			// Delete the referenced SVI from the VRF and store the VRF to the DB
			if err := vrf.DeleteSvi(svi.Name); err != nil {
				log.Fatal(err)
				return err
			}

			err = infradb.client.Set(vrf.Name, vrf)
			if err != nil {
				log.Fatal(err)
				return err
			}

			// Delete the referenced SVI from the Logical Bridge and store the Logical Bridge to the DB
			if err := lb.DeleteSvi(svi.Name); err != nil {
				log.Fatal(err)
				return err
			}

			err = infradb.client.Set(lb.Name, lb)
			if err != nil {
				log.Fatal(err)
				return err
			}

			// Delete the SVI object from the DB
			err = infradb.client.Delete(svi.Name)
			if err != nil {
				log.Fatal(err)
				return err
			}

			// Delete the SVI from the svis map
			svis := make(map[string]bool)
			found, err = infradb.client.Get("svis", &svis)
			if err != nil {
				log.Fatal(err)
				return err
			}
			if !found {
				fmt.Println("UpdateSviStatus(): No Svis have been found")
				return ErrKeyNotFound
			}

			delete(svis, svi.Name)
			err = infradb.client.Set("svis", &svis)
			if err != nil {
				log.Fatal(err)
				return err
			}

			fmt.Printf("UpdateSviStatus(): Svi %s has been deleted\n", Name)
		} else {
			svi.Status.SviOperStatus = SVI_OPER_STATUS_UP
			err = infradb.client.Set(svi.Name, svi)
			if err != nil {
				log.Fatal(err)
				return err
			}
			fmt.Printf("UpdateSviStatus(): SVI %s has been updated: %+v\n", Name, svi)
		}
	} else {

		err = infradb.client.Set(svi.Name, svi)
		if err != nil {
			log.Fatal(err)
			return err
		}
		fmt.Printf("UpdateSviStatus(): SVI %s has been updated: %+v\n", Name, svi)
	}

	task_manager.TaskMan.StatusUpdated(svi.Name, "svi", svi.ResourceVersion, notificationId, false, &component)

	return nil
}
