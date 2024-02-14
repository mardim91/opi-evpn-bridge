package p4driverAPI

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"net"
	"strings"
	"time"

	proto "github.com/golang/protobuf/proto"
	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc"

	p4_v1 "github.com/p4lang/p4runtime/go/p4/v1"

	"github.com/antoninbas/p4runtime-go-client/pkg/client"
	"github.com/antoninbas/p4runtime-go-client/pkg/signals"
)

const (
	defaultDeviceID = 1
)

var (
	Ctx context.Context

	P4RtC *client.Client
)

type TableEntry struct {
	Tablename string
	TableField
	Action
}

type Action struct {
	Action_name string
	Params      []interface{}
}

type TableField struct {
	FieldValue map[string][2]interface{}
	Priority   int32
}

func uint16toBytes(val uint16) []byte {
	return []byte{byte(val >> 8), byte(val)}
}
func boolToBytes(val bool) []byte {
	if val {
		return []byte{1}
	}
	return []byte{0}
}

func uint32toBytes(num uint32) []byte {
	bytes := make([]byte, 4)
	binary.BigEndian.PutUint32(bytes, num)
	return bytes
}
func Buildmfs(tablefield TableField) (map[string]client.MatchInterface, bool, error) {
	var isTernary bool
	isTernary = false
	mfs := map[string]client.MatchInterface{}
	for key, value := range tablefield.FieldValue {
		switch v := value[0].(type) {
		case net.HardwareAddr:
			mfs[key] = &client.ExactMatch{Value: value[0].(net.HardwareAddr)}
		case uint16:
			if value[1].(string) == "lpm" {
				mfs[key] = &client.LpmMatch{Value: uint16toBytes(value[0].(uint16)), PLen: 31}
			} else if value[1].(string) == "ternary" {
				isTernary = true
				mfs[key] = &client.TernaryMatch{Value: uint16toBytes(value[0].(uint16)), Mask: uint32toBytes(4294967295)}
			} else {
				mfs[key] = &client.ExactMatch{Value: uint16toBytes(value[0].(uint16))}
			}
		case *net.IPNet:
			maskSize, _ := v.Mask.Size()
			ip := v.IP.To4()
			if value[1].(string) == "lpm" {
				mfs[key] = &client.LpmMatch{Value: v.IP.To4(), PLen: int32(maskSize)}
			} else if value[1].(string) == "ternary" {
				isTernary = true
				mfs[key] = &client.TernaryMatch{Value: []byte(ip), Mask: uint32toBytes(4294967295)}
			} else {
				mfs[key] = &client.ExactMatch{Value: []byte(ip)}
			}
		case net.IP:
			if value[1].(string) == "lpm" {
				mfs[key] = &client.LpmMatch{Value: value[0].(net.IP).To4(), PLen: 24}
			} else if value[1].(string) == "ternary" {
				isTernary = true
				mfs[key] = &client.TernaryMatch{Value: []byte(v), Mask: uint32toBytes(4294967295)}
			} else {
				mfs[key] = &client.ExactMatch{Value: []byte(v)}
			}
		case bool:
			mfs[key] = &client.ExactMatch{Value: boolToBytes(value[0].(bool))}
		case uint32:
			if value[1].(string) == "lpm" {
				mfs[key] = &client.LpmMatch{Value: uint32toBytes(value[0].(uint32)), PLen: 31}
			} else if value[1].(string) == "ternary" {
				isTernary = true
				mfs[key] = &client.TernaryMatch{Value: uint32toBytes(value[0].(uint32)), Mask: uint32toBytes(4294967295)}
			} else {
				mfs[key] = &client.ExactMatch{Value: uint32toBytes(value[0].(uint32))}
			}
		default:
			fmt.Println("Unknown field", v)
			return mfs, false, fmt.Errorf("invalid inputtype %d for %s", v, key)
		}
	}
	return mfs, isTernary, nil
}

func Get_entry(table string) ([]*p4_v1.TableEntry, error) {
	// mfs, isTernary, err := Buildmfs(Entry.TableField)
	//if err != nil {
	//	log.Fatalf("Error in Building mfs: %v and isTernary: %v", err,isTernary)
	//return p4_v1.TableEntry,err
	//}
	//	entry, err1 := P4RtC.ReadTableEntry(Ctx, Entry.Tablename, mfs)
	entry, err1 := P4RtC.ReadTableEntryWildcard(Ctx, table)
	return entry, err1
}

