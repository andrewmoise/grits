package grits

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"time"
)

// Config represents the core server configuration.
type Config struct {
	// File locations
	ServerDir string `json:"-"`

	// Storage configuration
	StorageSize         int64 `json:"storageSize"`
	StorageFreeSize     int64 `json:"storageFreeSize"`
	NamespaceSavePeriod int   `json:"namespaceSavePeriod"`
	HardLinkBlobs       bool  `json:"hardLinkBlobs"`
	ValidateBlobs       bool  `json:"validateBlobs"`

	// User configuration
	RunAsUser  string `json:"runAsUser,omitempty"`
	RunAsGroup string `json:"runAsGroup,omitempty"`

	// Modules and configs for same
	Modules []json.RawMessage `json:"modules"`

	// Logging
	DoLogging bool `json:"doLogging,omitempty"`
}

// NewConfig creates a new configuration instance with default values.
func NewConfig(serverDir string) *Config {
	return &Config{
		ServerDir: serverDir,

		StorageSize:         100 * 1024 * 1024, // Max size of data
		StorageFreeSize:     80 * 1024 * 1024,  // Size to clean down to when overfull
		NamespaceSavePeriod: 30,                // # of seconds between namespace checkpoints
		HardLinkBlobs:       false,
		ValidateBlobs:       false,
	}
}

func (c *Config) ServerPath(path string) string {
	return filepath.Join(c.ServerDir, path)
}

func (c *Config) LoadFromFile(filename string) error {
	file, err := os.Open(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	// Use json.Decoder to decode directly into the struct
	decoder := json.NewDecoder(file)
	if err := decoder.Decode(c); err != nil {
		return err
	}

	return nil
}

// SaveToFile writes the config to the specified file in JSON format
func (c *Config) SaveToFile(filename string) error {
	// Create or truncate the file
	file, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	// Create an encoder with pretty-printing for readability
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")

	// Write the JSON to the file
	if err := encoder.Encode(c); err != nil {
		return err
	}

	return nil
}

// UnmarshalDurationFields unmarshals a JSON object into a struct, treating
// any time.Duration fields as human-readable strings (e.g. "30s", "24h").
// Usage: call from your struct's UnmarshalJSON method.
func UnmarshalDurationFields(data []byte, dst any) error {
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	v := reflect.ValueOf(dst).Elem()
	t := v.Type()

	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		jsonTag := field.Tag.Get("json")
		jsonName := splitTag(jsonTag)
		if jsonName == "" || jsonName == "-" {
			continue
		}

		rawValue, ok := raw[jsonName]
		if !ok {
			continue
		}

		fieldValue := v.Field(i)

		switch field.Type {
		case reflect.TypeOf(time.Duration(0)):
			str, ok := rawValue.(string)
			if !ok {
				return fmt.Errorf("field %s: expected duration string, got %T", jsonName, rawValue)
			}
			d, err := time.ParseDuration(str)
			if err != nil {
				return fmt.Errorf("field %s: invalid duration %q: %v", jsonName, str, err)
			}
			fieldValue.Set(reflect.ValueOf(d))

		case reflect.TypeOf(""):
			if str, ok := rawValue.(string); ok {
				fieldValue.SetString(str)
			}

		case reflect.TypeOf(false):
			if b, ok := rawValue.(bool); ok {
				fieldValue.SetBool(b)
			}

		case reflect.TypeOf(0):
			if n, ok := rawValue.(float64); ok {
				fieldValue.SetInt(int64(n))
			}
		}
	}

	return nil
}

// MarshalDurationFields marshals a struct to JSON, converting time.Duration
// fields to human-readable strings.
func MarshalDurationFields(src any) ([]byte, error) {
	v := reflect.ValueOf(src)
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}
	t := v.Type()

	m := make(map[string]any)
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		jsonTag := field.Tag.Get("json")
		jsonName := splitTag(jsonTag)
		if jsonName == "" || jsonName == "-" {
			continue
		}

		fieldValue := v.Field(i)

		if field.Type == reflect.TypeOf(time.Duration(0)) {
			d := fieldValue.Interface().(time.Duration)
			if d == 0 && hasOmitempty(jsonTag) {
				continue
			}
			m[jsonName] = d.String()
		} else {
			iface := fieldValue.Interface()
			// Skip zero values if omitempty
			if hasOmitempty(jsonTag) && reflect.DeepEqual(iface, reflect.Zero(field.Type).Interface()) {
				continue
			}
			m[jsonName] = iface
		}
	}

	return json.Marshal(m)
}

func splitTag(tag string) string {
	if idx := len(tag); idx == 0 {
		return ""
	}
	for i, c := range tag {
		if c == ',' {
			return tag[:i]
		}
	}
	return tag
}

func hasOmitempty(tag string) bool {
	for i, c := range tag {
		if c == ',' {
			rest := tag[i+1:]
			for len(rest) > 0 {
				var part string
				if j := len(rest); j == 0 {
					break
				}
				for j, c2 := range rest {
					if c2 == ',' {
						part = rest[:j]
						rest = rest[j+1:]
						break
					}
					if j == len(rest)-1 {
						part = rest
						rest = ""
					}
				}
				if part == "omitempty" {
					return true
				}
			}
		}
	}
	return false
}
