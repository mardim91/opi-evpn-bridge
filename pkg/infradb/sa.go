// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2022-2024 Intel Corporation, or its subsidiaries.
// Copyright (C) 2024 Ericsson AB.

package infradb

import (
	"errors"
	"fmt"
	"log"
	"net"

	"github.com/opiproject/opi-evpn-bridge/pkg/infradb/common"
	"github.com/opiproject/opi-evpn-bridge/pkg/infradb/subscriberframework/eventbus"
	pb "github.com/opiproject/opi-evpn-bridge/pkg/ipsec/gen/go"
	"github.com/opiproject/opi-evpn-bridge/pkg/utils"
)

var saIdxPoolRange = struct {
	saIdxPoolMin, saIdxPoolMax uint32
}{
	saIdxPoolMin: 1,
	saIdxPoolMax: 4000,
}

var saIdxPool,_ = utils.IDPoolInit("SaIdxPool", saIdxPoolRange.saIdxPoolMin, saIdxPoolRange.saIdxPoolMax)

// SaOperStatus operational Status for Sas
type SaOperStatus int32

const (
	// SaOperStatusUnspecified for Sa unknown state
	SaOperStatusUnspecified SaOperStatus = iota
	// SaOperStatusUp for Sa up state
	SaOperStatusUp = iota
	// SaOperStatusDown for Sa down state
	SaOperStatusDown = iota
	// SaOperStatusToBeDeleted for Sa to be deleted state
	SaOperStatusToBeDeleted = iota
)

// Protocol defines a custom type for protocol
type Protocol int

// Define constants for protocol
const (
	IPSecProtoRSVD Protocol = iota
	IPSecProtoESP
	IPSecProtoAH
)

func convertPbToProtocol(p pb.IPSecProtocol) (Protocol, error) {
	switch p {
	case pb.IPSecProtocol_IPSecProtoRSVD:
		return IPSecProtoRSVD, nil
	case pb.IPSecProtocol_IPSecProtoESP:
		return IPSecProtoESP, nil
	case pb.IPSecProtocol_IPSecProtoAH:
		return IPSecProtoAH, nil
	default:
		err := fmt.Errorf("convertPbToProtocol(): Unknown protocol %+v", p)
		return -1, err
	}
}

// Mode defines a custom type for mode
type Mode int

// Define constants for mode
const (
	NoneMode Mode = iota
	TransportMode
	TunnelMode
	BeetMode
	PassMode
	DropMode
)

func convertPbToMode(m pb.IPSecMode) (Mode, error) {
	switch m {
	case pb.IPSecMode_MODE_NONE:
		return NoneMode, nil
	case pb.IPSecMode_MODE_TRANSPORT:
		return TransportMode, nil
	case pb.IPSecMode_MODE_TUNNEL:
		return TunnelMode, nil
	case pb.IPSecMode_MODE_BEET:
		return BeetMode, nil
	case pb.IPSecMode_MODE_PASS:
		return PassMode, nil
	case pb.IPSecMode_MODE_DROP:
		return DropMode, nil
	default:
		err := fmt.Errorf("convertPbToMode(): Unknown ipsec mode %+v", m)
		return -1, err
	}
}

// CryptoAlg defines a custom type for crypto algorithm for encryption
type CryptoAlg int

// Define constants for crypto algorithm for encryption
//
//nolint:revive,stylecheck
const (
	RSVD CryptoAlg = iota
	NULL
	AES_CBC
	AES_CTR
	AES_CCM_8
	AES_CCM_12
	AES_CCM_16
	AES_GCM_8
	AES_GCM_12
	AES_GCM_16
	NULL_AUTH_AES_GMAC
	CHACHA20_POLY1305
)