func Del_entry(Entry TableEntry) error {
	Options := &client.TableEntryOptions{
		Priority: Entry.TableField.Priority,
	}
	mfs, isTernary, err := Buildmfs(Entry.TableField)
	if err != nil {
		log.Fatalf("Error in Building mfs: %v", err)
		return err
	}
	if isTernary {
		entry := P4RtC.NewTableEntry(Entry.Tablename, mfs, nil, Options)
		fmt.Println("Delete Table Name---", Entry.Tablename)
		fmt.Println("Delete Rule----", entry)
		return P4RtC.DeleteTableEntry(Ctx, entry)
	} else {
		entry := P4RtC.NewTableEntry(Entry.Tablename, mfs, nil, nil)
		fmt.Println("Delete Table Name---", Entry.Tablename)
		fmt.Println("Delete Rule----", entry)
		return P4RtC.DeleteTableEntry(Ctx, entry)
	}
}

func mustMarshal(msg proto.Message) []byte {
	data, err := proto.Marshal(msg)
	if err != nil {
		panic(err) // You should handle errors appropriately in your code
	}
	return data
}

func Add_entry(Entry TableEntry) error {
	Options := &client.TableEntryOptions{
		Priority: Entry.TableField.Priority + 1,
	}
	mfs, isTernary, err := Buildmfs(Entry.TableField)
	if err != nil {
		log.Fatalf("Error in Building mfs: %v", err)
		return err
	}
	params := make([][]byte, len(Entry.Action.Params))
	for i := 0; i < len(Entry.Action.Params); i++ {
		switch v := Entry.Action.Params[i].(type) {
		case uint16:
			buf := new(bytes.Buffer)
			err1 := binary.Write(buf, binary.BigEndian, v)
			if err1 != nil {
				fmt.Println("binary.Write failed:", err1)
				return err1
			}
			params[i] = buf.Bytes()
		case uint32:
			buf := new(bytes.Buffer)
			err1 := binary.Write(buf, binary.BigEndian, v)
			if err1 != nil {
				fmt.Println("binary.Write failed:", err1)
				return err1
			}
			params[i] = buf.Bytes()
		case net.HardwareAddr:
			params[i] = v
		case net.IP:
			params[i] = v
		default:
			fmt.Println("Unknown actionparam", v)
			return nil
		}
	}

	actionSet := P4RtC.NewTableActionDirect(Entry.Action.Action_name, params)

	if isTernary {
		entry := P4RtC.NewTableEntry(Entry.Tablename, mfs, actionSet, Options)
		fmt.Println("isTernary Table Name---", Entry.Tablename)
		fmt.Println("Rule----", entry)
		return P4RtC.InsertTableEntry(Ctx, entry)
	} else {
		entry := P4RtC.NewTableEntry(Entry.Tablename, mfs, actionSet, nil)
		fmt.Println("Table Name---", Entry.Tablename)
		fmt.Println("Rule----", entry)
		return P4RtC.InsertTableEntry(Ctx, entry)
	}
}
func encodeMac(macAddrString string) []byte {
	str := strings.Replace(macAddrString, ":", "", -1)
	decoded, _ := hex.DecodeString(str)
	return decoded
}
func NewP4RuntimeClient(binPath string, p4infoPath string, conn *grpc.ClientConn) error {
	Ctx = context.Background()
	c := p4_v1.NewP4RuntimeClient(conn)
	resp, err := c.Capabilities(Ctx, &p4_v1.CapabilitiesRequest{})
	if err != nil {
		log.Fatalf("Error in Capabilities RPC: %v", err)
		return err
	}
	log.Infof("P4Runtime server version is %s", resp.P4RuntimeApiVersion)

	stopCh := signals.RegisterSignalHandlers()

	electionID := &p4_v1.Uint128{High: 0, Low: 1}

	P4RtC = client.NewClient(c, defaultDeviceID, electionID)
	arbitrationCh := make(chan bool)
	go P4RtC.Run(stopCh, arbitrationCh, nil)

	waitCh := make(chan struct{})

	go func() {
		sent := false
		for isPrimary := range arbitrationCh {
			if isPrimary {
				log.Infof("We are the primary client!")
				if !sent {
					waitCh <- struct{}{}
					sent = true
				}
			} else {
				log.Infof("We are not the primary client!")
			}
		}
	}()

	func() {
		timeout := 5 * time.Second
		Ctx2, cancel := context.WithTimeout(Ctx, timeout)
		defer cancel()
		select {
		case <-Ctx2.Done():
			log.Fatalf("Could not become the primary client within %v", timeout)
		case <-waitCh:
		}
	}()
	log.Info("Setting forwarding pipe")
	if _, err := P4RtC.SetFwdPipe(Ctx, binPath, p4infoPath, 0); err != nil {
		log.Fatalf("Error when setting forwarding pipe: %v", err)
		return err
	}
	return nil
}
