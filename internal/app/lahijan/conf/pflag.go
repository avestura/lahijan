package conf

import (
	"fmt"
	"log"
	"net"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	computeddefault "github.com/avestura/lahijan/internal/app/lahijan/conf/computedDefault"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
	"gopkg.in/yaml.v3"
)

var configFlagSet *pflag.FlagSet

func InitConfigBindFlags() {
	configFlagSet = pflag.NewFlagSet("config", pflag.ExitOnError)

	node := yaml.Node{}
	if err := yaml.Unmarshal(defaultConfig, &node); err != nil {
		log.Fatalf("failed to parse YAML: %v", err)
	}

	declareFlagsFromYAMLRecursive(configFlagSet, &node, "")

	configFlagSet.Parse(os.Args[1:])

	if err := viper.BindPFlags(configFlagSet); err != nil {
		log.Fatalf("error binding flags to configuration: %v", err)
	}
}

func declareFlagsFromYAMLRecursive(flags *pflag.FlagSet, node *yaml.Node, path string) {
	if node.Kind == yaml.DocumentNode {
		declareFlagsFromYAMLRecursive(flags, node.Content[0], path)
	}

	if node.Kind != yaml.MappingNode {
		return
	}

	for i := 0; i < len(node.Content); i += 2 {
		keyNode := node.Content[i]
		valueNode := node.Content[i+1]

		key := keyNode.Value
		fullPath := key
		if path != "" {
			fullPath = path + "." + key
		}

		defVal := valueNode.Value
		usage := strings.TrimPrefix(keyNode.HeadComment, "#")
		usage = strings.TrimSpace(usage)
		switch valueNode.Kind {
		case yaml.MappingNode:
			declareFlagsFromYAMLRecursive(flags, valueNode, fullPath)
		case yaml.SequenceNode:
			seqType, err := detectSequenceNodeTypes(valueNode)
			if err != nil {
				log.Printf("sequence configuration not supported. conf: %s, error: %s", fullPath, err.Error())
			} else {
				switch seqType {
				case "!!int":
					flags.IntSlice(fullPath, nodesToIntSlice(valueNode.Content), usage)
				case "!!bool":
					flags.BoolSlice(fullPath, nodesToBoolSlice(valueNode.Content), usage)
				case "!!float":
					flags.Float64Slice(fullPath, nodesToFloatSlice(valueNode.Content), usage)
				case "!!timestamp":
					flags.StringSlice(fullPath, nodesToTimestampSlice(valueNode.Content), usage)
				case "!!str":
					flags.StringSlice(fullPath, nodesToStringSlice(valueNode.Content), usage)
				case "!!ip":
					flags.IPSlice(fullPath, nodesToIPSlice(valueNode.Content), usage)
				case "!!ipnet":
					flags.IPNetSlice(fullPath, nodesToIPNetSlice(valueNode.Content), usage)
				}
			}
		case yaml.ScalarNode:

			switch valueNode.Tag {
			case "!!int":
				var intVal int
				fmt.Sscanf(defVal, "%d", &intVal)
				flags.Int(fullPath, intVal, usage)
			case "!!bool":
				var boolVal bool
				fmt.Sscanf(defVal, "%t", &boolVal)
				flags.Bool(fullPath, boolVal, usage)

			case "!!str":
				if defVal == "$go-computed" {
					err := computeddefault.SetupFlagset(flags, fullPath, usage)
					if err != nil {
						log.Printf("configuration was using runtime default, but failed to evaluate. conf: %s, value: %s, err: %s", fullPath, defVal, err.Error())
					}
				} else {
					ip := net.ParseIP(defVal)
					if ip != nil {
						flags.IP(fullPath, ip, usage)
					} else {
						_, cidr, err := net.ParseCIDR(defVal)
						if err == nil {
							flags.IPNet(fullPath, *cidr, usage)
						} else {
							flags.String(fullPath, defVal, usage)
						}
					}
				}

			case "!!float":
				f, err := strconv.ParseFloat(defVal, 64)
				if err != nil {
					log.Printf("configuration was tagged as yaml float, but couldn't be parsed as golang float. conf: %s, value: %s, err: %s", fullPath, defVal, err.Error())
				} else {
					flags.Float64(fullPath, f, usage)
				}

			case "!!null":
				log.Printf("configuration with `null` default is not supported. conf: %s", fullPath)

			case "!!timestamp":
				parsed, err := time.Parse(time.RFC3339, defVal)
				if err != nil {
					log.Printf("timestamp configuration could not be parsed. conf: %s, value: %s, err: %s", fullPath, defVal, err.Error())
				} else {
					flags.String(fullPath, parsed.String(), usage)
				}

			case "!!seq":
				log.Printf("sequence configuration not supported in this place. conf: %s, value: %s", fullPath, defVal)

			case "!!merge":
				log.Printf("merge configuration not supported. conf: %s, value: %s", fullPath, defVal)

			default:
				log.Printf("unknown configuration was deletected: config: %s, value: %s, tag: %s", fullPath, defVal, valueNode.Tag)
			}
		}
	}
}