func convertPbToCryptoAlg(c pb.CryptoAlgorithm) (CryptoAlg, error) {
	switch c {
	case pb.CryptoAlgorithm_ENCR_RSVD:
		return RSVD, nil
	case pb.CryptoAlgorithm_ENCR_NULL:
		return NULL, nil
	case pb.CryptoAlgorithm_ENCR_AES_CBC:
		return AES_CBC, nil
	case pb.CryptoAlgorithm_ENCR_AES_CTR:
		return AES_CTR, nil
	case pb.CryptoAlgorithm_ENCR_AES_CCM_8:
		return AES_CCM_8, nil
	case pb.CryptoAlgorithm_ENCR_AES_CCM_12:
		return AES_CCM_12, nil
	case pb.CryptoAlgorithm_ENCR_AES_CCM_16:
		return AES_CCM_16, nil
	case pb.CryptoAlgorithm_ENCR_AES_GCM_8:
		return AES_GCM_8, nil
	case pb.CryptoAlgorithm_ENCR_AES_GCM_12:
		return AES_GCM_12, nil
	case pb.CryptoAlgorithm_ENCR_AES_GCM_16:
		return AES_GCM_16, nil
	case pb.CryptoAlgorithm_ENCR_NULL_AUTH_AES_GMAC:
		return NULL_AUTH_AES_GMAC, nil
	case pb.CryptoAlgorithm_ENCR_CHACHA20_POLY1305:
		return CHACHA20_POLY1305, nil
	default:
		err := fmt.Errorf("convertPbToCryptoAlg(): Unknown ipsec crypto algorithm %+v", c)
		return -1, err
	}
}

// IntegAlg defines a custom type for crypto algorithm for authentication
type IntegAlg int

// Define constants for crypto algorithm for authentication
//
//nolint:revive,stylecheck
const (
	NONE IntegAlg = iota
	HMAC_SHA1_96
	AES_XCBC_96
	AES_CMAC_96
	AES_128_GMAC
	AES_192_GMAC
	AES_256_GMAC
	HMAC_SHA2_256_128
	HMAC_SHA2_384_192
	HMAC_SHA2_512_256
	UNDEFINED
)

func convertPbToIntegAlg(c pb.IntegAlgorithm) (IntegAlg, error) {
	switch c {
	case pb.IntegAlgorithm_NONE:
		return NONE, nil
	case pb.IntegAlgorithm_AUTH_HMAC_SHA1_96:
		return HMAC_SHA1_96, nil
	case pb.IntegAlgorithm_AUTH_AES_XCBC_96:
		return AES_XCBC_96, nil
	case pb.IntegAlgorithm_AUTH_AES_CMAC_96:
		return AES_CMAC_96, nil
	case pb.IntegAlgorithm_AUTH_AES_128_GMAC:
		return AES_128_GMAC, nil
	case pb.IntegAlgorithm_AUTH_AES_192_GMAC:
		return AES_192_GMAC, nil
	case pb.IntegAlgorithm_AUTH_AES_256_GMAC:
		return AES_256_GMAC, nil
	case pb.IntegAlgorithm_AUTH_HMAC_SHA2_256_128:
		return HMAC_SHA2_256_128, nil
	case pb.IntegAlgorithm_AUTH_HMAC_SHA2_384_192:
		return HMAC_SHA2_384_192, nil
	case pb.IntegAlgorithm_AUTH_HMAC_SHA2_512_256:
		return HMAC_SHA2_512_256, nil
	case pb.IntegAlgorithm_AUTH_UNDEFINED:
		return UNDEFINED, nil
	default:
		err := fmt.Errorf("convertPbToIntegAlg(): Unknown ipsec authentication algorithm %+v", c)
		return -1, err
	}
}

// DscpCopy defines a custom type for DSCP header field
type DscpCopy int

// Define constants for DSCP header field
//
//nolint:revive,stylecheck
const (
	OUT_ONLY DscpCopy = iota
	IN_ONLY
	YES
	NO
)

func convertPbToDscpCopy(d pb.DSCPCopy) (DscpCopy, error) {
	switch d {
	case pb.DSCPCopy_DSCP_COPY_OUT_ONLY:
		return OUT_ONLY, nil
	case pb.DSCPCopy_DSCP_COPY_IN_ONLY:
		return IN_ONLY, nil
	case pb.DSCPCopy_DSCP_COPY_YES:
		return YES, nil
	case pb.DSCPCopy_DSCP_COPY_NO:
		return NO, nil
	default:
		err := fmt.Errorf("convertPbToDscpCopy(): Unknown ipsec DscpCopy %+v", d)
		return -1, err
	}
}

