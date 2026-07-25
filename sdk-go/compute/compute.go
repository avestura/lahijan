// Package compute provides idiomatic Go access to the Lahijan compute
// service from inside a WASM plugin. Plugins can create, list, inspect,
// control (start/stop/restart), and delete compute instances — all gated
// by compute.instance.* permissions and scoped to the plugin's tenant.
//
// Every function uses the JSON-in/JSON-out host function ABI: the SDK
// marshals args, calls the host, and unmarshals the JSON result into a
// typed Go struct. The BufferTooSmall retry is handled internally.
package compute

import (
	"encoding/json"

	"github.com/avestura/lahijan/sdk-go/mem"
	"github.com/avestura/lahijan/sdk-go/status"
)

// Instance represents a compute instance row returned by the host.
type Instance struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Type         string   `json:"type"`
	Status       string   `json:"status"`
	StatusCode   int32    `json:"status_code"`
	ImageAlias   string   `json:"image_alias"`
	Profiles     []string `json:"profiles"`
	Description  string   `json:"description"`
}

// CreateInstanceParams carries the user-controlled fields for creating
// an instance.
type CreateInstanceParams struct {
	Name       string            `json:"name"`
	Type       string            `json:"type"`
	ImageAlias string            `json:"image_alias"`
	Profiles   []string          `json:"profiles"`
	Config     map[string]string `json:"config"`
}

const (
	bufStartSize = 4096
	bufMaxSize   = 256 * 1024
)

// CreateInstance creates a new compute instance (container or VM).
func CreateInstance(params CreateInstanceParams) (Instance, error) {
	args, err := json.Marshal(params)
	if err != nil {
		return Instance{}, err
	}
	raw, err := callWithRetry(instanceCreate, args)
	if err != nil {
		return Instance{}, err
	}
	var inst Instance
	return inst, json.Unmarshal(raw, &inst)
}

// GetInstance fetches a single instance by ID.
func GetInstance(id string) (Instance, error) {
	raw, err := callWithRetry(instanceGet, mustMarshal(map[string]string{"id": id}))
	if err != nil {
		return Instance{}, err
	}
	var inst Instance
	return inst, json.Unmarshal(raw, &inst)
}

// ListInstances lists instances in the plugin's tenant.
func ListInstances(limit, offset int32) ([]Instance, error) {
	raw, err := callWithRetry(instanceList, mustMarshal(map[string]int32{"limit": limit, "offset": offset}))
	if err != nil {
		return nil, err
	}
	var insts []Instance
	return insts, json.Unmarshal(raw, &insts)
}

// SetInstanceState changes an instance's lifecycle state.
// action is "start", "stop", "restart", "freeze", or "unfreeze".
func SetInstanceState(id, action string, force bool, timeoutSecs int) (Instance, error) {
	args := struct {
		ID          string `json:"id"`
		Action      string `json:"action"`
		Force       bool   `json:"force"`
		TimeoutSecs int    `json:"timeout_secs"`
	}{id, action, force, timeoutSecs}
	raw, err := callWithRetry(instanceSetState, mustMarshal(args))
	if err != nil {
		return Instance{}, err
	}
	var inst Instance
	return inst, json.Unmarshal(raw, &inst)
}

// DeleteInstance deletes an instance by ID.
func DeleteInstance(id string) error {
	_, err := callWithRetry(instanceDelete, mustMarshal(map[string]string{"id": id}))
	return err
}

// callWithRetry calls a 4-param host function (args_ptr, args_len, buf_ptr,
// buf_cap) and retries with a larger buffer on BufferTooSmall.
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
