// Package dns provides idiomatic Go access to the Lahijan DNS service
// from inside a WASM plugin. Plugins can create, list, and delete DNS
// zones and records — all gated by dns.zone.* / dns.record.* permissions
// and scoped to the plugin's tenant.
package dns

import (
	"encoding/json"

	"github.com/avestura/lahijan/sdk-go/mem"
	"github.com/avestura/lahijan/sdk-go/status"
)

// Zone represents a DNS zone.
type Zone struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Kind        string `json:"kind"`
	Description string `json:"description"`
}

// Record represents a DNS record within a zone.
type Record struct {
	ID      string `json:"id"`
	ZoneID  string `json:"zone_id"`
	Name    string `json:"name"`
	Type    string `json:"type"`
	Content string `json:"content"`
	TTL     int    `json:"ttl"`
}

// CreateZoneParams carries the fields for creating a DNS zone.
type CreateZoneParams struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Kind        string `json:"kind"`
}

// CreateRecordParams carries the fields for creating a DNS record.
type CreateRecordParams struct {
	ZoneID  string `json:"zone_id"`
	Name    string `json:"name"`
	Type    string `json:"type"`
	Content string `json:"content"`
	TTL     int    `json:"ttl"`
}

const (
	bufStartSize = 4096
	bufMaxSize   = 256 * 1024
)

// CreateZone creates a new DNS zone.
func CreateZone(params CreateZoneParams) (Zone, error) {
	raw, err := callWithRetry(zoneCreate, mustMarshal(params))
	if err != nil {
		return Zone{}, err
	}
	var z Zone
	return z, json.Unmarshal(raw, &z)
}

// GetZone fetches a zone by ID.
func GetZone(id string) (Zone, error) {
	raw, err := callWithRetry(zoneGet, mustMarshal(map[string]string{"id": id}))
	if err != nil {
		return Zone{}, err
	}
	var z Zone
	return z, json.Unmarshal(raw, &z)
}

// ListZones lists DNS zones in the plugin's tenant.
func ListZones(limit, offset int32) ([]Zone, error) {
	raw, err := callWithRetry(zoneList, mustMarshal(map[string]int32{"limit": limit, "offset": offset}))
	if err != nil {
		return nil, err
	}
	var zones []Zone
	return zones, json.Unmarshal(raw, &zones)
}

// DeleteZone deletes a zone by ID.
func DeleteZone(id string) error {
	_, err := callWithRetry(zoneDelete, mustMarshal(map[string]string{"id": id}))
	return err
}

// CreateRecord creates a new DNS record within a zone.
func CreateRecord(params CreateRecordParams) (Record, error) {
	raw, err := callWithRetry(recordCreate, mustMarshal(params))
	if err != nil {
		return Record{}, err
	}
	var r Record
	return r, json.Unmarshal(raw, &r)
}

// ListRecords lists DNS records within a zone.
func ListRecords(zoneID string, limit, offset int32) ([]Record, error) {
	raw, err := callWithRetry(recordList, mustMarshal(map[string]any{"zone_id": zoneID, "limit": limit, "offset": offset}))
	if err != nil {
		return nil, err
	}
	var recs []Record
	return recs, json.Unmarshal(raw, &recs)
}

// DeleteRecord deletes a DNS record by zone ID + record ID.
func DeleteRecord(zoneID, recordID string) error {
	_, err := callWithRetry(recordDelete, mustMarshal(map[string]string{"zone_id": zoneID, "record_id": recordID}))
	return err
}

func callWithRetry(fn func(argsPtr, argsLen, bufPtr, bufCap uint32) int32, args []byte) ([]byte, error) {
	buf := make([]byte, bufStartSize)
	for {
		n := fn(mem.Ptr(args), mem.Len(args), mem.Ptr(buf), uint32(len(buf)))
		if status.Code(n) == status.BufferTooSmall {
			if len(buf) >= bufMaxSize {
				return nil, status.ErrBufferTooSmall
			}
			buf = make([]byte, len(buf)*2)
			continue
		}
		if err := status.FromCode(n); err != nil {
			return nil, err
		}
		return buf[:n], nil
	}
}

func mustMarshal(v any) []byte {
	out, _ := json.Marshal(v)
	return out
}
