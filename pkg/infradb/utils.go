// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2023-2024 Intel Corporation, or its subsidiaries.
// Copyright (c) 2024 Ericsson AB

// Package infradb exposes the interface for the manipulation of the api objects
package infradb

import (
	"fmt"
	"log"

	"github.com/opiproject/opi-evpn-bridge/pkg/infradb/subscriberframework/actionbus"
	"github.com/opiproject/opi-evpn-bridge/pkg/infradb/subscriberframework/eventbus"
	"github.com/opiproject/opi-evpn-bridge/pkg/infradb/taskmanager"
)

func startReplayProcedure(componentName string) {
	globalLock.Lock()

	var deferErr error
	var preSubscriber *actionbus.Subscriber

	defer func() {
		if deferErr != nil {
			log.Println("startReplayProcedure(): The replay procedure has failed")
			log.Println("startReplayProcedure(): unblocking the TaskManager to continue")
			taskmanager.TaskMan.ReplayFinished()
		}
	}()

	preSubscribers := actionbus.ABus.GetSubscribers("preReplay")

	for _, preSub := range preSubscribers {
		if preSub.Name == componentName {
			preSubscriber = preSub
			break
		}
	}

	if preSubscriber == nil {
		deferErr = fmt.Errorf("no pre-replay subscriber for %s", componentName)
		log.Printf("startReplayProcedure(): Error %+v\n", deferErr)
		return
	}

	// Notify the preReplay subscriber
	actionData := actionbus.NewActionData()
	deferErr = actionbus.ABus.Publish(actionData, preSubscriber)
	if deferErr != nil {
		log.Printf("startReplayProcedure(): Error %+v\n", deferErr)
		return
	}

	// Waiting for the pre-replay procedure to finish
	deferErr = <-actionData.ErrCh
	close(actionData.ErrCh)

	if deferErr != nil {
		log.Printf("startReplayProcedure(): Error %+v\n", deferErr)
		return
	}

	log.Printf("startReplayProcedure(): Component %s has successfully executed pre-replay steps", componentName)

	objectTypesToReplay := getObjectTypesToReplay(componentName)

	objectsToReplay, subsForReplay, err := gatherObjectsAndSubsToReplay(componentName, objectTypesToReplay)

	if err != nil {
		log.Printf("startReplayProcedure(): Error %+v\n", err)
		deferErr = err
		return
	}

	// Releasing the lock as all the operations in the DB has finished
	globalLock.Unlock()

	// Notify task manager to continue processing tasks as
	// the replay of objects in the DB has finished
	taskmanager.TaskMan.ReplayFinished()

	createReplayTasks(objectsToReplay, subsForReplay)
}

// getObjectTypesToReplay collects all the types of object to be replayed
// which are related to the component that called the replay.
func getObjectTypesToReplay(componentName string) []string {
	objectTypesToReplay := []string{}

	tunRepSubs := eventbus.EBus.GetSubscribers("tun-rep")
	bpSubs := eventbus.EBus.GetSubscribers("bridge-port")
	sviSubs := eventbus.EBus.GetSubscribers("svi")
	lbSubs := eventbus.EBus.GetSubscribers("logical-bridge")
	vrfSubs := eventbus.EBus.GetSubscribers("vrf")
	saSubs := eventbus.EBus.GetSubscribers("sa")

	for _, vrfSub := range vrfSubs {
		if vrfSub.Name == componentName {
			objectTypesToReplay = append(objectTypesToReplay, "vrf")
			break
		}
	}

	for _, lbSub := range lbSubs {
		if lbSub.Name == componentName {
			objectTypesToReplay = append(objectTypesToReplay, "logical-bridge")
			break
		}
	}

	for _, saSub := range saSubs {
		if saSub.Name == componentName {
			objectTypesToReplay = append(objectTypesToReplay, "sa")
			break
		}
	}

	for _, sviSub := range sviSubs {
		if sviSub.Name == componentName {
			objectTypesToReplay = append(objectTypesToReplay, "svi")
			break
		}
	}

	for _, bpSub := range bpSubs {
		if bpSub.Name == componentName {
			objectTypesToReplay = append(objectTypesToReplay, "bridge-port")
			break
		}
	}

	for _, tunRepSub := range tunRepSubs {
		if tunRepSub.Name == componentName {
			objectTypesToReplay = append(objectTypesToReplay, "tun-rep")
			break
		}
	}

	return objectTypesToReplay
}