func convertPbToBoolean(b pb.Bool) (bool, error) {
	switch b {
	case pb.Bool_TRUE:
		return true, nil
	case pb.Bool_FALSE:
		return false, nil
	default:
		err := fmt.Errorf("convertPbToBoolean(): Unknown boolean value %+v", b)
		return false, err
	}
}

// Lifetime holds lifetime fields
type Lifetime struct {
	Life   uint64
	Rekey  uint64
	Jitter uint64
}

// NewLifetime creates a Lifetime object
func NewLifetime(lt *pb.LifeTime) *Lifetime {
	if lt == nil {
		return nil
	}
	return &Lifetime{
		Life:   lt.Life,
		Rekey:  lt.Rekey,
		Jitter: lt.Jitter,
	}
}

// LifetimeCfg holds lifetime configuration
type LifetimeCfg struct {
	Time    *Lifetime
	Bytes   *Lifetime
	Packets *Lifetime
}

// NewLifetimeCfg creates a LifetimeCfg object
func NewLifetimeCfg(ltCfg *pb.LifeTimeCfg) *LifetimeCfg {
	if ltCfg == nil {
		return nil
	}

	lTtime := NewLifetime(ltCfg.Time)
	lTbytes := NewLifetime(ltCfg.Bytes)
	lTpackets := NewLifetime(ltCfg.Packets)

	if lTtime == nil && lTbytes == nil && lTpackets == nil {
		return nil
	}
	return &LifetimeCfg{
		Time:    lTtime,
		Bytes:   lTbytes,
		Packets: lTpackets,
	}
}

// SaStatus holds Sa Status
type SaStatus struct {
	SaOperStatus SaOperStatus
	Components   []common.Component
}

// SaSpec holds Sa Spec
type SaSpec struct {
	SrcIP        *net.IP
	DstIP        *net.IP
	Spi          *uint32
	Protocol     Protocol
	IfID         uint32
	Mode         Mode
	Interface    string
	LifetimeCfg  *LifetimeCfg
	EncAlg       CryptoAlg
	EncKey       []byte
	IntAlg       IntegAlg
	IntKey       []byte
	ReplayWindow uint32
	UDPEncap     bool
	Esn          bool
	CopyDf       bool
	CopyEcn      bool
	CopyDscp     DscpCopy
	Inbound      bool
	Vrf          string
}

// SaMetadata holds Sa Metadata
type SaMetadata struct{}

// Sa holds SA info
type Sa struct {
	Name            string
	Spec            *SaSpec
	Status          *SaStatus
	Metadata        *SaMetadata
	Vrf             string
	Index           *uint32
	OldVersions     []string
	ResourceVersion string
}