func detectSequenceNodeTypes(node *yaml.Node) (string, error) {
	if node.Kind != yaml.SequenceNode {
		return "", fmt.Errorf("not a valid sequence node")
	}
	if len(node.Content) == 0 {
		lc := node.LineComment
		lc = strings.TrimPrefix(lc, "#")
		lc = strings.TrimSpace(lc)
		if lc == "" {
			return "", fmt.Errorf("node had no content, and no type hint was provided. can't detect type.")
		}
		if isValidSequanceScalerTypes(lc) {
			return lc, nil
		}
		return "", fmt.Errorf("scaler type hint provided is not valid: %s", lc)
	}
	firstContent := node.Content[0]
	tag := firstContent.Tag
	if !isValidSequanceScalerTypes(tag) {
		return "", fmt.Errorf("scaler type is not valid: %s", tag)
	}
	for _, c := range node.Content {
		if tag != c.Tag {
			return "", fmt.Errorf("failed to detect sequence type. values of array have different types.")
		}
	}
	return tag, nil
}

func isValidSequanceScalerTypes(kind string) bool {
	valid := []string{"!!str", "!!int", "!!bool", "!!float", "!!timestamp", "!!ip", "!!ipnet"}
	return slices.Contains(valid, kind)
}

func nodesToIntSlice(nodes []*yaml.Node) []int {
	result := make([]int, len(nodes))
	for i, c := range nodes {
		v, _ := strconv.Atoi(c.Value)
		result[i] = v
	}
	return result
}

func nodesToBoolSlice(nodes []*yaml.Node) []bool {
	result := make([]bool, len(nodes))
	for i, c := range nodes {
		v, _ := strconv.ParseBool(c.Value)
		result[i] = v
	}
	return result
}

func nodesToFloatSlice(nodes []*yaml.Node) []float64 {
	result := make([]float64, len(nodes))
	for i, c := range nodes {
		v, _ := strconv.ParseFloat(c.Value, 64)
		result[i] = v
	}
	return result
}

func nodesToTimestampSlice(nodes []*yaml.Node) []string {
	result := make([]string, len(nodes))
	for i, c := range nodes {
		parsed, _ := time.Parse(time.RFC3339, c.Value)
		result[i] = parsed.String()
	}
	return result
}

func nodesToStringSlice(nodes []*yaml.Node) []string {
	result := make([]string, len(nodes))
	for i, c := range nodes {
		result[i] = c.Value
	}
	return result
}

func nodesToIPSlice(nodes []*yaml.Node) []net.IP {
	result := make([]net.IP, len(nodes))
	for i, c := range nodes {
		ip := net.ParseIP(c.Value)
		result[i] = ip
	}
	return result
}

func nodesToIPNetSlice(nodes []*yaml.Node) []net.IPNet {
	result := make([]net.IPNet, len(nodes))
	for i, c := range nodes {
		_, cidr, _ := net.ParseCIDR(c.Value)
		result[i] = *cidr
	}
	return result
}

func ShowAllCliConfigFlags() {
	configFlagSet.VisitAll(func(f *pflag.Flag) {
		fmt.Printf("--%s (default: %s): %s\n", f.Name, f.DefValue, f.Usage)
	})
}