// nolint: funlen, gocognit, gocyclo
func gatherObjectsAndSubsToReplay(componentName string, objectTypesToReplay []string) ([]interface{}, [][]*eventbus.Subscriber, error) {
	objectsToReplay := []interface{}{}
	subsForReplay := [][]*eventbus.Subscriber{}

	tunRepSubs := eventbus.EBus.GetSubscribers("tun-rep")
	bpSubs := eventbus.EBus.GetSubscribers("bridge-port")
	sviSubs := eventbus.EBus.GetSubscribers("svi")
	lbSubs := eventbus.EBus.GetSubscribers("logical-bridge")
	vrfSubs := eventbus.EBus.GetSubscribers("vrf")
	saSubs := eventbus.EBus.GetSubscribers("sa")

	for _, objType := range objectTypesToReplay {
		switch objType {
		case "vrf":
			vrfsMap := make(map[string]bool)
			found, err := infradb.client.Get("vrfs", &vrfsMap)

			if err != nil {
				return nil, nil, err
			}

			if !found {
				log.Println("gatherObjectsAndSubsToReplay(): No VRFs have been found")
				continue
			}

			for key := range vrfsMap {
				vrf := &Vrf{}
				found, err := infradb.client.Get(key, vrf)
				if err != nil {
					return nil, nil, err
				}

				// Dimitris: Do we need to just continue here or throw error and stop ?
				if !found {
					return nil, nil, ErrKeyNotFound
				}

				// tempSubs holds the subscribers list to be contacted for every VRF object each time
				// for replay
				tempSubs := vrf.prepareObjectsForReplay(componentName, vrfSubs)

				err = infradb.client.Set(vrf.Name, vrf)
				if err != nil {
					return nil, nil, err
				}

				subsForReplay = append(subsForReplay, tempSubs)
				objectsToReplay = append(objectsToReplay, vrf)
			}
		case "logical-bridge":
			lbsMap := make(map[string]bool)
			found, err := infradb.client.Get("lbs", &lbsMap)
			if err != nil {
				return nil, nil, err
			}

			if !found {
				log.Println("gatherObjectsAndSubsToReplay(): No Logical Bridges have been found")
				continue
			}
			for key := range lbsMap {
				lb := &LogicalBridge{}
				found, err := infradb.client.Get(key, lb)
				if err != nil {
					return nil, nil, err
				}

				if !found {
					return nil, nil, ErrKeyNotFound
				}

				// tempSubs holds the subscribers list to be contacted for every Logical Bridge object each time
				// for replay
				tempSubs := lb.prepareObjectsForReplay(componentName, lbSubs)

				err = infradb.client.Set(lb.Name, lb)
				if err != nil {
					return nil, nil, err
				}
				subsForReplay = append(subsForReplay, tempSubs)
				objectsToReplay = append(objectsToReplay, lb)
			}
		case "sa":
			sasMap := make(map[string]bool)
			found, err := infradb.client.Get("sas", &sasMap)

			if err != nil {
				return nil, nil, err
			}

			if !found {
				log.Println("gatherObjectsAndSubsToReplay(): No SAs have been found")
				continue
			}

			for key := range sasMap {
				sa := &Sa{}
				found, err := infradb.client.Get(key, sa)
				if err != nil {
					return nil, nil, err
				}

				if !found {
					return nil, nil, ErrKeyNotFound
				}

				// tempSubs holds the subscribers list to be contacted for every SA object each time
				// for replay
				tempSubs := sa.prepareObjectsForReplay(componentName, saSubs)

				err = infradb.client.Set(sa.Name, sa)
				if err != nil {
					return nil, nil, err
				}

				subsForReplay = append(subsForReplay, tempSubs)
				objectsToReplay = append(objectsToReplay, sa)
			}
		case "svi":
			svisMap := make(map[string]bool)
			found, err := infradb.client.Get("svis", &svisMap)
			if err != nil {
				return nil, nil, err
			}

			if !found {
				log.Println("gatherObjectsAndSubsToReplay(): No SVIs have been found")
				continue
			}
			for key := range svisMap {
				svi := &Svi{}
				found, err := infradb.client.Get(key, svi)
				if err != nil {
					return nil, nil, err
				}

				if !found {
					return nil, nil, ErrKeyNotFound
				}

				// tempSubs holds the subscribers list to be contacted for every SVI object each time
				// for replay
				tempSubs := svi.prepareObjectsForReplay(componentName, sviSubs)

				err = infradb.client.Set(svi.Name, svi)
				if err != nil {
					return nil, nil, err
				}
				subsForReplay = append(subsForReplay, tempSubs)
				objectsToReplay = append(objectsToReplay, svi)
			}
		case "bp":
			bpsMap := make(map[string]bool)
			found, err := infradb.client.Get("bps", &bpsMap)
			if err != nil {
				return nil, nil, err
			}

			if !found {
				log.Println("gatherObjectsAndSubsToReplay(): No Bridge Ports have been found")
				continue
			}
			for key := range bpsMap {
				bp := &BridgePort{}
				found, err := infradb.client.Get(key, bp)
				if err != nil {
					return nil, nil, err
				}

				if !found {
					return nil, nil, ErrKeyNotFound
				}

				// tempSubs holds the subscribers list to be contacted for every Bridge Port object each time
				// for replay
				tempSubs := bp.prepareObjectsForReplay(componentName, bpSubs)

				err = infradb.client.Set(bp.Name, bp)
				if err != nil {
					return nil, nil, err
				}
				subsForReplay = append(subsForReplay, tempSubs)
				objectsToReplay = append(objectsToReplay, bp)
			}
		case "tun-rep":
			tunRepsMap := make(map[string]bool)
			found, err := infradb.client.Get("tunreps", &tunRepsMap)
			if err != nil {
				return nil, nil, err
			}

			if !found {
				log.Println("gatherObjectsAndSubsToReplay(): No tunnel representors have been found")
				continue
			}
			for key := range tunRepsMap {
				tunRep := &TunRep{}
				found, err := infradb.client.Get(key, tunRep)
				if err != nil {
					return nil, nil, err
				}

				if !found {
					return nil, nil, ErrKeyNotFound
				}

				// tempSubs holds the subscribers list to be contacted for every TunRep object each time
				// for replay
				tempSubs := tunRep.prepareObjectsForReplay(componentName, tunRepSubs)

				err = infradb.client.Set(tunRep.Name, tunRep)
				if err != nil {
					return nil, nil, err
				}
				subsForReplay = append(subsForReplay, tempSubs)
				objectsToReplay = append(objectsToReplay, tunRep)
			}
		}
	}

	return objectsToReplay, subsForReplay, nil
}

