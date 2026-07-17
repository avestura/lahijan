// Package computeddefault holds a registry of Go-computed default values for
// config keys that cannot be expressed as static YAML (e.g. derived from the
// runtime environment such as Fiber's DefaultConcurrency).
package computeddefault

import (
	"errors"
	"fmt"
	"net"

	"github.com/spf13/pflag"
)

type confDefault struct {
	kind  string
	value any
}

var defaults map[string]*confDefault

func init() {
	defaults = make(map[string]*confDefault)
}

func RegisterStringDefault(conf, defValue string) {
	if _, exists := defaults[conf]; exists {
		fmt.Printf("default for config '%s' already exists. re-register failed.", conf)
	} else {
		defaults[conf] = &confDefault{
			kind:  "!!str",
			value: defValue,
		}
	}
}

func RegisterIntDefault(conf string, defValue int) {
	if _, exists := defaults[conf]; exists {
		fmt.Printf("default for config '%s' already exists. re-register failed.", conf)
	} else {
		defaults[conf] = &confDefault{
			kind:  "!!int",
			value: defValue,
		}
	}
}

func RegisterFloatDefault(conf string, defValue float64) {
	if _, exists := defaults[conf]; exists {
		fmt.Printf("default for config '%s' already exists. re-register failed.", conf)
	} else {
		defaults[conf] = &confDefault{
			kind:  "!!float",
			value: defValue,
		}
	}
}

func RegisterBoolDefault(conf string, defValue bool) {
	if _, exists := defaults[conf]; exists {
		fmt.Printf("default for config '%s' already exists. re-register failed.", conf)
	} else {
		defaults[conf] = &confDefault{
			kind:  "!!bool",
			value: defValue,
		}
	}
}

func RegisterIPDefault(conf string, defValue net.IP) {
	if _, exists := defaults[conf]; exists {
		fmt.Printf("default for config '%s' already exists. re-register failed.", conf)
	} else {
		defaults[conf] = &confDefault{
			kind:  "!!ip",
			value: defValue,
		}
	}
}

func RegisterIPNetDefault(conf string, defValue net.IPNet) {
	if _, exists := defaults[conf]; exists {
		fmt.Printf("default for config '%s' already exists. re-register failed.", conf)
	} else {
		defaults[conf] = &confDefault{
			kind:  "!!ipnet",
			value: defValue,
		}
	}
}

func SetupFlagset(fs *pflag.FlagSet, conf, usage string) error {
	item, exists := defaults[conf]
	if !exists {
		return fmt.Errorf("config does not exist: %s", conf)
	}

	switch item.kind {
	case "!!str":
		v, ok := item.value.(string)
		if !ok {
			return fmt.Errorf("computed default for %s is not a string", conf)
		}
		_ = fs.String(conf, v, usage)
	case "!!int":
		v, ok := item.value.(int)
		if !ok {
			return fmt.Errorf("computed default for %s is not an int", conf)
		}
		_ = fs.Int(conf, v, usage)
	case "!!bool":
		v, ok := item.value.(bool)
		if !ok {
			return fmt.Errorf("computed default for %s is not a bool", conf)
		}
		_ = fs.Bool(conf, v, usage)
	case "!!float":
		v, ok := item.value.(float64)
		if !ok {
			return fmt.Errorf("computed default for %s is not a float64", conf)
		}
		_ = fs.Float64(conf, v, usage)
	case "!!ip":
		v, ok := item.value.(net.IP)
		if !ok {
			return fmt.Errorf("computed default for %s is not a net.IP", conf)
		}
		_ = fs.IP(conf, v, usage)
	case "!!ipnet":
		v, ok := item.value.(net.IPNet)
		if !ok {
			return fmt.Errorf("computed default for %s is not a net.IPNet", conf)
		}
		_ = fs.IPNet(conf, v, usage)
	default:
		return fmt.Errorf("unknown computed default kind %q for %s", item.kind, conf)
	}
	return nil
}

// ErrConflict is returned when a default is registered twice for the same key.
var ErrConflict = errors.New("computed default already registered for this key")
