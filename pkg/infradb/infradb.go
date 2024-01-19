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
	ErrKeyNotFound       = errors.New("Key not found")
	ErrComponentNotFound = errors.New("Component not found")
	// Add more error constants as needed
)

func NewInfraDB(address string, dbtype string) error {

	store, err := storage.NewStore(dbtype, address)
	if err != nil {
		return err
		log.Fatal(err)
	}

	infradb = &InfraDB{
		client: store.GetClient(),
	}
	return nil

}
func Close() error {
	return infradb.client.Close()
}
func CreateLB(br *Bridge) error {

	globalLock.Lock()
	defer globalLock.Unlock()
	br.ResourceVersion = generateVersion()

	fmt.Printf("\nCreateLB:%+v\n", br)
	err := infradb.client.Set(br.Name, br)
	if err != nil {
		log.Fatal(err)
		return err
	}

	return nil
}
func DeleteLB(Name string) error {
	globalLock.Lock()
	defer globalLock.Unlock()

	err := infradb.client.Delete(Name)
	if err != nil {
		log.Fatal(err)
	}
	return err
}
func GetLB(Name string) (Bridge, error) {

	globalLock.Lock()
	defer globalLock.Unlock()

	bridge := Bridge{}
	found, err := infradb.client.Get(Name, &bridge)
	if !found {
		return bridge, ErrKeyNotFound
	}
	return bridge, err
}
func UpdateLB(br *Bridge) error {

	globalLock.Lock()
	defer globalLock.Unlock()

	br.ResourceVersion = generateVersion()

	err := infradb.client.Set(br.Name, br)
	if err != nil {
		log.Fatal(err)
		return err
	}
	return nil
}
func CreateBP(port *Port) error {

	globalLock.Lock()
	defer globalLock.Unlock()

	port.ResourceVersion = generateVersion()

	err := infradb.client.Set(port.Name, port)
	if err != nil {
		log.Fatal(err)
	}
	return err
}
func DeleteBP(Name string) error {

	globalLock.Lock()
	defer globalLock.Unlock()

	err := infradb.client.Delete(Name)
	if err != nil {
		log.Fatal(err)
	}
	return err
}

func GetBP(Name string) (Port, error) {

	globalLock.Lock()
	defer globalLock.Unlock()

	Port := Port{}
	found, err := infradb.client.Get(Name, &Port)
	if found != true {
		return Port, errors.New("KeyNotFound")
	}
	return Port, err
}
func UpdateBP(port *Port) error {

	globalLock.Lock()
	defer globalLock.Unlock()

	port.ResourceVersion = generateVersion()

	err := infradb.client.Set(port.Name, port)
	if err != nil {
		log.Fatal(err)
	}
	return nil
}

func CreateVrf(vrf *Vrf) error {

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
	_, err = infradb.client.Get("vrfs", vrfs)
	if err != nil {
		log.Fatal(err)
		return err
	}
	// The reason that we use a map and not a list is
	// because in the delete case we can delete the vrf from the
	// map by just using the name. No need to iterate the whole list until
	// we find the vrf and then delete it.
	vrfs[vrf.Name] = false
	err = infradb.client.Set("vrfs", vrfs)
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

	/*err := infradb.client.Delete(Name)
	if err != nil {
		log.Fatal(err)
	}*/
	vrf := Vrf{}
	found, err := infradb.client.Get(Name, &vrf)
	if found != true {
		return ErrKeyNotFound
	}

	//Dimitris: Do we need to generateVersion again ? Why ?
	vrf.ResourceVersion = generateVersion()
	vrf.Status.VrfOperStatus = VRF_OPER_STATUS_TO_BE_DELETED

	err = infradb.client.Set(vrf.Name, vrf)
	if err != nil {
		return err
	}

	/* Create task manager task
	taskMgr.CreateTask(vrf.name,vrf.ResourceVersion, subscribers )
	*/

	return err
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

func GetAllVrfs() ([]*Vrf, error) {
	globalLock.Lock()
	defer globalLock.Unlock()

	vrfs := []*Vrf{}
	vrfsMap := make(map[string]bool)
	found, err := infradb.client.Get("vrfs", vrfsMap)

	if err != nil {
		log.Fatal(err)
		return nil, err
	}

	if !found {
		fmt.Println("GetAllVrfs(): No VRFs has been found")
		return nil, ErrKeyNotFound
	}

	for key := range vrfsMap {
		vrf, err := GetVrf(key)
		if err != nil {
			if err != ErrKeyNotFound {
				fmt.Printf("GetAllVrfs(): Failed to get the VRF %s store: %v", vrf.Name, err)
				return nil, err
			}
			fmt.Printf("GetAllVrfs(): VRF %s not found: %v", vrf.Name, err)
			return nil, err
		}
		vrfs = append(vrfs, vrf)
	}

	return vrfs, nil

}
func UpdateVrf(vrf *Vrf) error {

	globalLock.Lock()
	defer globalLock.Unlock()

	vrf.ResourceVersion = generateVersion()

	for _, component := range vrf.Status.Components {
		component.CompStatus = common.COMP_STATUS_PENDING
		fmt.Printf("Component: %s, Value: %d\n", component.Name, component.CompStatus)
	}
	err := infradb.client.Set(vrf.Name, vrf)
	if err != nil {
		log.Fatal(err)
	}
	return nil
}
func UpdateVrfStatus(Name string, resourceVersion string, notificationId string, component common.Component) error {

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
		task_manager.TaskMan.StatusUpdated(vrf.Name, "vrf", vrf.ResourceVersion, notificationId, true, &component)
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

	// Is it ok to delete an object before we update the last component status to success ?
	if lastCompSuccsess {
		if vrf.Status.VrfOperStatus == VRF_OPER_STATUS_TO_BE_DELETED {
			err = infradb.client.Delete(vrf.Name)
			if err != nil {
				log.Fatal(err)
				return err
			}

			vrfs := make(map[string]bool)
			found, err = infradb.client.Get("vrfs", vrfs)
			if err != nil {
				log.Fatal(err)
				return err
			}
			if !found {
				fmt.Println("UpdateVrfStatus(): No VRFs has been found")
				return ErrKeyNotFound
			}

			delete(vrfs, vrf.Name)
			err = infradb.client.Set("vrfs", vrfs)
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

	svi.ResourceVersion = generateVersion()

	err := infradb.client.Set(svi.Name, svi)
	if err != nil {
		log.Fatal(err)
	}

	return err
}
func DeleteSvi(Name string) error {

	globalLock.Lock()
	defer globalLock.Unlock()

	err := infradb.client.Delete(Name)
	if err != nil {
		log.Fatal(err)
	}
	return err
}
func GetSvi(Name string) (Svi, error) {

	globalLock.Lock()
	defer globalLock.Unlock()

	svi := Svi{}
	found, err := infradb.client.Get(Name, &svi)
	if found != true {
		return svi, errors.New("KeyNotFound")
	}
	return svi, err
}
func UpdateSvi(svi *Svi) error {

	globalLock.Lock()
	defer globalLock.Unlock()

	svi.ResourceVersion = generateVersion()

	err := infradb.client.Set(svi.Name, svi)
	if err != nil {
		log.Fatal(err)
	}
	return nil
}
