package computeddefault

import (
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

func RegisterStringDefault(conf string, defValue string) {
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

func SetupFlagset(fs *pflag.FlagSet, conf string, usage string) error {
	if item, exists := defaults[conf]; exists {
		switch item.kind {
		case "!!str":
			fs.String(conf, item.value.(string), usage)
		case "!!int":
			fs.Int(conf, item.value.(int), usage)
		case "!!bool":
			fs.Bool(conf, item.value.(bool), usage)
		case "!!float":
			fs.Float64(conf, item.value.(float64), usage)
		case "!!ip":
			fs.IP(conf, item.value.(net.IP), usage)
		case "!!ipnet":
			fs.IPNet(conf, item.value.(net.IPNet), usage)
		default:
			return fmt.Errorf("type or value for config '%s' was not correct.", conf)
		}
		return nil
	}
	return fmt.Errorf("config does not exists: %s", conf)
}