// createReplayTasks create new tasks for the realization of the new replay objects intents
func createReplayTasks(objectsToReplay []interface{}, subsForReplay [][]*eventbus.Subscriber) {
	for i, obj := range objectsToReplay {
		switch tempObj := obj.(type) {
		case *Vrf:
			taskmanager.TaskMan.CreateTask(tempObj.Name, "vrf", tempObj.ResourceVersion, subsForReplay[i])
		case *LogicalBridge:
			taskmanager.TaskMan.CreateTask(tempObj.Name, "logical-bridge", tempObj.ResourceVersion, subsForReplay[i])
		case *Sa:
			taskmanager.TaskMan.CreateTask(tempObj.Name, "sa", tempObj.ResourceVersion, subsForReplay[i])
		case *Svi:
			taskmanager.TaskMan.CreateTask(tempObj.Name, "svi", tempObj.ResourceVersion, subsForReplay[i])
		case *BridgePort:
			taskmanager.TaskMan.CreateTask(tempObj.Name, "bridge-port", tempObj.ResourceVersion, subsForReplay[i])
		case *TunRep:
			taskmanager.TaskMan.CreateTask(tempObj.Name, "tun-rep", tempObj.ResourceVersion, subsForReplay[i])
		default:
			log.Printf("createReplayTasks: Unknown object type %+v\n", tempObj)
		}
	}
}