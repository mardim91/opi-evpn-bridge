// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2023 Nordix Foundation.

package infradb

import (
	"errors"
	"fmt"
	"github.com/philippgille/gokv"
	"log"
	"sync"
	"github.com/opiproject/opi-evpn-bridge/pkg/infradb/subsrciber_framework/event_bus"
	"github.com/opiproject/opi-evpn-bridge/pkg/storage"
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

	vrf.ResourceVersion = generateVersion()
	subscribers := event_bus.EBus.GetSubscribers("vrf")
	if subscribers == nil {
		fmt.Printf("No subscriber for Vrf: \n")
	}

	for _, sub := range subscribers {
		component := Component{Name: sub.Name, CompStatus: COMP_STATUS_PENDING, Details: ""}
		vrf.Status.Components = append(vrf.Status.Components, component)
	}

	fmt.Printf("Create Vrf: %+v\n", vrf)

	err := infradb.client.Set(vrf.Name, vrf)
	if err != nil {
		log.Fatal(err)
		return err
	}

	/* Create task manager task
	taskMgr.CreateTask(vrf.name,vrf.ResourceVersion, subscribers )
	*/

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
func UpdateVrf(vrf *Vrf) error {

	globalLock.Lock()
	defer globalLock.Unlock()

	vrf.ResourceVersion = generateVersion()

	for _, component := range vrf.Status.Components {
		component.CompStatus = COMP_STATUS_PENDING
		fmt.Printf("Component: %s, Value: %d\n", component.Name, component.CompStatus)
	}
	err := infradb.client.Set(vrf.Name, vrf)
	if err != nil {
		log.Fatal(err)
	}
	return nil
}
func UpdateVrfStatus(Name string, resourceVersion string, component Component) error {

	vrf := Vrf{}
	found, err := infradb.client.Get(Name, &vrf)
	if found != true {
		return ErrKeyNotFound
	}

	vrf.ResourceVersion = resourceVersion

	for i, comp := range vrf.Status.Components {
		if comp.Name == component.Name {

			vrf.Status.Components[i] = component

			err = infradb.client.Set(vrf.Name, vrf)

			if err != nil {
				log.Fatal(err)
				return err
			}

			return nil
		}
	}

	/*vrf.Status.Components = append(vrf.Status.Components, component)

	err = infradb.client.Set(vrf.Name, vrf)

	if err != nil {
		log.Fatal(err)
		return err
	}*/

	/* Create task manager task
	taskMgr.StatusUpdated(vrf.name,vrf.ResourceVersion, component )
	*/

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
