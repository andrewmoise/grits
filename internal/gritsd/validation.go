package gritsd

import (
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"strings"
)

// UnmarshalAndValidate - centralized unmarshaling with validation

// Validation patterns and types
var validationPatterns = map[string]*regexp.Regexp{
	"blobAddr":     regexp.MustCompile(`^Qm[1-9A-HJ-NP-Za-km-z]{44}$`),
	"relativePath": regexp.MustCompile(`^([^\x00/]([^\x00]*[^\x00/])?)?$`),
	"volumeName":   regexp.MustCompile(`^[a-zA-Z0-9\-_]+$`),
	"base64":       regexp.MustCompile(`^[A-Za-z0-9+/]*={0,2}$`),
}

type ValidationType string

const (
	ValidateBlobAddr     ValidationType = "blobAddr"
	ValidateRelativePath ValidationType = "relativePath"
	ValidateVolumeName   ValidationType = "volumeName"
	ValidateBase64       ValidationType = "base64"
)

// Validate while unmarshaling
func UnmarshalAndValidate[T any](data []byte, result *T) error {
	if err := json.Unmarshal(data, result); err != nil {
		return fmt.Errorf("unmarshal failed: %v", err)
	}

	if err := ValidateStruct(result); err != nil {
		return fmt.Errorf("validation failed: %v", err)
	}

	return nil
}

// Validate primitive types (strings, etc.)
func ValidatePrimitive(value interface{}, validationType ValidationType) error {
	v := reflect.ValueOf(value)
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}

	if v.Kind() != reflect.String {
		return fmt.Errorf("primitive validation requires string type")
	}

	str := v.String()
	pattern, exists := validationPatterns[string(validationType)]
	if !exists {
		return fmt.Errorf("unknown validation type: %s", validationType)
	}

	if !pattern.MatchString(str) {
		return fmt.Errorf("invalid format for %s", validationType)
	}

	// Additional checks for relativePath
	if validationType == ValidateRelativePath {
		if len(str) > 4096 {
			return fmt.Errorf("path too long")
		}

		// Check for paths that are only periods or contain path components that are only periods
		parts := strings.Split(str, "/")
		for _, part := range parts {
			if part != "" && regexp.MustCompile(`^\.+$`).MatchString(part) {
				return fmt.Errorf("path components cannot be only periods")
			}
		}
	}

	return nil
}

// Struct validation using tags
func ValidateStruct(s interface{}) error {
	v := reflect.ValueOf(s)
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}

	if v.Kind() != reflect.Struct {
		return nil // Not a struct, skip
	}

	t := v.Type()
	for i := 0; i < v.NumField(); i++ {
		field := v.Field(i)
		fieldType := t.Field(i)

		validateTag := fieldType.Tag.Get("validate")
		if validateTag == "" {
			continue
		}

		if err := ValidateField(field, validateTag, fieldType.Name); err != nil {
			return err
		}

		// Recursively validate nested structs
		if field.Kind() == reflect.Struct || (field.Kind() == reflect.Ptr && field.Elem().Kind() == reflect.Struct) {
			if err := ValidateStruct(field.Interface()); err != nil {
				return fmt.Errorf("field %s: %v", fieldType.Name, err)
			}
		}

		// Validate slices of structs
		if field.Kind() == reflect.Slice {
			for j := 0; j < field.Len(); j++ {
				elem := field.Index(j)
				if elem.Kind() == reflect.Struct || (elem.Kind() == reflect.Ptr && elem.Elem().Kind() == reflect.Struct) {
					if err := ValidateStruct(elem.Interface()); err != nil {
						return fmt.Errorf("field %s[%d]: %v", fieldType.Name, j, err)
					}
				}
			}
		}
	}

	return nil
}

func ValidateField(field reflect.Value, validateTag, fieldName string) error {
	rules := strings.Split(validateTag, ",")

	for _, rule := range rules {
		rule = strings.TrimSpace(rule)

		// Handle special rules
		switch rule {
		case "optional":
			if field.Kind() == reflect.String && field.String() == "" {
				return nil // Skip other validations for empty optional fields
			}
			continue
		case "required":
			if field.Kind() == reflect.Slice && field.Len() == 0 {
				return fmt.Errorf("field %s is required", fieldName)
			}
			if field.Kind() == reflect.String && field.String() == "" {
				return fmt.Errorf("field %s is required", fieldName)
			}
			continue
		}

		// Pattern validation
		pattern, exists := validationPatterns[rule]
		if !exists {
			continue // Unknown rule, skip
		}

		if field.Kind() != reflect.String {
			return fmt.Errorf("field %s: pattern validation requires string field", fieldName)
		}

		str := field.String()
		if str != "" && !pattern.MatchString(str) {
			return fmt.Errorf("field %s: invalid format for %s", fieldName, rule)
		}

		// Additional checks for relativePath
		if rule == "relativePath" {
			if len(str) > 4096 {
				return fmt.Errorf("field %s: path too long", fieldName)
			}

			// Check for paths that are only periods or contain path components that are only periods
			parts := strings.Split(str, "/")
			for _, part := range parts {
				if part != "" && regexp.MustCompile(`^\.+$`).MatchString(part) {
					return fmt.Errorf("field %s: path components cannot be only periods", fieldName)
				}
			}
		}
	}

	return nil
}