// NewSa creates new SA object from protobuf message
func NewSa(name string, sa *pb.AddSAReq) (*Sa, error) {
	components := make([]common.Component, 0)
	
        saIndex := saIdxPool.GetID(name)
	if saIndex == 0 {
		return nil, errors.New("NewSa(): Failed to get id from the pool for SA")
	}

	srcIP := net.ParseIP(sa.SaId.Src)
	if srcIP == nil {
		err := fmt.Errorf("NewSa(): Incorrect src IP format %+v", sa.SaId.Src)
		return nil, err
	}

	dstIP := net.ParseIP(sa.SaId.Dst)
	if dstIP == nil {
		err := fmt.Errorf("NewSa(): Incorrect dst IP format %+v", sa.SaId.Dst)
		return nil, err
	}

	proto, err := convertPbToProtocol(sa.SaId.Proto)
	if err != nil {
		return nil, err
	}
	mode, err := convertPbToMode(sa.SaData.Mode)
	if err != nil {
		return nil, err
	}

	lifetimeCfg := NewLifetimeCfg(sa.SaData.Lifetime)

	encAlg, err := convertPbToCryptoAlg(sa.SaData.EncAlg)
	if err != nil {
		return nil, err
	}

	intAlg, err := convertPbToIntegAlg(sa.SaData.IntAlg)
	if err != nil {
		return nil, err
	}

	udpEncap, err := convertPbToBoolean(sa.SaData.Encap)
	if err != nil {
		return nil, err
	}

	esn, err := convertPbToBoolean(sa.SaData.Esn)
	if err != nil {
		return nil, err
	}

	copyDf, err := convertPbToBoolean(sa.SaData.CopyDf)
	if err != nil {
		return nil, err
	}

	copyEcn, err := convertPbToBoolean(sa.SaData.CopyEcn)
	if err != nil {
		return nil, err
	}

	copyDscp, err := convertPbToDscpCopy(sa.SaData.CopyDscp)
	if err != nil {
		return nil, err
	}

	inbound, err := convertPbToBoolean(sa.SaData.Inbound)
	if err != nil {
		return nil, err
	}

	subscribers := eventbus.EBus.GetSubscribers("sa")
	if len(subscribers) == 0 {
		log.Println("NewSa(): No subscribers for SA objects")
		return nil, errors.New("no subscribers found for SAs")
	}

	for _, sub := range subscribers {
		component := common.Component{Name: sub.Name, CompStatus: common.ComponentStatusPending, Details: ""}
		components = append(components, component)
	}

	return &Sa{
		Name: name,
		Spec: &SaSpec{
			SrcIP:        &srcIP,
			DstIP:        &dstIP,
			Spi:          &sa.SaId.Spi,
			Protocol:     proto,
			IfID:         sa.SaId.IfId,
			Mode:         mode,
			Interface:    sa.SaData.Interface,
			LifetimeCfg:  lifetimeCfg,
			EncAlg:       encAlg,
			EncKey:       sa.SaData.EncKey,
			IntAlg:       intAlg,
			IntKey:       sa.SaData.IntKey,
			ReplayWindow: sa.SaData.ReplayWindow,
			UDPEncap:     udpEncap,
			Esn:          esn,
			CopyDf:       copyDf,
			CopyEcn:      copyEcn,
			CopyDscp:     copyDscp,
			Inbound:      inbound,
		},
		Status: &SaStatus{
			SaOperStatus: SaOperStatus(SaOperStatusDown),

			Components: components,
		},
		Metadata:        &SaMetadata{},
		Index:           &saIndex,
		ResourceVersion: generateVersion(),
	}, nil
}

// setComponentState set the stat of the component
func (in *Sa) setComponentState(component common.Component) {
	saComponents := in.Status.Components
	for i, comp := range saComponents {
		if comp.Name == component.Name {
			in.Status.Components[i] = component
			break
		}
	}
}

// checkForAllSuccess check if all the components are in Success state
func (in *Sa) checkForAllSuccess() bool {
	for _, comp := range in.Status.Components {
		if comp.CompStatus != common.ComponentStatusSuccess {
			return false
		}
	}
	return true
}

// parseMeta parse metadata
func (in *Sa) parseMeta(saMeta *SaMetadata) {
	if saMeta != nil {
		in.Metadata = saMeta
	}
}

// prepareObjectsForReplay prepares an object for replay by setting the unsuccessful components
// in pending state and returning a list of the components that need to be contacted for the
// replay of the particular object that called the function.
func (in *Sa) prepareObjectsForReplay(componentName string, saSubs []*eventbus.Subscriber) []*eventbus.Subscriber {
	// We assume that the list of Components that are returned
	// from DB is ordered based on the priority as that was the
	// way that has been stored in the DB in first place.
	saComponents := in.Status.Components
	tempSubs := []*eventbus.Subscriber{}
	for i, comp := range saComponents {
		if comp.Name == componentName || comp.CompStatus != common.ComponentStatusSuccess {
			in.Status.Components[i] = common.Component{Name: comp.Name, CompStatus: common.ComponentStatusPending, Details: ""}
			tempSubs = append(tempSubs, saSubs[i])
		}
	}
	if in.Status.SaOperStatus == SaOperStatusUp {
		in.Status.SaOperStatus = SaOperStatusDown
	}

	in.ResourceVersion = generateVersion()
	return tempSubs
}

func (in *Sa) releaseSaPoolIndex() {
	saIdxPool.ReleaseID(in.Name)
}
